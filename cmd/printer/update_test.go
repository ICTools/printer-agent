package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"print-agent/internal/selfupdate"
	"print-agent/internal/service"
)

// fakeSystemd simulates a running agent; after the restarts listed in
// crashAfter, it reports a crash loop.
func fakeSystemd(t *testing.T, crashAfter map[int]bool) *[]string {
	t.Helper()
	var calls []string
	restarts := 0
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 1 && args[1] == "restart" {
			restarts++
		}
		if len(args) > 1 && args[1] == "show" {
			if crashAfter[restarts] {
				return "ActiveState=activating\nSubState=auto-restart\nNRestarts=1\nResult=exit-code\n", nil
			}
			return "ActiveState=active\nSubState=running\nNRestarts=0\n", nil
		}
		return "", nil
	}
	old, oldStep := newSystemd, healthPollStep
	newSystemd = func() *service.Systemd { return &service.Systemd{Run: runner, Sleep: func(time.Duration) {}} }
	healthPollStep = time.Millisecond
	t.Cleanup(func() { newSystemd, healthPollStep = old, oldStep })
	return &calls
}

func stage(t *testing.T, dir, content string) (bin, staged string) {
	t.Helper()
	bin = filepath.Join(dir, "printer")
	staged = filepath.Join(dir, ".printer-update-x")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, staged
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallAndRestartKeepsHealthyUpdate(t *testing.T) {
	fakeSystemd(t, nil)
	dir := t.TempDir()
	bin, staged := stage(t, dir, "new")
	writeRefused(dir, "v1.1.0")
	if err := installAndRestart(context.Background(), staged, bin, "v1.1.0", 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if read(t, bin) != "new" || read(t, bin+selfupdate.PrevSuffix) != "old" {
		t.Fatal("update not kept")
	}
	if readRefused(dir) != "" {
		t.Fatal("a successful install must clear the refused version")
	}
}

func TestInstallAndRestartRollsBackCrashingUpdate(t *testing.T) {
	calls := fakeSystemd(t, map[int]bool{1: true}) // first restart (new binary) crashes
	dir := t.TempDir()
	bin, staged := stage(t, dir, "new")
	err := installAndRestart(context.Background(), staged, bin, "v1.1.0", 5*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("err = %v", err)
	}
	if read(t, bin) != "old" {
		t.Fatal("previous binary not restored")
	}
	if readRefused(dir) != "v1.1.0" {
		t.Fatal("the rolled back version must be remembered")
	}
	n := 0
	for _, c := range *calls {
		if strings.Contains(c, "restart print-agent.service") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("restarts = %d, want 2 (new, then rollback)", n)
	}
}

func TestInstallAndRestartReportsWhenOldVersionFailsToo(t *testing.T) {
	fakeSystemd(t, map[int]bool{1: true, 2: true})
	dir := t.TempDir()
	bin, staged := stage(t, dir, "new")
	err := installAndRestart(context.Background(), staged, bin, "v1.1.0", 5*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "does not stay up either") {
		t.Fatalf("err = %v", err)
	}
}

func TestDescribeExplicit(t *testing.T) {
	cases := []struct {
		current, target, want string
		install               bool
	}{
		{"v1.2.0", "v1.1.0", "downgrade to v1.1.0", true},
		{"v1.1.0", "v1.2.0", "upgrade to v1.2.0", true},
		{"v1.2.0", "v1.2.0", "already installed", false},
		{"dev", "v1.2.0", "installing v1.2.0", true},
	}
	for _, c := range cases {
		msg, install := describeExplicit(c.current, c.target)
		if !strings.Contains(msg, c.want) || install != c.install {
			t.Errorf("%s -> %s: %q %v", c.current, c.target, msg, install)
		}
	}
}
