package node

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// lcuReleaseAPI is the GitHub release Persona installs LCU from. The archive is checked against the release's
// .sha256 sidecar before anything from it runs. Install uses the release's own install.sh --runtime-only, which
// records the ChatGPT app's Node and does not register Claude, Codex, or any other harness (those would show
// LCU's own approval UI).
var lcuReleaseAPI = "https://api.github.com/repos/amontlabs/lcu/releases/latest"

// lcuInstallRun runs the checked installer. Tests record the root instead of executing it.
var lcuInstallRun = runLCUInstaller

// lcuArchiveMost is a ceiling for one release archive (the published ones are about 6 MB).
const lcuArchiveMost = 80 << 20

func installLCU(ctx context.Context, step func(string)) error {
	if installOS != "darwin" && installOS != "linux" {
		return errors.New("LCU 目前只在 Mac 和 Linux 上安装")
	}
	if _, err := lcuAssetSuffix(installOS, installArch); err != nil {
		return err
	}
	if lcuBin() != "" {
		return errors.New("已经装好了")
	}
	if !chatgptApp() {
		return errors.New("先安装官方 ChatGPT 桌面版（https://chatgpt.com/download ）。LCU 用它自带的运行时，Persona 不会下载或登录它")
	}
	step("下载 LCU")
	asset, err := lcuReleaseAsset(ctx)
	if err != nil {
		return err
	}
	archive, err := fetchLimited(ctx, asset.URL, lcuArchiveMost, "")
	if err != nil {
		return fmt.Errorf("下载 LCU：%w", err)
	}
	step("校验安装包")
	sum, err := fetchLimited(ctx, asset.SumURL, 1<<16, "")
	if err != nil {
		return fmt.Errorf("下载校验：%w", err)
	}
	if err := checkSHA256(archive, sum); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "persona-lcu-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	step("解压")
	root, err := extractLCU(dir, archive)
	if err != nil {
		return err
	}
	if err := checkLCUInstaller(filepath.Join(root, "scripts", "install.sh")); err != nil {
		return err
	}
	step("安装 LCU")
	return lcuInstallRun(ctx, root)
}

// lcuInstallArgs is the only command the install runs. It must stay --runtime-only: an --agent flag would register
// a harness and put approvals on that harness's own screen.
func lcuInstallArgs(root string) []string {
	return []string{"/bin/bash", filepath.Join(root, "scripts", "install.sh"), "--runtime-only", "--yes"}
}

func runLCUInstaller(ctx context.Context, root string) error {
	args := lcuInstallArgs(root)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(lcuLastLines(msg, 8))
	}
	return nil
}

type lcuAsset struct {
	URL    string
	SumURL string
}

func lcuReleaseAsset(ctx context.Context) (lcuAsset, error) {
	suffix, err := lcuAssetSuffix(installOS, installArch)
	if err != nil {
		return lcuAsset{}, err
	}
	body, err := fetchLimited(ctx, lcuReleaseAPI, 1<<20, "application/vnd.github+json")
	if err != nil {
		return lcuAsset{}, fmt.Errorf("查询 LCU 版本：%w", err)
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.Unmarshal(body, &rel) != nil {
		return lcuAsset{}, errors.New("LCU 的版本说明不是 JSON")
	}
	var archiveName, archiveURL, sumURL string
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, "lcu-") && strings.HasSuffix(a.Name, "-"+suffix+".tar.gz") {
			archiveName, archiveURL = a.Name, a.URL
		}
	}
	for _, a := range rel.Assets {
		if a.Name == archiveName+".sha256" {
			sumURL = a.URL
		}
	}
	if archiveURL == "" || sumURL == "" {
		return lcuAsset{}, fmt.Errorf("这个版本没有 %s 的安装包", suffix)
	}
	return lcuAsset{URL: archiveURL, SumURL: sumURL}, nil
}

// lcuAssetSuffix is the platform part of a release file name, without the version.
func lcuAssetSuffix(goos, goarch string) (string, error) {
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "darwin-arm64", nil
	case goos == "linux" && goarch == "arm64":
		return "linux-arm64", nil
	case goos == "linux" && (goarch == "amd64" || goarch == "x64"):
		return "linux-x64", nil
	default:
		return "", errors.New("LCU 没有这个系统的安装包")
	}
}

func fetchLimited(ctx context.Context, url string, most int64, accept string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "persona")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, most+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > most {
		return nil, errors.New("下载超过了大小限制")
	}
	return body, nil
}

func checkSHA256(archive, sidecar []byte) error {
	want := ""
	for _, f := range strings.Fields(string(sidecar)) {
		if len(f) == 64 {
			want = strings.ToLower(f)
			break
		}
	}
	if want == "" {
		return errors.New("校验文件里没有 SHA256")
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != want {
		return errors.New("安装包的校验没通过")
	}
	return nil
}

// extractLCU unpacks the archive and returns the directory that contains scripts/install.sh. Names that leave the
// destination are refused.
func extractLCU(dir string, archive []byte) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", errors.New("安装包不是 gzip")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var root string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		name := pathClean(h.Name)
		if name == "" || strings.HasPrefix(name, "..") || filepath.IsAbs(h.Name) {
			return "", errors.New("安装包里的路径不安全")
		}
		dest := filepath.Join(dir, name)
		if !strings.HasPrefix(dest, dir+string(os.PathSeparator)) && dest != dir {
			return "", errors.New("安装包里的路径不安全")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return "", err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, lcuArchiveMost)); err != nil {
				f.Close()
				return "", err
			}
			if err := f.Close(); err != nil {
				return "", err
			}
			if strings.HasSuffix(name, "/scripts/install.sh") || name == "scripts/install.sh" {
				root = filepath.Dir(filepath.Dir(dest))
			}
		default:
			// Symlinks and the rest are not needed to run the installer, and are not followed.
		}
	}
	if root == "" {
		return "", errors.New("安装包里没有 scripts/install.sh")
	}
	return root, nil
}

func pathClean(name string) string {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	return pathCleanSlash(name)
}

func pathCleanSlash(name string) string {
	parts := strings.Split(name, "/")
	var out []string
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			return ".."
		}
		out = append(out, p)
	}
	return strings.Join(out, "/")
}

func checkLCUInstaller(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.New("安装包里没有 scripts/install.sh")
	}
	if !bytes.HasPrefix(b, []byte("#!")) {
		return errors.New("安装器不是脚本")
	}
	if !bytes.Contains(b, []byte("--runtime-only")) {
		return errors.New("安装器不像 LCU 的 install.sh")
	}
	return nil
}

func lcuLastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
