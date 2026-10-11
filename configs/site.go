package configs

import (
	"os"

	"github.com/xpzouying/xiaohongshu-mcp/site"
)

// UseSiteFromEnv 按 XHS_SITE 设定本进程服务的站点（cn 或 rednote，没设是 cn）。
// 不认识的值返回错误，入口层据此拒绝启动。env 读取集中在配置层。
// Added in ycl-2004/xiaohongshu-mcp; see NOTICE.
func UseSiteFromEnv() (site.Site, error) {
	s, err := site.Parse(os.Getenv(site.EnvName))
	if err != nil {
		return site.Site{}, err
	}
	site.Set(s)
	return s, nil
}
