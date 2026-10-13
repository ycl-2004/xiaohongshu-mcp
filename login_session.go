package main

import "sync"

// loginSessions 管理「已发出二维码、还在等扫码」的登录会话。
//
// 取一次二维码就要留一个浏览器活着等扫码，否则检测不到登录、也存不了 cookie。
// 但没有任何东西拦着重复调用，于是每调一次就多一个浏览器活到超时为止。
// 这里的约束是：同一时刻只保留一个待扫码会话，开新的就把旧的关掉。
type loginSessions struct {
	mu     sync.Mutex
	seq    uint64
	cancel func()
	state  string // 最近一个会话的状态（loginState*），调用方轮询它，不用为了看结果再开浏览器
}

// 登录会话的状态。Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.
const (
	loginStateNone    = "none"    // 还没取过二维码
	loginStateWaiting = "waiting" // 二维码已发出，等扫码
	loginStateSuccess = "success" // 扫上了，cookie 已保存
	loginStateExpired = "expired" // 到时间没扫上
	loginStateFailed  = "failed"  // 扫上了但 cookie 没存下来
)

// start 结束上一个待扫码会话（如果有），登记新的，返回本次会话的序号。
// 序号用于 finish 判断自己是不是仍然是当前会话。
func (l *loginSessions) start(cancel func()) uint64 {
	l.mu.Lock()
	prev := l.cancel
	l.seq++
	seq := l.seq
	l.cancel = cancel
	l.state = loginStateWaiting
	l.mu.Unlock()

	// 放到锁外调用：取消动作会触发对方 goroutine 的收尾，避免相互等待
	if prev != nil {
		prev()
	}
	return seq
}

// finish 会话自己结束时清理登记、记下结果。仅当它仍是当前会话才动，
// 否则会把后来者的登记抹掉，导致后来者永远不会被 start 关闭；被取代的会话的结果也不该盖掉后来者的「等扫码」。
func (l *loginSessions) finish(seq uint64, state string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.seq == seq {
		l.cancel = nil
		l.state = state
	}
}

// snapshot 当前会话的序号和状态；还没取过二维码时状态是 none。
func (l *loginSessions) snapshot() (uint64, string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == "" {
		return l.seq, loginStateNone
	}
	return l.seq, l.state
}
