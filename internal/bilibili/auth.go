package bilibili

import (
	"fmt"

	"game-live-room/internal/model"
)

// BuildCookieHeader constructs a B站 cookie header string from a BiliCookie record.
// Includes buvid3 if stored, which helps bypass B站 risk control on server IPs.
func BuildCookieHeader(c *model.BiliCookie) string {
	s := fmt.Sprintf("SESSDATA=%s; bili_jct=%s; DedeUserID=%s",
		c.SESSDATA, c.BiliJCT, c.DedeUserID)
	if c.Buvid3 != "" {
		s += "; buvid3=" + c.Buvid3
	}
	return s
}
