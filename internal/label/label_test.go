package label

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteScriptOnceWritesExecutableCopy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scripts", "abc")
	path, err := writeScriptOnce(dir, "print.py", []byte("print('hi')\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "print('hi')\n" {
		t.Fatalf("content = %q, %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("script not executable: %v", info.Mode())
	}
	// Second call with the same content is a no-op; different content replaces it.
	if _, err := writeScriptOnce(dir, "print.py", []byte("print('hi')\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeScriptOnce(dir, "print.py", []byte("print('new')\n")); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "print('new')\n" {
		t.Fatalf("content not replaced: %q", got)
	}
}

func TestDefaultScriptPathPrefersEnv(t *testing.T) {
	t.Setenv("LABEL_SCRIPT_PATH", "/opt/custom/print.py")
	if got := defaultScriptPath("print.py"); got != "/opt/custom/print.py" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultScriptPathFallsBackToEmbeddedScript(t *testing.T) {
	t.Setenv("LABEL_SCRIPT_PATH", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	got := defaultScriptPath("print_sticker.py")
	if filepath.Base(got) != "print_sticker.py" || !filepath.IsAbs(got) {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal(err)
	}
}
