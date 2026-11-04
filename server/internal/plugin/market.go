package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Plugin markets (docs/plugins.md「插件市场」): a repository or a website publishes an index of its plugins,
// persona-plugins.json; a hub that registered the market lists them, and installs one by downloading its package and
// checking it against the sha256 the index pins.

// IndexFile is a market's index, at the root of a repository or of a website's path.
const IndexFile = "persona-plugins.json"

// Limits of a market.
const (
	IndexMost         = 4 << 20
	IndexPluginsMost  = 500
	MarketsMost       = 32
	marketFetchFor    = 30 * time.Second
	MarketTokenMost   = 4096
	marketTagMost     = 16
	marketPackageMost = 16
)

// MarketSpec is where a market's index is.
type MarketSpec struct {
	Kind string // https | github | git
	// URL: the index's https URL (Kind https), or the repository's (Kind git).
	URL string
	// Owner, Repo, Ref: a GitHub repository (Kind github); Ref "" is the default branch (also of Kind git).
	Owner, Repo, Ref string
}

var githubName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ParseMarket reads github:owner/repo[@ref], or an https URL: of the index itself (ending in .json) or of the
// directory it is in (persona-plugins.json is added).
func ParseMarket(s string) (MarketSpec, error) {
	s = strings.TrimSpace(s)
	if spec, ok, err := parseGit(s); ok {
		return spec, err
	}
	if rest, ok := strings.CutPrefix(s, "github:"); ok {
		repo, ref, _ := strings.Cut(rest, "@")
		owner, name, ok := strings.Cut(repo, "/")
		if !ok || !githubName.MatchString(owner) || !githubName.MatchString(name) || strings.Contains(name, "/") {
			return MarketSpec{}, errors.New("GitHub 市场写成 github:owner/repo 或 github:owner/repo@分支")
		}
		if ref != "" && (strings.ContainsAny(ref, " \t\n?#") || strings.Contains(ref, "..") || len(ref) > 200) {
			return MarketSpec{}, errors.New("分支名不对")
		}
		return MarketSpec{Kind: "github", Owner: owner, Repo: strings.TrimSuffix(name, ".git"), Ref: ref}, nil
	}
	if strings.HasPrefix(s, "https://github.com/") {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(s, "https://github.com/"), "/"), "/")
		if len(parts) == 2 {
			return ParseMarket("github:" + parts[0] + "/" + parts[1])
		}
		if len(parts) >= 4 && parts[2] == "tree" {
			return ParseMarket("github:" + parts[0] + "/" + parts[1] + "@" + strings.Join(parts[3:], "/"))
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return MarketSpec{}, errors.New("市场的地址是 https://… 或 github:owner/repo")
	}
	if u.Scheme != "https" {
		return MarketSpec{}, errors.New("市场只能用 https")
	}
	if u.User != nil {
		return MarketSpec{}, errors.New("地址里不能带账号密码：私有市场填 token")
	}
	u.Fragment = ""
	if !strings.HasSuffix(u.Path, ".json") {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/" + IndexFile
	}
	return MarketSpec{Kind: "https", URL: u.String()}, nil
}

// String is the spec as kept and shown.
func (s MarketSpec) String() string {
	if s.Kind == "git" {
		out := "git:" + s.URL
		if s.Ref != "" {
			out += "#" + s.Ref
		}
		return out
	}
	if s.Kind == "github" {
		out := "github:" + s.Owner + "/" + s.Repo
		if s.Ref != "" {
			out += "@" + s.Ref
		}
		return out
	}
	return s.URL
}

// MarketID is a short stable id of a spec (the market's key in the hub's API).
func MarketID(s MarketSpec) string {
	sum := sha256.Sum256([]byte(s.String()))
	return hex.EncodeToString(sum[:5])
}

