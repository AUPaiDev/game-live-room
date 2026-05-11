package bilibili

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// LiveMessage represents a parsed live room event.
type LiveMessage struct {
	Cmd        string
	Text       string
	UID        uint64
	Username   string
	Timestamp  int64
	Color      int
	GiftName   string
	GiftCount  int
	Price      int    // 金瓜子(gift) or RMB元(SC/guard)
	GuardLevel int
}

// packet operation codes
const (
	opHeartbeat      = 2
	opHeartbeatReply = 3
	opMessage        = 5
	opAuth           = 7
	opAuthReply      = 8
)

// packet header size
const headerSize = 16

// DanmakuClient connects to B站 live danmaku WebSocket.
type DanmakuClient struct {
	roomID     uint64
	cookie     string
	userAgent  string
	wsURL      string
	logger     *zap.Logger
	cookieFunc func() string // optional: returns current cookie from DB

	OnDanmaku func(msg LiveMessage)
}

// NewDanmakuClient creates a new DanmakuClient.
func NewDanmakuClient(roomID uint64, cookie, userAgent, wsURL string, logger *zap.Logger) *DanmakuClient {
	return &DanmakuClient{
		roomID:    roomID,
		cookie:    cookie,
		userAgent: userAgent,
		wsURL:     wsURL,
		logger:    logger,
	}
}

// SetCookieFunc sets a function that returns the current active cookie.
// When set, it is called before each reconnect to pick up newly saved cookies.
func (c *DanmakuClient) SetCookieFunc(fn func() string) {
	c.cookieFunc = fn
}

// activeCookie returns the current cookie, preferring the dynamic source.
func (c *DanmakuClient) activeCookie() string {
	if c.cookieFunc != nil {
		if cookie := c.cookieFunc(); cookie != "" {
			return cookie
		}
	}
	return c.cookie
}

// Connect starts the danmaku client with exponential backoff reconnection.
// It blocks until ctx is cancelled.
func (c *DanmakuClient) Connect(ctx context.Context) {
	backoff := 5 * time.Second
	const maxBackoff = 60 * time.Second

	for {
		if err := c.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Warn("danmaku disconnected, reconnecting",
				zap.Error(err), zap.Duration("backoff", backoff))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
		}

		// Refresh token before reconnecting.
		token, host, err := GetDanmuInfo(c.roomID, c.activeCookie(), c.userAgent)
		if err != nil {
			c.logger.Warn("refresh danmu info failed", zap.Error(err))
		} else if host != "" {
			c.wsURL = host
			_ = token // token used in auth below
		}
	}
}

func (c *DanmakuClient) connect(ctx context.Context) error {
	cookie := c.activeCookie()
	token, _, err := GetDanmuInfo(c.roomID, cookie, c.userAgent)
	if err != nil {
		c.logger.Warn("get danmu info failed, connecting without token", zap.Error(err))
	}

	dialer := websocket.DefaultDialer
	header := map[string][]string{
		"User-Agent": {c.userAgent},
		"Cookie":     {cookie},
	}
	conn, _, err := dialer.DialContext(ctx, c.wsURL, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	uid := ExtractUID(cookie)
	if err := c.sendAuth(conn, uid, token); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.heartbeatLoop(heartbeatCtx, conn)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		c.handlePacket(data)
	}
}

func (c *DanmakuClient) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pkt := buildPacket(opHeartbeat, 1, 0, []byte("[object Object]"))
			if err := conn.WriteMessage(websocket.BinaryMessage, pkt); err != nil {
				c.logger.Warn("heartbeat write failed", zap.Error(err))
				return
			}
		}
	}
}

func (c *DanmakuClient) sendAuth(conn *websocket.Conn, uid uint64, token string) error {
	auth := map[string]interface{}{
		"uid":      uid,
		"roomid":   c.roomID,
		"protover": 3,
		"platform": "web",
		"type":     2,
		"key":      token,
	}
	body, err := json.Marshal(auth)
	if err != nil {
		return fmt.Errorf("marshal auth: %w", err)
	}
	pkt := buildPacket(opAuth, 1, 0, body)
	return conn.WriteMessage(websocket.BinaryMessage, pkt)
}

// buildPacket constructs a B站 WebSocket packet.
func buildPacket(op, ver, seq int, body []byte) []byte {
	totalLen := headerSize + len(body)
	buf := make([]byte, totalLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLen))
	binary.BigEndian.PutUint16(buf[4:6], uint16(headerSize))
	binary.BigEndian.PutUint16(buf[6:8], uint16(ver))
	binary.BigEndian.PutUint32(buf[8:12], uint32(op))
	binary.BigEndian.PutUint32(buf[12:16], uint32(seq))
	copy(buf[headerSize:], body)
	return buf
}

func (c *DanmakuClient) handlePacket(data []byte) {
	if len(data) < headerSize {
		return
	}
	totalLen := int(binary.BigEndian.Uint32(data[0:4]))
	headerLen := int(binary.BigEndian.Uint16(data[4:6]))
	ver := int(binary.BigEndian.Uint16(data[6:8]))
	op := int(binary.BigEndian.Uint32(data[8:12]))

	if totalLen > len(data) || headerLen < headerSize {
		return
	}
	body := data[headerLen:totalLen]

	switch op {
	case opHeartbeatReply:
		// popularity count, ignore
	case opMessage:
		c.handleMessageBody(ver, body)
	case opAuthReply:
		c.logger.Info("danmaku auth reply received")
	}
}

