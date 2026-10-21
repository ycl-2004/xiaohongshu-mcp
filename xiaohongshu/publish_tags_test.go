package xiaohongshu

// Added in ycl-2004/xiaohongshu-mcp; see NOTICE.

import (
	"net/url"
	"os"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
)

var tagsOf20261020 = []string{"温哥华数学辅导", "温哥华补课", "BC高中数学", "数学一对一", "大温家长"}

func TestMalformedTagsCatchesTheDoubleHashAndTheLostHash(t *testing.T) {
	// 2026-10-20 话题搜索接口报 500 时发布页上那一行
	require.True(t, malformedTags("##温哥华数学辅导 温哥华补课 #BC高中数学 #数学一对一 #大温家长 ", tagsOf20261020))
	require.True(t, malformedTags("#温哥华数学辅导 温哥华补课 #BC高中数学", tagsOf20261020), "丢了「#」")
	require.True(t, malformedTags("#温哥华数学辅导 ##BC高中数学", tagsOf20261020))
	require.False(t, malformedTags("#温哥华数学辅导 #温哥华补课 #BC高中数学 #数学一对一 #大温家长 ", tagsOf20261020))
	require.False(t, malformedTags("#温哥华数学\u200b #BC高中数学课程", tagsOf20261020), "联想换成了别的话题不算坏")
	require.False(t, malformedTags("#温哥华数学辅导#温哥华补课", tagsOf20261020), "挨在一起不算坏")
	require.False(t, malformedTags("", tagsOf20261020))
	require.Equal(t, "正文 第二段", flatText("正文\n\n第二段\u200b\n"))
}

func editorPage(t *testing.T, body string) (*rod.Page, *rod.Element) {
	t.Helper()
	bin := os.Getenv("XHS_BROWSER_BIN")
	if bin == "" {
		t.Skip("设 XHS_BROWSER_BIN 才跑真浏览器测试")
	}
	browser := rod.New().ControlURL(launcher.New().Bin(bin).Headless(true).MustLaunch()).MustConnect()
	t.Cleanup(browser.MustClose)
	page := browser.MustPage("data:text/html;charset=utf-8," + url.PathEscape(
		`<div id="ed" role="textbox" contenteditable="true">`+body+`</div>`))
	page.MustWaitLoad()
	return page, page.MustElement("#ed")
}

// TestABrokenTagLineIsDroppedAndTheBodyIsLeftAlone 坏了的标签行删掉，正文一个字不动，可以发。
func TestABrokenTagLineIsDroppedAndTheBodyIsLeftAlone(t *testing.T) {
	_, ed := editorPage(t, `<p>带 10 到 12 年级孩子学 BC 数学。</p><p>第二段。</p>`)
	body, err := editorText(ed)
	require.NoError(t, err)
	ed.MustEval(`() => { this.insertAdjacentHTML("beforeend",
		"<p><br></p><p><a class=\"topic\">##温哥华数学辅导</a> 温哥华补课 <a class=\"topic\">#BC高中数学</a></p>") }`)

	require.NoError(t, checkTags(ed, tagsOf20261020, body))
	after, err := editorText(ed)
	require.NoError(t, err)
	require.Equal(t, flatText(body), flatText(after))
	require.NotContains(t, after, "温哥华补课")
}

// TestAGoodTagLineIsKept 标签好好的不动。
func TestAGoodTagLineIsKept(t *testing.T) {
	_, ed := editorPage(t, `<p>正文。</p><p><br></p><p>#温哥华数学辅导 #温哥华补课</p>`)
	require.NoError(t, checkTags(ed, tagsOf20261020, "正文。"))
	line, err := lastParagraphText(ed)
	require.NoError(t, err)
	require.Equal(t, "#温哥华数学辅导 #温哥华补课", line)
}

// TestTagsInTheBodyParagraphAreNotDeletedWithIt 标签没落在单独一段（和正文同一段）：删了会动正文，就不发。
func TestTagsInTheBodyParagraphAreNotDeletedWithIt(t *testing.T) {
	_, ed := editorPage(t, `<p>正文。 ##温哥华数学辅导 温哥华补课</p>`)
	err := checkTags(ed, tagsOf20261020, "正文。")
	require.ErrorContains(t, err, "正文变了")
}
