// Package site 小红书站点：中文站 xiaohongshu.com 和海外站 rednote.com。
//
// 两个站点是两套独立的登录状态（cookie 域名、创作中心会话、可用功能都不同），
// 一个进程只服务一个站点；会话文件按站点标记，不同站点的会话拒绝加载。
// 这里不依赖项目里其他包，configs、cookies、xiaohongshu 都能引用它。
//
// Added in ycl-2004/xiaohongshu-mcp; see NOTICE.
package site

import (
	"fmt"
	"strings"
)

// Site 一个站点的差异项。主站、创作中心等地址都由根域名推出。
type Site struct {
	Name   string // 写进会话文件、XHS_SITE 用的名字
	Domain string // 根域名
	// CreatorSessionCookie 创作中心已登录时浏览器里会有的 cookie
	CreatorSessionCookie string
}

var (
	CN      = Site{Name: "cn", Domain: "xiaohongshu.com", CreatorSessionCookie: "galaxy_creator_session_id"}
	Rednote = Site{Name: "rednote", Domain: "rednote.com", CreatorSessionCookie: "id_token"}
)

// EnvName 选择站点的环境变量；没设就是中文站，和加站点之前的行为一致。
const EnvName = "XHS_SITE"

// Parse 按名字取站点；空字符串是中文站，不认识的名字报错（不静默回落）。
func Parse(name string) (Site, error) {
	switch name {
	case "", CN.Name:
		return CN, nil
	case Rednote.Name:
		return Rednote, nil
	}
	return Site{}, fmt.Errorf("%s=%q 不认识，只能是 %s 或 %s", EnvName, name, CN.Name, Rednote.Name)
}

var current = CN

// Set 入口层启动时设一次。
func Set(s Site) { current = s }

// Current 本进程服务的站点。
func Current() Site { return current }

// WebURL 主站地址，path 以 / 开头（空串是首页）。
func (s Site) WebURL(path string) string { return "https://www." + s.Domain + path }

// CreatorURL 创作中心地址，path 以 / 开头。
func (s Site) CreatorURL(path string) string { return "https://creator." + s.Domain + path }

// Owns host 是否属于这个站点（根域名本身或它的子域名）。
func (s Site) Owns(host string) bool {
	return host == s.Domain || strings.HasSuffix(host, "."+s.Domain)
}
