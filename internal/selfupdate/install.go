package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// PrevSuffix names the copy of the previous binary kept next to the current one.
const PrevSuffix = ".prev"

// Stage downloads the asset and its checksum into dir (which must be on the
// same filesystem as the final binary so the swap is an atomic rename),
// verifies the sha256 and makes the file executable. The caller removes the
// returned file if it does not install it.
func Stage(ctx context.Context, c *Client, rel Release, assetName, dir string) (string, error) {
	asset, err := rel.FindAsset(assetName)
	if err != nil {
		return "", err
	}
	sumsAsset, err := rel.FindAsset(ChecksumsAsset)
	if err != nil {
		return "", err
	}
	var sums strings.Builder
	if err := c.Download(ctx, sumsAsset, &sums); err != nil {
		return "", err
	}
	expected, err := ExpectedChecksum([]byte(sums.String()), assetName)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp(dir, ".printer-update-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := c.Download(ctx, asset, f); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fail(err)
	}
	if err := VerifyFile(path, expected); err != nil {
		return fail(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fail(err)
	}
	return path, nil
}

// Probe runs "<bin> version --short" and returns what it prints: it proves
// the binary executes on this machine and reports the expected version.
func Probe(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version", "--short").Output()
	if err != nil {
		return "", fmt.Errorf("new binary does not run: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Swap atomically replaces target with staged. The binary it replaces is kept
// as target+PrevSuffix for Rollback. A running process keeps executing its
// old inode, so swapping the binary of a running service is safe.
func Swap(staged, target string) error {
	prev := target + PrevSuffix
	if _, err := os.Stat(target); err == nil {
		if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := linkOrCopy(target, prev); err != nil {
			return fmt.Errorf("keep previous binary: %w", err)
		}
	}
	return os.Rename(staged, target)
}

// Rollback exchanges target and target+PrevSuffix: the previous binary
// becomes current and the current one is kept as the new ".prev", so a
// second Rollback undoes the first.
func Rollback(target string) error {
	prev := target + PrevSuffix
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("no previous binary to restore (%s): %w", prev, err)
	}
	hold := target + ".rollback"
	_ = os.Remove(hold)
	hasCurrent := false
	if _, err := os.Stat(target); err == nil {
		if err := linkOrCopy(target, hold); err != nil {
			return err
		}
		hasCurrent = true
	}
	if err := os.Rename(prev, target); err != nil {
		_ = os.Remove(hold)
		return err
	}
	if hasCurrent {
		return os.Rename(hold, prev)
	}
	return nil
}

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Lock takes an exclusive, non-blocking lock so a manual update and the
// daily timer never run at the same time.
func Lock(dir string) (func(), error) {
	path := filepath.Join(dir, ".printer-update.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another update is running (%s)", path)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
