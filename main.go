package main

// Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.

import (
	"errors"
	"flag"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

// version 构建版本号，发布时通过 -ldflags "-X main.version=vX.Y.Z" 注入。
var version = "dev"

func main() {
	var (
		headless bool
		port     string
		access   accessConfig
		lockWait time.Duration
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&port, "port", "127.0.0.1:18060", "监听地址，只允许本机回环地址")
	flag.StringVar(&access.ReadToken, "token-read", "", "只读 token，留空则读取 AUTH_TOKEN_READ")
	flag.StringVar(&access.WriteToken, "token-write", "", "读写 token，留空则读取 AUTH_TOKEN_WRITE")
	flag.BoolVar(&access.EnableMCP, "enable-mcp", false, "开启 /mcp 端点（需要写 token）")
	flag.DurationVar(&lockWait, "lock-wait", 15*time.Minute, "浏览器操作排队的最长等待，超时返回 503 BUSY")
	flag.Parse()
	if access.ReadToken == "" {
		access.ReadToken = os.Getenv("AUTH_TOKEN_READ")
	}
	if access.WriteToken == "" {
		access.WriteToken = os.Getenv("AUTH_TOKEN_WRITE")
	}

	logrus.Infof("xiaohongshu-mcp version: %s", version)

	// 鉴权和监听地址不合格就不启动，也不去下载浏览器。
	if err := access.validate(); err != nil {
		logrus.Fatalf("%v", err)
	}
	if err := validateListen(port); err != nil {
		logrus.Fatalf("%v", err)
	}
	// 站点要在读会话文件之前定下来：不同站点的会话拒绝加载。
	s, err := configs.UseSiteFromEnv()
	if err != nil {
		logrus.Fatalf("%v", err)
	}
	logrus.Infof("site: %s (%s)", s.Name, s.Domain)
	if _, err := cookies.NewLoadCookie(cookies.GetCookiesFilePath()).LoadCookies(); errors.Is(err, cookies.ErrSiteMismatch) {
		logrus.Fatalf("%v", err)
	}

	// 只用内置浏览器。启动时就备好，缺它直接退出，不拖到第一个请求才失败。
	binPath, err := browser.EnsureBrowser()
	if err != nil {
		logrus.Fatalf("%v", err)
	}
	logrus.Infof("using browser binary: %s", binPath)

	configs.InitHeadless(headless)
	// 入口层解析出 seed 和代理，经 configs 透传给浏览器工厂。
	// seed 取值：环境变量 > 会话文件 > 新生成并写回，保证同一账号每次启动一致。
	configs.SetFingerprintSeed(configs.ResolveFingerprintSeed(
		cookies.NewLoadCookie(cookies.GetCookiesFilePath())))
	configs.SetProxy(configs.ProxyFromEnv())

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService()

	// 创建并启动应用服务器
	appServer := NewAppServer(xiaohongshuService, access)
	appServer.lockWait = lockWait
	if err := appServer.Start(port); err != nil {
		logrus.Fatalf("failed to run server: %v", err)
	}
}
