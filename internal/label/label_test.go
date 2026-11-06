package label

import (
	"bytes"
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

func TestDefaultScriptPathPrefersExistingEnvPath(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "print.py")
	if err := os.WriteFile(custom, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LABEL_SCRIPT_PATH", custom)
	if got := defaultScriptPath("print.py"); got != custom {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultScriptPathIgnoresMissingEnvPath(t *testing.T) {
	t.Setenv("LABEL_SCRIPT_PATH", filepath.Join(t.TempDir(), "deleted-clone", "print.py"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	got := defaultScriptPath("print.py")
	if got == os.Getenv("LABEL_SCRIPT_PATH") {
		t.Fatal("a missing LABEL_SCRIPT_PATH must not be used")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("fallback %q: %v", got, err)
	}
}

func TestDefaultScriptPathFallsBackToEmbeddedScripts(t *testing.T) {
	t.Setenv("STICKER_SCRIPT_PATH", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	got := defaultScriptPath("print_sticker.py")
	if filepath.Base(got) != "print_sticker.py" || !filepath.IsAbs(got) {
		t.Fatalf("got %q", got)
	}
	// print.py and sticker.png must sit next to it: print.py reads ./sticker.png.
	dir := filepath.Dir(got)
	for name, want := range embeddedFiles() {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(b, want) || len(want) == 0 {
			t.Errorf("%s: %d bytes, %v", name, len(b), err)
		}
	}
}
