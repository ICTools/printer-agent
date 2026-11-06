package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultRepo is the GitHub repository the releases are published to.
const DefaultRepo = "ICTools/printer-agent"

// ChecksumsAsset is the name of the sha256sum file attached to each release.
const ChecksumsAsset = "checksums.txt"

// AssetName is the release asset holding the binary for a platform.
func AssetName(goos, goarch string) string {
	return fmt.Sprintf("printer_%s_%s", goos, goarch)
}

// Asset is a file attached to a GitHub release.
type Asset struct {
	Name string `json:"name"`
	// URL is the API URL; with "Accept: application/octet-stream" it serves
	// the file, also for a private repository (with a token).
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release is the subset of a GitHub release the updater uses.
type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// FindAsset returns the asset with that exact name.
func (r Release) FindAsset(name string) (Asset, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("release %s has no asset %s", r.TagName, name)
}

// Client talks to the GitHub REST API.
type Client struct {
	Repo    string
	BaseURL string // https://api.github.com, overridable in tests
	Token   string // optional: needed only for a private repository
	HTTP    *http.Client
	// MaxSize bounds every download (DefaultMaxSize when 0).
	MaxSize int64
	// AllowInsecure accepts http:// and any host: local test servers only
	// (PRINTER_UPDATE_INSECURE=1). Otherwise every request, redirects included,
	// must be https to GitHub (or to the PRINTER_GITHUB_API host).
	AllowInsecure bool
}

// DefaultMaxSize is far above the size of a release binary (~10 MB).
const DefaultMaxSize = 100 << 20

// githubHosts serve the API, the release pages and the asset redirects.
func githubHost(host string) bool {
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// checkURL refuses anything but https to GitHub (or the configured API host).
func (c *Client) checkURL(u *neturl.URL) error {
	if c.AllowInsecure {
		return nil
	}
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-https URL %s", u.Redacted())
	}
	host := u.Hostname()
	if githubHost(host) {
		return nil
	}
	if base, err := neturl.Parse(c.BaseURL); err == nil && base.Hostname() == host {
		return nil
	}
	return fmt.Errorf("refusing download from unexpected host %s", host)
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if err := c.checkURL(req.URL); err != nil {
		return nil, err
	}
	hc := *c.HTTP
	hc.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return c.checkURL(r.URL)
	}
	return hc.Do(req)
}

// NewClient returns a client for repo, authenticated when a token is found
// (see TokenFromEnv).
func NewClient(repo string) *Client {
	base := "https://api.github.com"
	if v := os.Getenv("PRINTER_GITHUB_API"); v != "" { // GitHub Enterprise, or a local test server
		base = v
	}
	return &Client{
		Repo:          repo,
		BaseURL:       base,
		Token:         TokenFromEnv(),
		HTTP:          &http.Client{Timeout: 5 * time.Minute},
		AllowInsecure: os.Getenv("PRINTER_UPDATE_INSECURE") == "1",
	}
}

// TokenFromEnv returns PRINTER_GITHUB_TOKEN, GH_TOKEN or GITHUB_TOKEN, else
// the token of the gh CLI when it is installed and logged in, else "".
func TokenFromEnv() string {
	for _, k := range []string{"PRINTER_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if _, err := exec.LookPath("gh"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

// Latest returns the latest published (non-draft, non-prerelease) release.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	return c.release(ctx, "releases/latest")
}

// ByTag returns the release of a given tag.
func (c *Client) ByTag(ctx context.Context, tag string) (Release, error) {
	return c.release(ctx, "releases/tags/"+tag)
}

func (c *Client) release(ctx context.Context, path string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/repos/%s/%s", strings.TrimRight(c.BaseURL, "/"), c.Repo, path), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	c.authorize(req)
	resp, err := c.do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		hint := "no release published yet?"
		if c.Token == "" {
			hint += " If the repository is private, set GITHUB_TOKEN or run `gh auth login`."
		}
		return Release{}, fmt.Errorf("GitHub: no matching release for %s (%s)", c.Repo, hint)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Release{}, fmt.Errorf("GitHub API: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Release{}, fmt.Errorf("GitHub API: %w", err)
	}
	return r, nil
}

// Download streams an asset to w.
func (c *Client) Download(ctx context.Context, a Asset, w io.Writer) error {
	url := a.BrowserDownloadURL
	if c.Token != "" && a.URL != "" {
		url = a.URL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if url == a.URL {
		c.authorize(req)
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", a.Name, resp.Status)
	}
	limit := c.MaxSize
	if limit <= 0 {
		limit = DefaultMaxSize
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("download %s: larger than %d bytes, refused", a.Name, limit)
	}
	return nil
}

func (c *Client) authorize(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "printer-agent-updater")
}
