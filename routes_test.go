package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPStatelessSinglePost 固定「/mcp 接受不带 initialize 握手的单次 POST」这一契约。
//
// 契约由 routes.go 里一行 Stateless 支撑，丢掉它编译和其他单测都不会报错。
func TestMCPStatelessSinglePost(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), withMCP()))
	server := httptest.NewServer(router)
	defer server.Close()

	post := func(t *testing.T, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+testAccess.WriteToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		return resp
	}

	// 只用 tools/list：它不碰浏览器，而契约丢失时它恰好就是失败点——
	// 有状态模式下无 session 调用 tools/list 会被回以「握手前不允许调用」。
	resp := post(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	require.Nil(t, result.Error, "无握手的 tools/list 不应报错")
	assert.NotEmpty(t, result.Result.Tools, "应返回已注册的工具")
}

// TestNotificationToolsRegistered 固定通知相关工具已注册到 MCP。
//
// 三个工具的注册各是 registerTools 里一段独立代码，漏掉任何一个编译都不会报错，
// 只有真正调用时才会发现工具不存在。
func TestNotificationToolsRegistered(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), withMCP()))
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+testAccess.WriteToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	names := make(map[string]bool, len(result.Result.Tools))
	for _, tool := range result.Result.Tools {
		names[tool.Name] = true
	}

	for _, want := range []string{"get_unread_count", "list_notifications", "reply_notification", "like_notification"} {
		assert.True(t, names[want], "工具 %s 应已注册", want)
	}
}

func withMCP() accessConfig {
	cfg := testAccess
	cfg.EnableMCP = true
	return cfg
}

// TestRegisteredRoutes 固定路由表：只注册 Marketing_Agent 用到的路由。
// 读路由表而不是发请求：这些 handler 会真的起浏览器访问小红书，单测里不能碰。
func TestRegisteredRoutes(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), testAccess))

	registered := make(map[string]bool)
	for _, r := range router.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /health",
		"GET /api/v1/login/status",
		"GET /api/v1/access/probe",
		"POST /api/v1/feeds/search",
		"POST /api/v1/feeds/detail",
		"POST /api/v1/user/profile",
		"GET /api/v1/user/me",
		"GET /api/v1/notifications/unread",
		"POST /api/v1/notifications/list",
		"GET /api/v1/chat/conversations",
		"POST /api/v1/publish",
		"POST /api/v1/feeds/comment",
		"POST /api/v1/feeds/comment/reply",
	} {
		assert.True(t, registered[want], "路由 %s 应已注册", want)
	}
	for _, gone := range []string{
		"GET /api/v1/login/qrcode",
		"DELETE /api/v1/login/cookies",
		"POST /api/v1/publish_video",
		"GET /api/v1/feeds/list",
		"POST /api/v1/feeds/like",
		"POST /api/v1/feeds/favorite",
		"POST /api/v1/notifications/reply",
		"POST /api/v1/notifications/like",
		"POST /mcp",
	} {
		assert.False(t, registered[gone], "路由 %s 不应注册", gone)
	}
}

// TestRouteScopes 写路由拒绝读 token；鉴权通过后请求体为空，在起浏览器之前就被参数校验拦下（400）。
func TestRouteScopes(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), withMCP()))
	read, write := "Bearer "+testAccess.ReadToken, "Bearer "+testAccess.WriteToken

	tests := []struct {
		name          string
		method        string
		path          string
		authorization string
		want          int
	}{
		{"健康检查公开", http.MethodGet, "/health", "", http.StatusOK},
		{"读路由要 token", http.MethodPost, "/api/v1/feeds/detail", "", http.StatusUnauthorized},
		{"读路由接受读 token", http.MethodPost, "/api/v1/feeds/detail", read, http.StatusBadRequest},
		{"写路由拒绝读 token", http.MethodPost, "/api/v1/feeds/comment", read, http.StatusForbidden},
		{"写路由接受写 token", http.MethodPost, "/api/v1/feeds/comment", write, http.StatusBadRequest},
		{"评论缺幂等 key 被参数校验拦下", http.MethodPost, "/api/v1/feeds/comment", write, http.StatusBadRequest},
		{"回复拒绝读 token", http.MethodPost, "/api/v1/feeds/comment/reply", read, http.StatusForbidden},
		{"发布拒绝读 token", http.MethodPost, "/api/v1/publish", read, http.StatusForbidden},
		{"MCP 拒绝读 token", http.MethodPost, "/mcp", read, http.StatusForbidden},
		{"没有 CORS 预检", http.MethodOptions, "/api/v1/feeds/detail", "", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(""))
			request.Header.Set("Content-Type", "application/json")
			if tt.authorization != "" {
				request.Header.Set("Authorization", tt.authorization)
			}
			router.ServeHTTP(recorder, request)
			assert.Equal(t, tt.want, recorder.Code)
			assert.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

func TestMCPDisabledByDefault(t *testing.T) {
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), testAccess))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer "+testAccess.WriteToken)
	router.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}
