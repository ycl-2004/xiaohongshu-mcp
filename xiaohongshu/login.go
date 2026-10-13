package xiaohongshu

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
	"github.com/xpzouying/xiaohongshu-mcp/site"
)

type LoginAction struct {
	page *rod.Page
}

func NewLogin(page *rod.Page) *LoginAction {
	return &LoginAction{page: page}
}

func (a *LoginAction) CheckLoginStatus(ctx context.Context) (bool, error) {
	// 加超时保护：只是查登录态的快速检查，不应无限挂（登录扫码的等待在 Login/WaitForLogin 里）
	pp := a.page.Context(ctx).Timeout(30 * time.Second)
	pp.MustNavigate(site.Current().WebURL("/explore")).MustWaitLoad()

	time.Sleep(1 * time.Second)

	exists, _, err := pp.Has(`.main-container .user .link-wrapper .channel`)
	if err != nil {
		return false, errors.Wrap(err, "check login status failed")
	}

	if !exists {
		return false, errors.Wrap(err, "login status element not found")
	}

	return true, nil
}

// CurrentUser 当前登录用户的基础信息。
type CurrentUser struct {
	Nickname string `json:"nickname"`
	UserID   string `json:"userId"`
}

// CurrentUser 从当前页面的 __INITIAL_STATE__ 读取登录用户信息。
// 需在 CheckLoginStatus 之后调用：复用已加载的 explore 页，不做额外导航。
func (a *LoginAction) CurrentUser(ctx context.Context) (*CurrentUser, error) {
	pp := a.page.Context(ctx).Timeout(10 * time.Second)

	res, err := pp.Eval(`() => {
		const u = window.__INITIAL_STATE__ && window.__INITIAL_STATE__.user;
		const info = u && u.userInfo && u.userInfo.value !== undefined ? u.userInfo.value : (u && u.userInfo);
		if (!info || info.guest) return "";
		return JSON.stringify({nickname: info.nickname, userId: info.userId || info.user_id});
	}`)
	if err != nil {
		return nil, errors.Wrap(err, "read current user state failed")
	}

	raw := res.Value.String()
	if raw == "" {
		return nil, errors.New("current user not found in page state")
	}

	var user CurrentUser
	if err := json.Unmarshal([]byte(raw), &user); err != nil {
		return nil, errors.Wrap(err, "unmarshal current user failed")
	}

	return &user, nil
}

func (a *LoginAction) Login(ctx context.Context) error {
	pp := a.page.Context(ctx)

	// 导航到小红书首页，这会触发二维码弹窗
	pp.MustNavigate(site.Current().WebURL("/explore")).MustWaitLoad()

	time.Sleep(2 * time.Second)

	if exists, _, _ := pp.Has(".main-container .user .link-wrapper .channel"); exists {
		return nil
	}

	pp.MustElement(".main-container .user .link-wrapper .channel")

	return nil
}

func (a *LoginAction) FetchQrcodeImage(ctx context.Context) (string, bool, error) {
	pp := a.page.Context(ctx)

	// 导航到小红书首页，这会触发二维码弹窗
	pp.MustNavigate(site.Current().WebURL("/explore")).MustWaitLoad()

	time.Sleep(2 * time.Second)

	if exists, _, _ := pp.Has(".main-container .user .link-wrapper .channel"); exists {
		return "", true, nil
	}

	// 中文站没登录时自动弹出登录框；海外站不弹，要先点侧栏的「登录」（2026-10-13 实测）。
	// 页面上另有一个「下载 App」的二维码，所以只认登录框里的。Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.
	if has, _, _ := pp.Has(".login-container .qrcode-img"); !has {
		button, err := pp.Timeout(qrcodeTimeout).Element("button.login-btn")
		if err != nil {
			return "", false, errors.Wrap(err, "没有登录框，也找不到「登录」按钮")
		}
		if err := humanize.Click(button); err != nil {
			return "", false, errors.Wrap(err, "点「登录」失败")
		}
	}
	img, err := pp.Timeout(qrcodeTimeout).Element(".login-container .qrcode-img")
	if err != nil {
		return "", false, errors.Wrap(err, "登录框里没有二维码")
	}
	src, err := img.Attribute("src")
	if err != nil {
		return "", false, errors.Wrap(err, "get qrcode src failed")
	}
	if src == nil || len(*src) == 0 {
		return "", false, errors.New("qrcode src is empty")
	}

	return *src, false, nil
}

// WaitForLogin 等扫码登上：页面出现登录后的用户入口就算。
//
// 海外站实测过「手机显示登录成功、网页不跟进」（2026-10-11）：会话 cookie 已经换了，页面没刷新。
// 所以另外盯着会话 cookie，变了之后页面还是没跟进，就刷新一次再看。只在 cookie 变了之后刷新：
// 提前刷新会换掉用户手里那张二维码。Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.
func (a *LoginAction) WaitForLogin(ctx context.Context) bool {
	pp := a.page.Context(ctx)
	before := sessionCookies(pp)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var changedAt time.Time
	reloaded := false
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if has, _, err := pp.Has(".main-container .user .link-wrapper .channel"); err == nil && has {
				return true
			}
			if reloaded {
				continue
			}
			if changedAt.IsZero() {
				if now := sessionCookies(pp); now != "" && now != before {
					changedAt = time.Now()
				}
				continue
			}
			if time.Since(changedAt) > followGrace {
				logrus.Info("会话 cookie 已经换了、页面没跟进：刷新一次确认登录")
				reloaded = true
				if err := pp.Reload(); err == nil {
					_ = pp.WaitLoad()
				}
			}
		}
	}
}

// qrcodeTimeout 等登录框和二维码出现的上限：原来没有上限，选择器对不上时请求会一直挂着。
const qrcodeTimeout = 30 * time.Second

// followGrace 会话 cookie 变了之后，给页面自己跟进的时间；过了还没跟进才刷新。
const followGrace = 3 * time.Second

// sessionCookieNames 登录后会换成新值的会话 cookie：中文站 web_session，海外站 id_token。
var sessionCookieNames = map[string]bool{"web_session": true, "id_token": true}

// sessionCookies 当前页面的会话 cookie，名字和值拼成一串，只用来比较有没有变（不记日志）。
func sessionCookies(page *rod.Page) string {
	cookies, err := page.Cookies(nil)
	if err != nil {
		return ""
	}
	var parts []string
	for _, c := range cookies {
		if sessionCookieNames[c.Name] {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, ";")
}
