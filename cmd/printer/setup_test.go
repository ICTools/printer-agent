package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	printagent "print-agent"
)

func testLayout(t *testing.T) layout {
	t.Helper()
	home := t.TempDir()
	l := layout{
		Bin:        filepath.Join(home, ".local", "bin", "printer"),
		LegacyBin:  filepath.Join(home, ".local", "bin", "print-agent"),
		ConfigDir:  filepath.Join(home, ".config", "print-agent"),
		ConfigFile: filepath.Join(home, ".config", "print-agent", "env"),
		UnitDir:    filepath.Join(home, ".config", "systemd", "user"),
	}
	if err := os.MkdirAll(filepath.Dir(l.Bin), 0o755); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestMigrateLegacyBinary(t *testing.T) {
	l := testLayout(t)
	if err := os.WriteFile(l.LegacyBin, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyBinary(l); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(l.LegacyBin); err != nil || target != "printer" {
		t.Fatalf("print-agent -> %q, %v", target, err)
	}
	if b, err := os.ReadFile(l.LegacyBin + ".legacy"); err != nil || string(b) != "old build" {
		t.Fatalf("legacy backup = %q, %v", b, err)
	}
	// Idempotent: a second setup leaves the symlink and the backup alone.
	if err := migrateLegacyBinary(l); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(l.LegacyBin + ".legacy"); string(b) != "old build" {
		t.Fatal("backup overwritten")
	}
}

func TestEnsureConfigKeepsExistingFile(t *testing.T) {
	l := testLayout(t)
	if err := os.MkdirAll(l.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("PRINT_AGENT_API_URL=https://app.example.test\n")
	if err := os.WriteFile(l.ConfigFile, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureConfig(l); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(l.ConfigFile)
	info, _ := os.Stat(l.ConfigFile)
	if string(got) != string(existing) || info.Mode().Perm() != 0o600 {
		t.Fatalf("config = %q mode %v", got, info.Mode())
	}
}

func TestEnsureConfigCreatesFromTemplate(t *testing.T) {
	l := testLayout(t)
	if err := ensureConfig(l); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(l.ConfigFile)
	if string(got) != string(printagent.EnvTemplate) {
		t.Fatal("config not created from the template")
	}
}

func TestWriteIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "units", "a.service")
	for i, want := range []bool{true, false} {
		changed, err := writeIfChanged(path, []byte("x"), 0o644)
		if err != nil || changed != want {
			t.Fatalf("call %d: changed=%v err=%v", i, changed, err)
		}
	}
	if changed, _ := writeIfChanged(path, []byte("y"), 0o644); !changed {
		t.Fatal("new content not written")
	}
}

func TestPathContains(t *testing.T) {
	if !pathContains("/usr/bin:/home/u/.local/bin/", "/home/u/.local/bin") {
		t.Fatal("not found")
	}
	if pathContains("/usr/bin", "/home/u/.local/bin") {
		t.Fatal("false positive")
	}
}

func TestEmbeddedUnitRunsPrinter(t *testing.T) {
	if !contains(string(printagent.ServiceUnit), "ExecStart=%h/.local/bin/printer run") ||
		!contains(string(printagent.ServiceUnit), "EnvironmentFile=%h/.config/print-agent/env") {
		t.Fatal("service unit must run ~/.local/bin/printer with the existing config path")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
