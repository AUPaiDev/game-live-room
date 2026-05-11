package bilibili

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	danmuInfoURL      = "https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id=%d"
	qrcodeGenerateURL = "https://passport.bilibili.com/x/passport-login/web/qrcode/generate"
	qrcodePollURL     = "https://passport.bilibili.com/x/passport-login/web/qrcode/poll?qrcode_key=%s"
	navURL            = "https://api.bilibili.com/x/web-interface/nav"
)

type danmuInfoResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Token    string `json:"token"`
		HostList []struct {
			Host    string `json:"host"`
			WssPort int    `json:"wss_port"`
		} `json:"host_list"`
	} `json:"data"`
}

// GetDanmuInfo fetches the danmaku WebSocket token and host for a room.
// It uses the provided cookie for authentication.
func GetDanmuInfo(roomID uint64, cookie string, userAgent string) (token string, host string, err error) {
	reqURL := fmt.Sprintf(danmuInfoURL, roomID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://live.bilibili.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("get danmu info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read response: %w", err)
	}

	var info danmuInfoResp
	if err := json.Unmarshal(body, &info); err != nil {
		return "", "", fmt.Errorf("parse danmu info: %w", err)
	}
	if info.Code != 0 {
		return "", "", fmt.Errorf("danmu info api error %d: %s", info.Code, info.Message)
	}

	if len(info.Data.HostList) > 0 {
		h := info.Data.HostList[0]
		host = fmt.Sprintf("wss://%s/sub", h.Host)
	}
	return info.Data.Token, host, nil
}

// ExtractUID parses the DedeUserID field from a B站 cookie string.
func ExtractUID(cookie string) uint64 {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "DedeUserID=") {
			val := strings.TrimPrefix(part, "DedeUserID=")
			var uid uint64
			fmt.Sscanf(val, "%d", &uid)
			return uid
		}
	}
	return 0
}

// QRCodeData holds the QR code URL and key for polling.
type QRCodeData struct {
	URL       string `json:"url"`
	QRCodeKey string `json:"qrcode_key"`
}

// QRPollResult holds the result of a QR code poll.
type QRPollResult struct {
	// Status codes: 86101=not scanned, 86090=scanned not confirmed, 86038=expired, 0=success
	Status     int    `json:"status"`
	Message    string `json:"message"`
	SESSDATA   string `json:"sessdata,omitempty"`
	BiliJCT    string `json:"bili_jct,omitempty"`
	DedeUserID string `json:"dede_user_id,omitempty"`
}

// UserInfo holds basic B站 user profile data.
type UserInfo struct {
	UID     uint64 `json:"uid"`
	Uname   string `json:"uname"`
	Face    string `json:"face"`
	Level   int    `json:"level"`
	IsValid bool   `json:"is_valid"`
}

// GenerateQRCode requests a new login QR code from B站.
func GenerateQRCode(userAgent string) (*QRCodeData, error) {
	req, err := http.NewRequest(http.MethodGet, qrcodeGenerateURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("generate qrcode: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Code int        `json:"code"`
		Data QRCodeData `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse qrcode response: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("generate qrcode error: %d", result.Code)
	}
	return &result.Data, nil
}

// PollQRCode checks the scan status of a QR code.
// On success (Status==0), SESSDATA/BiliJCT/DedeUserID are populated.
func PollQRCode(qrcodeKey string, userAgent string) (*QRPollResult, error) {
	reqURL := fmt.Sprintf(qrcodePollURL, qrcodeKey)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll qrcode: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var raw struct {
		Code int `json:"code"`
		Data struct {
			URL     string `json:"url"`
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse poll response: %w", err)
	}

	result := &QRPollResult{
		Status:  raw.Data.Code,
		Message: raw.Data.Message,
	}

	if raw.Data.Code == 0 && raw.Data.URL != "" {
		// Credentials are embedded as query params in the redirect URL
		if u, err := url.Parse(raw.Data.URL); err == nil {
			q := u.Query()
			result.SESSDATA = q.Get("SESSDATA")
			result.BiliJCT = q.Get("bili_jct")
			result.DedeUserID = q.Get("DedeUserID")
		}
	}
	return result, nil
}

// VerifyCookie checks if a cookie is valid by calling the B站 nav API.
func VerifyCookie(sessdata, biliJCT, dedeUserID, userAgent string) (*UserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, navURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Cookie", fmt.Sprintf("SESSDATA=%s; bili_jct=%s; DedeUserID=%s", sessdata, biliJCT, dedeUserID))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("verify cookie: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Code int `json:"code"`
		Data struct {
			Mid       uint64 `json:"mid"`
			Uname     string `json:"uname"`
			Face      string `json:"face"`
			LevelInfo struct {
				CurrentLevel int `json:"current_level"`
			} `json:"level_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse nav response: %w", err)
	}
	if result.Code != 0 {
		return &UserInfo{IsValid: false}, nil
	}
	d := result.Data
	return &UserInfo{
		UID:     d.Mid,
		Uname:   d.Uname,
		Face:    d.Face,
		Level:   d.LevelInfo.CurrentLevel,
		IsValid: true,
	}, nil
}
