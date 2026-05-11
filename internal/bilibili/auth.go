package bilibili

import (
	"fmt"

	"game-live-room/internal/model"
)

// BuildCookieHeader constructs a B站 cookie header string from a BiliCookie record.
func BuildCookieHeader(c *model.BiliCookie) string {
	return fmt.Sprintf("SESSDATA=%s; bili_jct=%s; DedeUserID=%s",
		c.SESSDATA, c.BiliJCT, c.DedeUserID)
}
