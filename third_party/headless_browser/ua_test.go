package headless_browser

import "testing"

// Windows 画像的 architecture 固定 x86；其他画像留空，交给 CloakBrowser 的补丁。
// Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.
func TestUAArchitecture(t *testing.T) {
	for platform, want := range map[string]string{"windows": "x86", "macos": "", "linux": ""} {
		if got := uaArchitecture(platform); got != want {
			t.Errorf("uaArchitecture(%q) = %q, want %q", platform, got, want)
		}
	}
}
