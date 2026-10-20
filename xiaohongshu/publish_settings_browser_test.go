package xiaohongshu

// Added in ycl-2004/xiaohongshu-mcp; see NOTICE.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settingsFixture 发布页「内容设置」一块的样子（按 2026-10-20 的截图和上午、下午两次发布推出来的布局）：
// 中间的表单在自己的容器里滚动，底部悬浮着「暂存离开 / 发布」一栏。内容类型声明正好落在这一栏下面——
// 还在视口里，但被挡着；可见范围在更下面，视口外。{{BODY}} 换成 settingsBody 里的展开或收起。
const settingsFixture = `<style>
body { margin: 0; font-size: 14px }
.header { height: 64px }
.main { position: fixed; top: 64px; left: 0; right: 0; bottom: 0; overflow: auto }
.editor { height: calc(100vh - 230px) }
.settings-head { height: 30px }
.card { height: 40px; margin: 5px 0 }
.d-select-content { height: 30px; width: 300px; border: 1px solid }
.footer { position: fixed; left: 0; right: 0; bottom: 0; height: 100px; background: white; z-index: 10 }
.d-options-wrapper { position: fixed; z-index: 20; background: white; width: 300px }
.d-option, .custom-option { height: 30px }
</style>
<div class="header">创作服务平台</div>
<div class="main">
  <div class="editor">正文</div>
  <div class="settings">{{BODY}}</div>
  <div style="height: 400px"></div>
</div>
<div class="footer"><button>暂存离开</button><button>发布</button></div>
<script>
window.CLICKED = [];
const choices = {
  declaration: ['虚构演绎，仅供娱乐', '笔记含AI合成内容', '内容包含营销广告'],
  visibility: ['公开可见', '仅自己可见', '仅互关好友可见'],
};
let open = null;
document.querySelectorAll('[data-select]').forEach(wrapper => {
  const shown = wrapper.querySelector('.d-select-content');
  shown.addEventListener('click', () => {
    const name = wrapper.dataset.select;
    window.CLICKED.push(name);
    if (open) open.remove();
    open = document.createElement('div');
    open.className = 'd-options-wrapper';
    const box = shown.getBoundingClientRect();
    open.style.left = box.left + 'px';
    open.style.top = box.bottom + 'px';
    for (const text of choices[name]) {
      const item = document.createElement('div');
      if (name === 'visibility') {
        item.className = 'd-grid-item';
        item.innerHTML = '<div class="custom-option"></div>';
        item.firstChild.textContent = text;
      } else {
        item.className = 'd-option';
        item.textContent = text;
      }
      item.addEventListener('click', () => { shown.textContent = text; open.remove(); open = null; });
      open.appendChild(item);
    }
    document.body.appendChild(open);
  });
});
const toggle = document.querySelector('.toggle');
toggle.addEventListener('click', () => {
  const body = document.querySelector('.settings-body');
  const collapsed = body.style.display === 'none';
  body.style.display = collapsed ? 'block' : 'none';
  toggle.firstChild.textContent = collapsed ? '收起' : '展开';
});
</script>`

// settingsBody 「内容设置」展开（toggle 显示「收起」）或收起（显示「展开」，里面的控件不显示）。
func settingsBody(collapsed bool) string {
	toggle, display := "收起", "block"
	if collapsed {
		toggle, display = "展开", "none"
	}
	return `<div class="settings-head"><span>内容设置</span><div class="toggle"><span>` + toggle + `</span></div></div>
<div class="settings-body" style="display: ` + display + `">
  <div class="custom-switch-card card">原创声明</div>
  <div class="d-select-wrapper card" data-select="declaration"><div class="d-select-content">添加内容类型声明</div></div>
  <div class="d-select-wrapper card"><div class="d-select-content">添加地点</div></div>
  <div class="d-select-wrapper card"><div class="d-select-content">关联群聊</div></div>
  <div class="permission-card-wrapper card"><div class="d-select-wrapper" data-select="visibility"><div class="d-select-content">公开可见</div></div></div>
</div>`
}

// TestTheDeclarationIsReachedWhateverTheVisibility 复现 2026-10-20：同一个号上午发「仅自己可见」成功，下午发「公开可见」
// 在「点开内容类型声明」上超时。选默认的公开可见时不点可见范围，没人把这一块滚上来；声明落在底部发布栏下面，rod 认为
// 它已经在视口里、不滚，点击一直被挡到超时。不声明（空）时完全不碰声明控件。要设 XHS_BROWSER_BIN 才跑。
func TestTheDeclarationIsReachedWhateverTheVisibility(t *testing.T) {
	bin := os.Getenv("XHS_BROWSER_BIN")
	if bin == "" {
		t.Skip("设 XHS_BROWSER_BIN 才跑真浏览器测试")
	}
	browser := rod.New().ControlURL(launcher.New().Bin(bin).Headless(true).MustLaunch()).MustConnect()
	defer browser.MustClose()

	cases := []struct {
		name        string
		collapsed   bool
		visibility  string
		contentType string
	}{
		{"公开可见：声明在底部发布栏下面（10-20 下午）", false, "", "笔记含AI合成内容"},
		{"仅自己可见：先点可见范围（10-20 上午）", false, "仅自己可见", "笔记含AI合成内容"},
		{"内容设置收起了", true, "", "笔记含AI合成内容"},
		{"不声明：不碰声明控件", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := browser.MustPage("about:blank")
			defer page.MustClose()
			page.MustSetViewport(1280, 800, 1, false)
			page.MustSetDocumentContent(strings.Replace(settingsFixture, "{{BODY}}", settingsBody(tc.collapsed), 1))
			p := page.Timeout(90 * time.Second)

			start := time.Now()
			require.NoError(t, setNoteSettings(p, tc.visibility, tc.contentType))

			want := tc.visibility
			if want == "" {
				want = "公开可见"
			}
			shown, err := elementText(p, visibilitySelector)
			require.NoError(t, err)
			assert.Equal(t, want, shown)

			clicked := page.MustEval(`() => window.CLICKED.join(",")`).Str()
			if tc.contentType == "" {
				assert.NotContains(t, clicked, "declaration", "没请求声明就不点它")
				assert.Equal(t, "添加内容类型声明", page.MustElement(`[data-select="declaration"]`).MustText())
				assert.Less(t, time.Since(start), 5*time.Second)
				return
			}
			assert.Equal(t, tc.contentType, shownContentType(p))
		})
	}
}
