package browser

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ulikunitz/xz"
)

// 内置浏览器的下载分发地址。
const browserCDNBase = "https://cdn.one-world.ai/browsers"

// browserVersion 是内置浏览器的唯一版本源。升级只改 browser_version.txt 一处，Go 与 Dockerfile 同读。
//
//go:embed browser_version.txt
var browserVersionRaw string

var browserVersion = strings.TrimSpace(browserVersionRaw)

// linuxArm64Version 是 Linux arm64 内置浏览器的唯一版本源。上面的 CDN 没有 Linux arm64 版，这个平台直接用
// CloakBrowser 在 GitHub Releases 上发布的免费版（免费版的 Linux arm64 只出到这一版，比 CDN 的版本旧）。
// 升级时改 browser_version_linux_arm64.txt 和 linuxArm64SHA256。
// Modified for ycl-2004/xiaohongshu-mcp; see NOTICE.
//
//go:embed browser_version_linux_arm64.txt
var linuxArm64VersionRaw string

var linuxArm64Version = strings.TrimSpace(linuxArm64VersionRaw)

// linuxArm64SHA256 是 Linux arm64 压缩包的 sha256，取自该版本 Release 的 SHA256SUMS。钉在源码里：
// Release 的附件可以被替换，和它从同一处下载的 SHA256SUMS 证明不了什么。
const linuxArm64SHA256 = "8b71ce53b4fd131327331a31fba3835d71882d19bfaabde78dd0f5390bd16f45"

const cloakReleaseBase = "https://github.com/CloakHQ/CloakBrowser/releases/download"

// browserAsset 是一个平台的内置浏览器：版本、下载地址、压缩包名、解压后的可执行文件名，
// 以及期望的 sha256（空：从下载地址同目录的 SHA256SUMS 取）。
type browserAsset struct {
	Version string
	URL     string
	Archive string
	Bin     string
	SHA256  string
}

func browserURL(name string) string {
	return browserCDNBase + "/" + browserVersion + "/" + name
}

func cdnAsset(archive, bin string) browserAsset {
	return browserAsset{Version: browserVersion, URL: browserURL(archive), Archive: archive, Bin: bin}
}

// platformAsset 返回 goos/goarch 对应的内置浏览器；第二个返回值为 false 表示这个平台没有预编译的浏览器。
func platformAsset(goos, goarch string) (browserAsset, bool) {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return cdnAsset("macos-arm64.dmg", "Chromium"), true
	case "linux/amd64":
		return cdnAsset("linux-x64.tar.xz", "chrome"), true
	case "linux/arm64":
		archive := "cloakbrowser-linux-arm64.tar.gz"
		return browserAsset{
			Version: linuxArm64Version,
			URL:     cloakReleaseBase + "/chromium-v" + linuxArm64Version + "/" + archive,
			Archive: archive,
			Bin:     "chrome",
			SHA256:  linuxArm64SHA256,
		}, true
	case "windows/amd64":
		return cdnAsset("windows-x64.zip", "chrome.exe"), true
	}
	return browserAsset{}, false
}

func browserCacheDir(version string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "xiaohongshu-mcp", "browser", version), nil
}

// EnsureBrowser 确保本地存在内置浏览器二进制，返回其路径。
// 已缓存则直接返回；否则下载 → 校验 SHA256 → 解压。当前平台无预编译二进制时返回 error。
func EnsureBrowser() (string, error) {
	asset, ok := platformAsset(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return "", fmt.Errorf("当前平台 %s/%s 无预编译浏览器，暂不支持", runtime.GOOS, runtime.GOARCH)
	}

	cacheDir, err := browserCacheDir(asset.Version)
	if err != nil {
		return "", err
	}

	// 已缓存：遍历查找二进制
	if bin := findBinary(cacheDir, asset.Bin); bin != "" {
		return bin, nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	// 下载（重试 3 次）
	logrus.Infof("首次运行：下载内置浏览器 %s（%s，约 140-250MB，仅一次）...", asset.Version, asset.Archive)
	archivePath := filepath.Join(cacheDir, asset.Archive)
	var dlErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if dlErr = downloadFile(asset.URL, archivePath); dlErr == nil {
			break
		}
		logrus.Warnf("下载失败（第 %d/3 次）: %v", attempt, dlErr)
		_ = os.Remove(archivePath)
		time.Sleep(2 * time.Second)
	}
	if dlErr != nil {
		return "", fmt.Errorf("下载内置浏览器失败: %w\n"+
			"  本项目只用内置浏览器，缺它不继续。请检查网络后重试；\n"+
			"  离线环境可手动下载 %s，解压到 %s 后重启。", dlErr, asset.URL, cacheDir)
	}
	defer os.Remove(archivePath)

	// 校验 SHA256（本地已成分发点，必须校验完整性）
	if err := verifySHA256(archivePath, asset); err != nil {
		return "", fmt.Errorf("校验失败: %w", err)
	}

	logrus.Infof("解压内置浏览器 ...")
	if err := extractArchive(archivePath, cacheDir); err != nil {
		return "", fmt.Errorf("解压失败: %w", err)
	}

	bin := findBinary(cacheDir, asset.Bin)
	if bin == "" {
		return "", fmt.Errorf("解压后未找到二进制 %s", asset.Bin)
	}
	logrus.Infof("内置浏览器就绪: %s", bin)
	return bin, nil
}

