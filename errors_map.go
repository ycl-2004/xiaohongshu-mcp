package main

// Added in ycl-2004/xiaohongshu-mcp; see NOTICE.

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

// 失败分类的错误码（调用方据此决定稍后重试、跳过这篇，还是全停等人）。
const (
	codeTimeout         = "TIMEOUT"          // 页面或数据没有在时限内出现
	codeNoteUnavailable = "NOTE_UNAVAILABLE" // 笔记被删、私密或不可见
)

// classifyFailure 把超时、页面拦截和笔记不可见从笼统的 500 里分出来。
// details 可能是 error、panic 的值或 err.Error() 字符串（上游 handler 都这样传）。
func classifyFailure(details any) (status int, code string, ok bool) {
	var text string
	switch v := details.(type) {
	case error:
		var access *xiaohongshu.PageAccessError
		if stderrors.As(v, &access) {
			return http.StatusConflict, access.Code, true
		}
		if stderrors.Is(v, context.DeadlineExceeded) {
			return http.StatusGatewayTimeout, codeTimeout, true
		}
		text = v.Error()
	case string:
		text = v
	case nil:
		return 0, "", false
	default:
		text = fmt.Sprint(v)
	}
	for _, access := range []string{
		xiaohongshu.CodeSecurityVerification,
		xiaohongshu.CodeLoginRequired,
		xiaohongshu.CodeAccountRestricted,
		xiaohongshu.CodeRateLimited,
	} {
		if strings.HasPrefix(text, access+":") {
			return http.StatusConflict, access, true
		}
	}
	switch {
	case strings.Contains(text, "context deadline exceeded"):
		return http.StatusGatewayTimeout, codeTimeout, true
	case strings.Contains(text, "笔记不可访问"):
		return http.StatusGone, codeNoteUnavailable, true
	}
	return 0, "", false
}

// writeFailure 写操作失败的响应体：state 告诉调用方这次到底发没发出。
type writeFailure struct {
	State string `json:"state"`           // not_sent / rejected / unknown
	Stage string `json:"stage,omitempty"` // 失败发生在哪一步
	Cause string `json:"cause,omitempty"`
}

// notPublished 发笔记在点「发布」之前就失败了（回执在点发布前才占位，所以不是 InteractionError 的都在点之前）：
// 记成没发出，码是 INTERACTION_NOT_SENT（同一 key 可以重试），不按超时归成 TIMEOUT——发布要先填完整页表单，
// 点之前卡住只说明这次没发出去（2026-10-20：声明控件点不到，超时被当成「机器慢」，白占了当天唯一一篇的额度）。
// 被页面拦下的照旧用拦截码；点了发布之后的失败本来就是 InteractionError（超时是 INTERACTION_UNKNOWN），原样返回。
func notPublished(err error) error {
	var interaction *xiaohongshu.InteractionError
	var dup *xiaohongshu.DuplicateError
	if stderrors.As(err, &interaction) || stderrors.As(err, &dup) {
		return err
	}
	return &xiaohongshu.InteractionError{Stage: "prepare", State: xiaohongshu.StateNotSent, Cause: err}
}

// respondInteractionError 写操作（评论、回复）的失败响应。
//   - 同一幂等 key 已有未确认成功的记录 → 409 DUPLICATE_OR_UNCERTAIN_COMMENT，state 为原记录状态；
//   - 平台拒绝 → 409，被拦时用页面拦截码，否则 INTERACTION_REJECTED；
//   - 点了发送但结果不明 → 502 INTERACTION_UNKNOWN（被拦时用拦截码，state 仍是 unknown）；
//   - 其余错误都发生在点发送之前（回执在点发送前才占位）→ state=not_sent，码按 classifyFailure 或 INTERACTION_NOT_SENT。
func respondInteractionError(c *gin.Context, message string, err error) {
	var dup *xiaohongshu.DuplicateError
	if stderrors.As(err, &dup) {
		respondError(c, http.StatusConflict, "DUPLICATE_OR_UNCERTAIN_COMMENT", message,
			writeFailure{State: dup.Saved.State, Cause: err.Error()})
		return
	}
	var access *xiaohongshu.PageAccessError
	blocked := stderrors.As(err, &access)
	var interaction *xiaohongshu.InteractionError
	if stderrors.As(err, &interaction) {
		failure := writeFailure{State: interaction.State, Stage: interaction.Stage, Cause: err.Error()}
		switch {
		case blocked:
			respondError(c, http.StatusConflict, access.Code, message, failure)
		case interaction.State == xiaohongshu.StateRejected:
			respondError(c, http.StatusConflict, "INTERACTION_REJECTED", message, failure)
		case interaction.State == xiaohongshu.StateUnknown:
			respondError(c, http.StatusBadGateway, "INTERACTION_UNKNOWN", message, failure)
		default:
			respondError(c, http.StatusBadGateway, "INTERACTION_NOT_SENT", message, failure)
		}
		return
	}
	failure := writeFailure{State: xiaohongshu.StateNotSent, Stage: "prepare", Cause: err.Error()}
	if status, code, ok := classifyFailure(err); ok {
		respondError(c, status, code, message, failure)
		return
	}
	respondError(c, http.StatusBadGateway, "INTERACTION_NOT_SENT", message, failure)
}
