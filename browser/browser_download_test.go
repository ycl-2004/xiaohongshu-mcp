package browser

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Linux arm64 不走 CDN：CloakBrowser 的 GitHub Release，版本和 sha256 都钉在源码里。
// Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.
func TestPlatformAsset_LinuxArm64(t *testing.T) {
	asset, ok := platformAsset("linux", "arm64")
	require.True(t, ok)
	assert.Equal(t, linuxArm64Version, asset.Version, "arm64 有自己的版本源")
	assert.Equal(t, "https://github.com/CloakHQ/CloakBrowser/releases/download/chromium-v"+
		linuxArm64Version+"/cloakbrowser-linux-arm64.tar.gz", asset.URL)
	assert.Equal(t, "chrome", asset.Bin)
	assert.Len(t, asset.SHA256, 64)
}

func TestPlatformAsset_CDNPlatforms(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, archive, bin string }{
		{"linux", "amd64", "linux-x64.tar.xz", "chrome"},
		{"darwin", "arm64", "macos-arm64.dmg", "Chromium"},
		{"windows", "amd64", "windows-x64.zip", "chrome.exe"},
	} {
		asset, ok := platformAsset(tc.goos, tc.goarch)
		require.True(t, ok, tc.goos+"/"+tc.goarch)
		assert.Equal(t, browserVersion, asset.Version)
		assert.Equal(t, browserURL(tc.archive), asset.URL)
		assert.Equal(t, tc.archive, asset.Archive)
		assert.Equal(t, tc.bin, asset.Bin)
		assert.Empty(t, asset.SHA256, "CDN 的哈希从同目录的 SHA256SUMS 取")
	}
}

func TestPlatformAsset_Unsupported(t *testing.T) {
	for _, p := range [][2]string{{"darwin", "amd64"}, {"linux", "386"}, {"windows", "arm64"}} {
		_, ok := platformAsset(p[0], p[1])
		assert.False(t, ok, p[0]+"/"+p[1])
	}
}

// 钉了 sha256 就只用它比对（不联网取 SHA256SUMS）。
func TestVerifySHA256_Pinned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	require.NoError(t, os.WriteFile(path, []byte("browser bytes"), 0o600))
	sum := sha256.Sum256([]byte("browser bytes"))

	good := browserAsset{Archive: "a.tar.gz", SHA256: hex.EncodeToString(sum[:])}
	assert.NoError(t, verifySHA256(path, good))

	bad := browserAsset{Archive: "a.tar.gz", SHA256: hex.EncodeToString(make([]byte, 32))}
	assert.ErrorContains(t, verifySHA256(path, bad), "SHA256 不匹配")
}

func writeTarGz(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())
}

// CloakBrowser 的 Linux 包是 tar.gz，chrome 在包的最外层。
func TestExtractArchive_TarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(t.TempDir(), "cloakbrowser-linux-arm64.tar.gz")
	writeTarGz(t, archive, map[string]string{"chrome": "#!/bin/sh\n", "locales/zh-CN.pak": "x"})

	require.NoError(t, extractArchive(archive, dir))
	assert.Equal(t, filepath.Join(dir, "chrome"), findBinary(dir, "chrome"))
	assert.FileExists(t, filepath.Join(dir, "locales", "zh-CN.pak"))
}

func TestExtractArchive_RefusesPathOutsideDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "browser")
	require.NoError(t, os.Mkdir(dir, 0o755))
	archive := filepath.Join(root, "evil.tar.gz")
	writeTarGz(t, archive, map[string]string{"../escaped": "x"})

	assert.ErrorContains(t, extractArchive(archive, dir), "跳出了解压目录")
	assert.NoFileExists(t, filepath.Join(root, "escaped"))
}

// 链接指到解压目录外（相对或绝对）一律拒绝；指在里面的照常建。
func TestExtractArchive_RefusesSymlinkOutsideDir(t *testing.T) {
	for _, link := range []string{"../outside", "/etc"} {
		root := t.TempDir()
		dir := filepath.Join(root, "browser")
		require.NoError(t, os.Mkdir(dir, 0o755))
		archive := filepath.Join(root, "evil.tar.gz")
		writeTarGzEntries(t, archive, []*tar.Header{{Name: "lnk", Linkname: link, Typeflag: tar.TypeSymlink, Mode: 0o777}})
		assert.ErrorContains(t, extractArchive(archive, dir), "链接指到了解压目录外", link)
	}

	dir := t.TempDir()
	archive := filepath.Join(t.TempDir(), "ok.tar.gz")
	writeTarGzEntries(t, archive, []*tar.Header{{Name: "lib/libx.so.1", Linkname: "libx.so", Typeflag: tar.TypeSymlink, Mode: 0o777}})
	require.NoError(t, extractArchive(archive, dir))
	got, err := os.Readlink(filepath.Join(dir, "lib", "libx.so.1"))
	require.NoError(t, err)
	assert.Equal(t, "libx.so", got)
}

func writeTarGzEntries(t *testing.T, path string, headers []*tar.Header) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		require.NoError(t, tw.WriteHeader(h))
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())
}
