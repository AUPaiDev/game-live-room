package bilibili

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const danmuInfoURL = "https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id=%d"

type danmuInfoResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Token       string `json:"token"`
		HostList    []struct {
			Host    string `json:"host"`
			WssPort int    `json:"wss_port"`
		} `json:"host_list"`
	} `json:"data"`
}

// GetDanmuInfo fetches the danmaku WebSocket token and host for a room.
// It uses the provided cookie for authentication.
func GetDanmuInfo(roomID uint64, cookie string, userAgent string) (token string, host string, err error) {
	url := fmt.Sprintf(danmuInfoURL, roomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