// verifySHA256 校验压缩包的哈希：源码里钉了 sha256 就用它，否则下载 CDN 同目录的 SHA256SUMS。
func verifySHA256(archivePath string, asset browserAsset) error {
	want := asset.SHA256
	if want == "" {
		var err error
		if want, err = fetchExpectedSHA(asset.Archive); err != nil {
			return err
		}
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%s SHA256 不匹配：期望 %s，实际 %s", asset.Archive, want, got)
	}
	return nil
}

func fetchExpectedSHA(asset string) (string, error) {
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(browserURL("SHA256SUMS"))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取 SHA256SUMS: HTTP %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		// 格式：<hash>␠␠<filename>
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS 中未找到 %s", asset)
}

func findBinary(dir, binName string) string {
	var found string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Base(path) == binName {
			found = path
			return io.EOF // 提前结束
		}
		return nil
	})
	if found != "" {
		if err := os.Chmod(found, 0o755); err != nil {
			logrus.Debugf("chmod %s: %v", found, err)
		}
	}
	return found
}

func downloadFile(url, dst string) error {
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractArchive(archivePath, destDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.xz"):
		return extractTarXz(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGz(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".dmg"):
		return extractDmg(archivePath, destDir)
	}
	return fmt.Errorf("不支持的压缩格式: %s", archivePath)
}

func extractTarXz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	xzr, err := xz.NewReader(f)
	if err != nil {
		return err
	}
	return extractTar(xzr, destDir)
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()
	return extractTar(gzr, destDir)
}

// extractTar 把 tar 流解到 destDir；路径跳出 destDir 的条目直接报错。
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, hdr.Name)
		if !within(destDir, target) {
			return fmt.Errorf("压缩包里的路径跳出了解压目录: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			// 链接只许相对、不带 ..：每一跳都只能指到自己所在目录或更深，串起来也出不了解压目录
			// （只查字面上的落点不够：a -> . 再 a/b -> .. 能绕出去）
			if !safeLink(hdr.Linkname) {
				return fmt.Errorf("压缩包里的链接可能指到解压目录外: %s -> %s", hdr.Name, hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// safeLink 判断链接目标是否只指向同级或更深：不能是绝对路径，也不能有 .. 这一段。
func safeLink(link string) bool {
	if link == "" || filepath.IsAbs(link) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(link), "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// within 判断 target 是否在 dir 里面（含 dir 本身）。
func within(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func extractZip(archivePath, destDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		target := filepath.Join(destDir, zf.Name)
		if !within(destDir, target) {
			return fmt.Errorf("压缩包里的路径跳出了解压目录: %s", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractDmg(archivePath, destDir string) error {
	mountPoint, err := os.MkdirTemp("", "bx-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mountPoint)

	if out, err := exec.Command("hdiutil", "attach", archivePath, "-nobrowse", "-mountpoint", mountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("hdiutil attach: %v: %s", err, out)
	}
	defer exec.Command("hdiutil", "detach", mountPoint, "-quiet").Run()

	var appPath string
	entries, _ := os.ReadDir(mountPoint)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".app") {
			appPath = filepath.Join(mountPoint, e.Name())
			break
		}
	}
	if appPath == "" {
		return fmt.Errorf("dmg 内未找到 .app")
	}
	dstApp := filepath.Join(destDir, filepath.Base(appPath))
	if out, err := exec.Command("cp", "-R", appPath, dstApp).CombinedOutput(); err != nil {
		return fmt.Errorf("拷贝 .app: %v: %s", err, out)
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", dstApp).Run()
	return nil
}
