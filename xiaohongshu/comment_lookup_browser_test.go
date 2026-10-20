package xiaohongshu

// Added in mengbingrock/xiaohongshu-mcp (2026-10-20); see NOTICE.

import (
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
)

// TestAFoundCommentStaysUsableAfterTheLookupTimeout 复现 2026-10-20：找评论用 2 秒的时限，找到的元素一直带着它，
// 过了 2 秒再滚动、悬停、点「…」全部超时（删二级回复要先展开回复，服务器上必挂）。要设 XHS_BROWSER_BIN 才跑。
func TestAFoundCommentStaysUsableAfterTheLookupTimeout(t *testing.T) {
	bin := os.Getenv("XHS_BROWSER_BIN")
	if bin == "" {
		t.Skip("设 XHS_BROWSER_BIN 才跑真浏览器测试")
	}
	url := launcher.New().Bin(bin).Headless(true).MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()
	page := browser.MustPage(`data:text/html;charset=utf-8,<div id="comment-c1" class="comment-item">回复<span class="more">…</span></div>`)
	page.MustWaitLoad()

	el := lookupComment(page, "c1", "")
	require.NotNil(t, el)
	time.Sleep(2500 * time.Millisecond)
	_, err := el.Element(".more")
	require.NoError(t, err, "找到 2 秒后，评论元素还要能用")
}
