package xiaohongshu

// Added in ycl-2004/xiaohongshu-mcp; see NOTICE.

import (
	"log/slog"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/pkg/errors"
)

// invisible 编辑器在话题旁边插的看不见的字符：比较文字之前去掉。
var invisible = strings.NewReplacer("\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "")

// checkTags 话题标签填完以后回读那一行。2026-10-20 话题搜索接口一直报 500：点了下拉框第一项，插进去的是
// 「##温哥华数学辅导」；没有下拉框的那个，打的「#」也丢了，只剩「温哥华补课」。坏了就把这一行删掉、不带标签发。
// 删的时候正文一个字都不能动：正文变了就回错误（还没点发布，算没发出，调用方稍后重试）；
// 删不掉但正文没动，照原样发（和没检查之前一样，只是标签难看）。
// body 是填标签之前编辑框的文字。
func checkTags(contentElem *rod.Element, tags []string, body string) error {
	if len(tags) == 0 {
		return nil
	}
	line, err := lastParagraphText(contentElem)
	if err != nil {
		slog.Warn("读不到话题标签那一行，照原样发", "error", err)
		return nil
	}
	if !malformedTags(line, tags) {
		return nil
	}
	slog.Warn("话题标签那一行坏了，删掉、不带标签发", "line", line)
	if err := clearLastParagraph(contentElem); err != nil {
		return errors.Wrap(err, "删掉坏了的话题标签失败")
	}
	time.Sleep(300 * time.Millisecond) // 技术等待：等编辑器把删除写回页面
	after, err := editorText(contentElem)
	if err != nil {
		return errors.Wrap(err, "删话题标签以后回读正文失败")
	}
	switch got, want := flatText(after), flatText(body); {
	case got == want:
		slog.Info("坏了的话题标签删掉了，这篇不带标签发")
		return nil
	case strings.HasPrefix(got, want):
		slog.Warn("坏了的话题标签没删掉，正文没动，照原样发", "after", after)
		return nil
	default:
		return errors.Errorf("删话题标签时正文变了，这次不发：%q", after)
	}
}

// malformedTags 标签那一行有没有坏：有「##」开头的，或者某个标签的字前面没有「#」。
// 联想换成了别的话题（字对不上）、几个话题挨在一起，都不算坏。
func malformedTags(line string, tags []string) bool {
	want := map[string]bool{}
	for _, tag := range tags {
		if tag = strings.TrimLeft(strings.TrimSpace(tag), "#"); tag != "" {
			want[tag] = true
		}
	}
	for _, token := range strings.Fields(invisible.Replace(line)) {
		if strings.HasPrefix(token, "##") || (!strings.HasPrefix(token, "#") && want[token]) {
			return true
		}
	}
	return false
}

// flatText 比较正文用：去掉看不见的字符，连续的空白（含换行、空段落）算一个空格。
func flatText(text string) string {
	return strings.Join(strings.Fields(invisible.Replace(text)), " ")
}

// editorText 编辑框现在的文字（innerText）。
func editorText(contentElem *rod.Element) (string, error) {
	got, err := contentElem.Eval(`() => this.innerText`)
	if err != nil {
		return "", err
	}
	return got.Value.Str(), nil
}

// lastParagraphText 编辑框最后一段的文字：inputTags 先按两次回车，标签在单独的一段。
func lastParagraphText(contentElem *rod.Element) (string, error) {
	got, err := contentElem.Eval(`() => { const p = this.lastElementChild; return p ? p.innerText : "" }`)
	if err != nil {
		return "", err
	}
	return got.Value.Str(), nil
}

// clearLastParagraph 选中编辑框最后一段的全部内容，按一次退格。选区用页面自己的 Selection 设：
// 编辑器（ProseMirror）按键时读页面的选区，删的就是这一段，话题也一起删。
// 先 focus 再设选区（反过来 focus 会把光标挪走）；按键用页面的键盘，不再 focus 元素。
func clearLastParagraph(contentElem *rod.Element) error {
	selected, err := contentElem.Eval(`() => {
		const p = this.lastElementChild;
		if (!p) return false;
		this.focus();
		const range = document.createRange();
		range.selectNodeContents(p);
		const selection = window.getSelection();
		selection.removeAllRanges();
		selection.addRange(range);
		return true;
	}`)
	if err != nil {
		return err
	}
	if !selected.Value.Bool() {
		return errors.New("编辑框里没有段落")
	}
	time.Sleep(200 * time.Millisecond) // 技术等待：等编辑器读到新的选区
	return contentElem.Page().KeyActions().Type(input.Backspace).Do()
}