func (c *DanmakuClient) handleMessageBody(ver int, body []byte) {
	switch ver {
	case 0:
		c.parseMessage(body)
	case 2:
		c.decompressZlib(body)
	case 3:
		c.decompressBrotli(body)
	default:
		c.logger.Debug("unknown protover", zap.Int("ver", ver))
	}
}

func (c *DanmakuClient) decompressZlib(data []byte) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		c.logger.Warn("zlib reader", zap.Error(err))
		return
	}
	defer r.Close()
	c.parseMultiPackets(r)
}

func (c *DanmakuClient) decompressBrotli(data []byte) {
	r := brotli.NewReader(bytes.NewReader(data))
	c.parseMultiPackets(r)
}

func (c *DanmakuClient) parseMultiPackets(r io.Reader) {
	buf, err := io.ReadAll(r)
	if err != nil {
		c.logger.Warn("read decompressed", zap.Error(err))
		return
	}
	offset := 0
	for offset+headerSize <= len(buf) {
		totalLen := int(binary.BigEndian.Uint32(buf[offset : offset+4]))
		headerLen := int(binary.BigEndian.Uint16(buf[offset+4 : offset+6]))
		if totalLen <= 0 || offset+totalLen > len(buf) {
			break
		}
		body := buf[offset+headerLen : offset+totalLen]
		c.parseMessage(body)
		offset += totalLen
	}
}

func (c *DanmakuClient) parseMessage(data []byte) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	var cmd string
	if err := json.Unmarshal(raw["cmd"], &cmd); err != nil {
		return
	}

	switch {
	case cmd == "DANMU_MSG":
		c.parseDanmaku(raw)
	case cmd == "SEND_GIFT":
		c.parseGift(raw)
	case cmd == "SUPER_CHAT_MESSAGE":
		c.parseSuperChat(raw)
	case cmd == "GUARD_BUY":
		c.parseGuardBuy(raw)
	case cmd == "ONLINE_RANK_COUNT":
		// ignore
	}
}

func (c *DanmakuClient) parseDanmaku(raw map[string]json.RawMessage) {
	var info []json.RawMessage
	if err := json.Unmarshal(raw["info"], &info); err != nil || len(info) < 3 {
		return
	}

	var meta []json.RawMessage
	if err := json.Unmarshal(info[0], &meta); err != nil || len(meta) < 4 {
		return
	}

	var text string
	json.Unmarshal(info[1], &text)

	var userInfo []json.RawMessage
	if err := json.Unmarshal(info[2], &userInfo); err != nil || len(userInfo) < 2 {
		return
	}

	var uid uint64
	var username string
	json.Unmarshal(userInfo[0], &uid)
	json.Unmarshal(userInfo[1], &username)

	var color int
	if len(meta) > 3 {
		json.Unmarshal(meta[3], &color)
	}

	var ts int64
	if len(meta) > 4 {
		json.Unmarshal(meta[4], &ts)
	}

	msg := LiveMessage{
		Cmd:       "DANMU_MSG",
		Text:      text,
		UID:       uid,
		Username:  username,
		Color:     color,
		Timestamp: ts,
	}
	c.dispatch(msg)
}

func (c *DanmakuClient) parseGift(raw map[string]json.RawMessage) {
	var data struct {
		UID       uint64 `json:"uid"`
		Uname     string `json:"uname"`
		GiftName  string `json:"giftName"`
		Num       int    `json:"num"`
		Price     int    `json:"price"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		return
	}
	msg := LiveMessage{
		Cmd:       "SEND_GIFT",
		UID:       data.UID,
		Username:  data.Uname,
		GiftName:  data.GiftName,
		GiftCount: data.Num,
		Price:     data.Price,
		Timestamp: data.Timestamp,
	}
	c.dispatch(msg)
}

func (c *DanmakuClient) parseSuperChat(raw map[string]json.RawMessage) {
	var data struct {
		UID       uint64 `json:"uid"`
		UserInfo  struct {
			Uname string `json:"uname"`
		} `json:"user_info"`
		Message   string `json:"message"`
		Price     int    `json:"price"`
		StartTime int64  `json:"start_time"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		return
	}
	msg := LiveMessage{
		Cmd:       "SUPER_CHAT_MESSAGE",
		UID:       data.UID,
		Username:  data.UserInfo.Uname,
		Text:      data.Message,
		Price:     data.Price,
		Timestamp: data.StartTime,
	}
	c.dispatch(msg)
}

func (c *DanmakuClient) parseGuardBuy(raw map[string]json.RawMessage) {
	var data struct {
		UID        uint64 `json:"uid"`
		Username   string `json:"username"`
		GuardLevel int    `json:"guard_level"`
		Price      int    `json:"price"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		return
	}
	msg := LiveMessage{
		Cmd:        "GUARD_BUY",
		UID:        data.UID,
		Username:   data.Username,
		GuardLevel: data.GuardLevel,
		Price:      data.Price / 1000, // convert 金瓜子 to RMB
	}
	c.dispatch(msg)
}

func (c *DanmakuClient) dispatch(msg LiveMessage) {
	if c.OnDanmaku != nil {
		c.OnDanmaku(msg)
	}
}