// MarketIndex is persona-plugins.json.
type MarketIndex struct {
	MarketVersion int           `json:"market_version"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Homepage      string        `json:"homepage,omitempty"`
	Plugins       []MarketEntry `json:"plugins"`
}

// MarketEntry is one plugin of a market: its manifest (so the permissions are known before downloading) and its
// packages.
type MarketEntry struct {
	Manifest json.RawMessage `json:"manifest"`
	Tags     []string        `json:"tags,omitempty"`
	Packages []MarketPackage `json:"packages"`
	// Parsed is Manifest, read.
	Parsed *Manifest `json:"-"`
}

// MarketPackage is a package of an entry.
type MarketPackage struct {
	// Platforms are the GOOS-GOARCH it runs on; none: any (a package for every platform, or one that runs no code).
	Platforms []string `json:"platforms,omitempty"`
	// URL: relative to the index, an https URL, or (a GitHub market) release:<tag>/<asset>.
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// ParseIndex reads and checks an index. Entries that do not check out are dropped with a warning each (one broken
// plugin does not hide a market); an index that is not one is an error.
func ParseIndex(data []byte) (*MarketIndex, []string, error) {
	if len(data) > IndexMost {
		return nil, nil, fmt.Errorf("索引大于 %d 字节", IndexMost)
	}
	var ix MarketIndex
	if err := json.Unmarshal(data, &ix); err != nil {
		return nil, nil, fmt.Errorf("不是插件市场的索引：%v", err)
	}
	if ix.MarketVersion != 1 {
		return nil, nil, fmt.Errorf("索引的 market_version 是 %d，这个 hub 只认 1", ix.MarketVersion)
	}
	ix.Name = strings.TrimSpace(ix.Name)
	if ix.Name == "" || len([]rune(ix.Name)) > 60 {
		return nil, nil, errors.New("索引要有 name（最多 60 个字）")
	}
	if len([]rune(ix.Description)) > StringMost {
		return nil, nil, errors.New("索引的 description 太长")
	}
	if ix.Homepage != "" && !isHTTPS(ix.Homepage) {
		ix.Homepage = ""
	}
	if len(ix.Plugins) > IndexPluginsMost {
		return nil, nil, fmt.Errorf("索引里多于 %d 个插件", IndexPluginsMost)
	}
	var warnings []string
	seen := map[string]bool{}
	kept := ix.Plugins[:0]
	for i, e := range ix.Plugins {
		m, err := e.check()
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("第 %d 个插件：%v", i+1, err))
		case seen[m.ID]:
			warnings = append(warnings, fmt.Sprintf("插件 %s 出现了两次，只用第一个", m.ID))
		default:
			seen[m.ID] = true
			e.Parsed = m
			kept = append(kept, e)
		}
	}
	ix.Plugins = kept
	return &ix, warnings, nil
}

func (e *MarketEntry) check() (*Manifest, error) {
	if len(e.Manifest) == 0 {
		return nil, errors.New("没有 manifest")
	}
	m, err := Parse(e.Manifest)
	if err != nil {
		return nil, err
	}
	if len(e.Tags) > marketTagMost {
		return nil, errors.New("tags 太多")
	}
	for _, t := range e.Tags {
		if t == "" || len([]rune(t)) > 24 {
			return nil, errors.New("tag 为空或太长")
		}
	}
	if len(e.Packages) == 0 || len(e.Packages) > marketPackageMost {
		return nil, fmt.Errorf("%s：要有 1–%d 个包", m.ID, marketPackageMost)
	}
	for _, p := range e.Packages {
		if b, err := hex.DecodeString(p.SHA256); err != nil || len(b) != sha256.Size {
			return nil, fmt.Errorf("%s：包要有 sha256（64 位十六进制）", m.ID)
		}
		if p.URL == "" || strings.HasPrefix(p.URL, "http://") || strings.Contains(p.URL, "://") && !strings.HasPrefix(p.URL, "https://") {
			return nil, fmt.Errorf("%s：包的地址要是相对路径或 https", m.ID)
		}
	}
	return m, nil
}

func isHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// Platform is this hub's GOOS-GOARCH.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// PackageFor is the entry's package for platform, or nil.
func (e *MarketEntry) PackageFor(platform string) *MarketPackage {
	for i, p := range e.Packages {
		if len(p.Platforms) == 0 || slices.Contains(p.Platforms, platform) {
			return &e.Packages[i]
		}
	}
	return nil
}

// Market is a registered market with what reading it needs.
type Market struct {
	Spec MarketSpec
	// Token, when set, is sent to the market's own origin only (a private repository or site).
	Token string
	// Client and GitHubAPI: tests replace them.
	Client    *http.Client
	GitHubAPI string
	// Dir is where git markets' clones are kept (the hub's data directory).
	Dir string
	// allowLocal lets tests clone a local repository.
	allowLocal bool
}

func (m *Market) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return http.DefaultClient
}

func (m *Market) api() string {
	if m.GitHubAPI != "" {
		return strings.TrimSuffix(m.GitHubAPI, "/")
	}
	return "https://api.github.com"
}

// get fetches u into w (at most limit bytes), with the token when u is the market's own origin.
func (m *Market) get(ctx context.Context, u, accept string, w io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if m.Token != "" && sameOrigin(u, m.origin()) {
		req.Header.Set("Authorization", "Bearer "+m.Token)
	}
	if m.Spec.Kind == "github" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	res, err := m.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound && m.Token == "":
		return errors.New("找不到（404）：地址不对，或者是私有的，要填 token")
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound:
		return fmt.Errorf("没有权限（HTTP %d）：token 不对或没有读这个仓库的权限", res.StatusCode)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(res.Body, limit+1))
	if err == nil && n > limit {
		err = fmt.Errorf("大于 %d 字节", limit)
	}
	return err
}

// origin is where the token may go.
func (m *Market) origin() string {
	if m.Spec.Kind == "github" {
		return m.api()
	}
	return m.Spec.URL
}

func sameOrigin(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && ua.Scheme == ub.Scheme && strings.EqualFold(ua.Host, ub.Host)
}

// contentsURL is a file of a GitHub market's repository, through the API (works for private ones with a token).
func (m *Market) contentsURL(p string) string {
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", m.api(), url.PathEscape(m.Spec.Owner), url.PathEscape(m.Spec.Repo), escapePath(p))
	if m.Spec.Ref != "" {
		u += "?ref=" + url.QueryEscape(m.Spec.Ref)
	}
	return u
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// Fetch reads the market's index.
func (m *Market) Fetch(ctx context.Context) (*MarketIndex, []string, error) {
	limit := marketFetchFor
	if m.Spec.Kind == "git" {
		limit = gitFor // a clone over SSH can be slow
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var b strings.Builder
	var err error
	if m.Spec.Kind == "git" {
		if m.Token != "" {
			return nil, nil, errors.New("git 市场用 hub 机器自己的凭据（SSH key），不收 token")
		}
		if err = m.syncGit(ctx); err == nil {
			err = m.gitFile(IndexFile, &b, IndexMost)
		}
	} else if m.Spec.Kind == "github" {
		err = m.get(ctx, m.contentsURL(IndexFile), "application/vnd.github.raw", &b, IndexMost)
	} else {
		err = m.get(ctx, m.Spec.URL, "application/json", &b, IndexMost)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("读不到 %s：%w", m.Spec, err)
	}
	return ParseIndex([]byte(b.String()))
}

// Download fetches pkg into dir (a fresh staging directory) and checks its sha256; it returns the file and the
// name it goes by (for its type).
func (m *Market) Download(ctx context.Context, pkg *MarketPackage, dir string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	file := filepath.Join(dir, "download")
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	name, err := m.download(ctx, pkg.URL, io.MultiWriter(f, h))
	f.Close()
	if err != nil {
		return "", "", fmt.Errorf("下载 %s：%w", pkg.URL, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, pkg.SHA256) {
		return "", "", fmt.Errorf("下载的包 sha256 是 %s，不是索引里写的 %s", got, strings.ToLower(pkg.SHA256))
	}
	return file, name, nil
}

func (m *Market) download(ctx context.Context, ref string, w io.Writer) (string, error) {
	const octet = "application/octet-stream"
	switch {
	case strings.HasPrefix(ref, "https://"):
		u, _ := url.Parse(ref)
		return u.Path, m.get(ctx, ref, octet, w, PackageMost)
	case strings.HasPrefix(ref, "release:"):
		if m.Spec.Kind != "github" {
			return "", errors.New("release: 只能用在 GitHub 市场")
		}
		tag, asset, ok := strings.Cut(strings.TrimPrefix(ref, "release:"), "/")
		if !ok || tag == "" || asset == "" {
			return "", errors.New("写成 release:<tag>/<附件名>")
		}
		var b strings.Builder
		rel := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", m.api(), url.PathEscape(m.Spec.Owner), url.PathEscape(m.Spec.Repo), url.PathEscape(tag))
		if err := m.get(ctx, rel, "application/vnd.github+json", &b, IndexMost); err != nil {
			return "", fmt.Errorf("Release %s：%w", tag, err)
		}
		var r struct {
			Assets []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"assets"`
		}
		if err := json.Unmarshal([]byte(b.String()), &r); err != nil {
			return "", err
		}
		for _, a := range r.Assets {
			if a.Name == asset {
				return asset, m.get(ctx, a.URL, octet, w, PackageMost)
			}
		}
		return "", fmt.Errorf("Release %s 里没有 %s", tag, asset)
	default:
		clean := path.Clean("/" + ref)[1:]
		if clean == "" || strings.HasPrefix(ref, "/") || clean != ref {
			return "", errors.New("包的相对路径不能跳出市场")
		}
		if m.Spec.Kind == "git" {
			return clean, m.gitFile(clean, w, PackageMost)
		}
		if m.Spec.Kind == "github" {
			return clean, m.get(ctx, m.contentsURL(clean), "application/vnd.github.raw", w, PackageMost)
		}
		base, _ := url.Parse(m.Spec.URL)
		u := base.ResolveReference(&url.URL{Path: clean})
		return u.Path, m.get(ctx, u.String(), octet, w, PackageMost)
	}
}

// MarketOrigin is the origin a plugin installed from a market records: market:<market id>/<plugin id>.
func MarketOrigin(marketID, pluginID string) string { return "market:" + marketID + "/" + pluginID }

// FromMarket is the market a plugin's origin says it came from ("" when not from a market).
func FromMarket(origin string) string {
	rest, ok := strings.CutPrefix(origin, "market:")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// MarketRecord is a market's row (table plugin_markets).
type MarketRecord struct {
	ID          string          `json:"id"`
	Spec        string          `json:"spec"`
	Name        string          `json:"name"`
	TokenSealed string          `json:"-"`
	Index       json.RawMessage `json:"-"`
	Warnings    []string        `json:"warnings"`
	Error       string          `json:"error,omitempty"`
	FetchedAt   *time.Time      `json:"fetched_at,omitempty"`
	AddedAt     time.Time       `json:"added_at"`
}

// MarketStore keeps the markets (the hub's Postgres).
type MarketStore interface {
	PluginMarkets(ctx context.Context) ([]MarketRecord, error)
	PutPluginMarket(ctx context.Context, r MarketRecord) error
	DeletePluginMarket(ctx context.Context, id string) error
}
