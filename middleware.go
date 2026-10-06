package main

// Modified for ycl-2004/xiaohongshu-mcp; see NOTICE. 上游是一枚可为空的 token 加 CORS *；
// 这里改成读、写两枚必填 token，且不开 CORS（调用方只有本机进程）。

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// minTokenLength 两枚 token 的最短长度，防止弱口令。
const minTokenLength = 32

// accessConfig 鉴权配置：读 token 只能调读路由，写 token 什么都能调。
type accessConfig struct {
	ReadToken  string
	WriteToken string
	EnableMCP  bool
}

// validate 启动前检查：两枚都要有、够长、互不相同。上游 token 为空时会放行所有请求。
func (a accessConfig) validate() error {
	for name, token := range map[string]string{"读": a.ReadToken, "写": a.WriteToken} {
		if len(token) < minTokenLength {
			return fmt.Errorf("%s token 少于 %d 个字符，拒绝启动", name, minTokenLength)
		}
	}
	if a.ReadToken == a.WriteToken {
		return fmt.Errorf("读、写 token 不能相同，拒绝启动")
	}
	return nil
}

// validateListen 只允许监听本机回环地址。
func validateListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("监听地址 %q 无效: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("只允许监听本机回环地址，%q 不是", addr)
	}
	return nil
}

type scope int

const (
	scopeRead scope = iota
	scopeWrite
)

// scopeMiddleware 读路由接受读或写 token；写路由只接受写 token。
// 没带或不认识的 token 返回 401；读 token 访问写路由返回 403。
func scopeMiddleware(cfg accessConfig, need scope) gin.HandlerFunc {
	read, write := []byte(cfg.ReadToken), []byte(cfg.WriteToken)
	return func(c *gin.Context) {
		scheme, credentials, found := strings.Cut(c.GetHeader("Authorization"), " ")
		given := []byte(strings.TrimLeft(credentials, " "))
		isWrite := found && strings.EqualFold(scheme, "Bearer") && subtle.ConstantTimeCompare(given, write) == 1
		isRead := found && strings.EqualFold(scheme, "Bearer") && subtle.ConstantTimeCompare(given, read) == 1
		switch {
		case isWrite || (isRead && need == scopeRead):
			c.Next()
		case isRead:
			respondError(c, http.StatusForbidden, "FORBIDDEN", "只读 token 不能调用写操作", nil)
			c.Abort()
		default:
			c.Header("WWW-Authenticate", "Bearer")
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "未授权", nil)
			c.Abort()
		}
	}
}

// errorHandlingMiddleware 错误处理中间件
func errorHandlingMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logrus.Errorf("服务器内部错误: %v, path: %s", recovered, c.Request.URL.Path)

		respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR",
			"服务器内部错误", recovered)
	})
}
