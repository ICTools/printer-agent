package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	ok := map[string]Version{
		"v1.2.3":        {1, 2, 3, ""},
		"1.2.3":         {1, 2, 3, ""},
		"v0.10.0-rc.1":  {0, 10, 0, "rc.1"},
		" v2.0.0 \n":    {2, 0, 0, ""},
		"v1.0.0-beta-2": {1, 0, 0, "beta-2"},
	}
	for in, want := range ok {
		got, err := ParseVersion(in)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"dev", "", "a82ed4d", "v1.2", "v1.2.3-4-ga82ed4d", "v1.2.3-dirty", "a82ed4d-dirty", "1.2.3.4"} {
		if _, err := ParseVersion(in); err == nil {
			t.Errorf("ParseVersion(%q) accepted", in)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.10.0", "v1.9.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.0-alpha.1", "v1.0.0-beta", -1},
		{"v1.0.0-1", "v1.0.0-alpha", -1},
	}
	for _, c := range cases {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		current, latest string
		want            Decision
	}{
		{"v1.0.0", "v1.1.0", UpdateAvailable},
		{"v1.1.0", "v1.1.0", UpToDate},
		{"v1.2.0", "v1.1.0", UpToDate},
		{"dev", "v1.1.0", DevBuild},
		{"v1.1.0-2-gabc1234", "v1.1.0", DevBuild},
	}
	for _, c := range cases {
		got, err := Decide(c.current, c.latest)
		if err != nil || got != c.want {
			t.Errorf("Decide(%q, %q) = %v, %v; want %v", c.current, c.latest, got, err, c.want)
		}
	}
	if _, err := Decide("v1.0.0", "nightly"); err == nil {
		t.Error("a non-semver release tag must be an error")
	}
}

const sumA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestParseChecksums(t *testing.T) {
	data := []byte(sumA + "  printer_linux_amd64\n" +
		strings.ToUpper(sumA) + " *printer_linux_arm64\n\n# comment\n")
	sums, err := ParseChecksums(data)
	if err != nil {
		t.Fatal(err)
	}
	if sums["printer_linux_amd64"] != sumA || sums["printer_linux_arm64"] != sumA {
		t.Fatalf("sums = %v", sums)
	}
	for _, bad := range []string{"", "nothex  file\n", sumA + "\n", "abcd  file\n"} {
		if _, err := ParseChecksums([]byte(bad)); err == nil {
			t.Errorf("ParseChecksums(%q) accepted", bad)
		}
	}
	if _, err := ExpectedChecksum(data, "printer_darwin_amd64"); err == nil {
		t.Error("missing asset must be an error")
	}
}

func TestVerifyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, sumA); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatch accepted")
	}
}

func TestAssetSelection(t *testing.T) {
	if got := AssetName("linux", "arm64"); got != "printer_linux_arm64" {
		t.Fatalf("AssetName = %q", got)
	}
	rel := Release{TagName: "v1.0.0", Assets: []Asset{
		{Name: "printer_linux_amd64.sig"},
		{Name: "printer_linux_amd64"},
		{Name: "checksums.txt"},
	}}
	a, err := rel.FindAsset("printer_linux_amd64")
	if err != nil || a.Name != "printer_linux_amd64" {
		t.Fatalf("FindAsset = %v, %v", a, err)
	}
	if _, err := rel.FindAsset("printer_linux_arm64"); err == nil {
		t.Fatal("missing asset must be an error")
	}
}

// fakeGitHub serves a release whose binary is content and whose checksums
// file announces sum for it.
func fakeGitHub(t *testing.T, content, sum string) (*Client, Release) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	rel := Release{TagName: "v1.2.0", Assets: []Asset{
		{Name: "printer_linux_amd64", BrowserDownloadURL: srv.URL + "/dl/bin", URL: srv.URL + "/api/bin"},
		{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/dl/sums", URL: srv.URL + "/api/sums"},
	}}
	mux.HandleFunc("/repos/acme/printer/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rel)
	})
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }
	}
	sums := sum + "  printer_linux_amd64\n"
	mux.HandleFunc("/dl/bin", serve(content))
	mux.HandleFunc("/dl/sums", serve(sums))
	mux.HandleFunc("/api/bin", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, content)
	})
	mux.HandleFunc("/api/sums", serve(sums))
	return &Client{Repo: "acme/printer", BaseURL: srv.URL, HTTP: srv.Client()}, rel
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestStageVerifiesChecksum(t *testing.T) {
	content := "#!/bin/sh\necho v1.2.0\n"
	c, _ := fakeGitHub(t, content, sha(content))
	rel, err := c.Latest(context.Background())
	if err != nil || rel.TagName != "v1.2.0" {
		t.Fatalf("Latest = %v, %v", rel, err)
	}
	dir := t.TempDir()
	path, err := Stage(context.Background(), c, rel, "printer_linux_amd64", dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 || filepath.Dir(path) != dir {
		t.Fatalf("staged %s: %v %v", path, info, err)
	}
	if v, err := Probe(context.Background(), path); err != nil || v != "v1.2.0" {
		t.Fatalf("Probe = %q, %v", v, err)
	}
}

func TestStageRejectsTamperedBinary(t *testing.T) {
	c, rel := fakeGitHub(t, "tampered", sha("original"))
	dir := t.TempDir()
	if _, err := Stage(context.Background(), c, rel, "printer_linux_amd64", dir); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("staged file left behind: %v", entries)
	}
}

func TestStageUsesAPIURLWithToken(t *testing.T) {
	content := "binary"
	c, rel := fakeGitHub(t, content, sha(content))
	c.Token = "tok"
	if _, err := Stage(context.Background(), c, rel, "printer_linux_amd64", t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestLatestNotFoundHintsAtToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &Client{Repo: "acme/printer", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestSwapAndRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "printer")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write(target, "old")
	staged := filepath.Join(dir, ".printer-update-1")
	write(staged, "new")

	if err := Swap(staged, target); err != nil {
		t.Fatal(err)
	}
	if read(target) != "new" || read(target+PrevSuffix) != "old" {
		t.Fatal("swap did not keep the previous binary")
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("staged file still present")
	}
	if err := Rollback(target); err != nil {
		t.Fatal(err)
	}
	if read(target) != "old" || read(target+PrevSuffix) != "new" {
		t.Fatal("rollback did not exchange the binaries")
	}
	if err := Rollback(target); err != nil || read(target) != "new" {
		t.Fatalf("second rollback: %v", err)
	}
}

func TestSwapWithoutExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "printer")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Swap(staged, target); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(target); err == nil {
		t.Fatal("rollback without a previous binary must fail")
	}
}

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); err == nil {
		t.Fatal("second lock acquired")
	}
	unlock()
	unlock2, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}
