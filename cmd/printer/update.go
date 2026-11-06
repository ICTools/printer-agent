package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"print-agent/internal/envfile"
	"print-agent/internal/selfupdate"
	"print-agent/internal/service"
)

func repo() string {
	if r := os.Getenv("PRINTER_REPO"); r != "" {
		return r
	}
	return selfupdate.DefaultRepo
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "Only compare the installed version with the latest release")
	tag := fs.String("version", "", "Install this release tag (e.g. v1.2.0) instead of the latest, also to downgrade")
	wait := fs.Duration("wait", 20*time.Second, "How long the restarted service must stay up before the update is kept")
	_ = fs.Parse(args)

	if runtime.GOOS != "linux" {
		return errors.New("update is only supported on Linux")
	}
	// PRINTER_GITHUB_TOKEN may be set in the config (private repository).
	_ = envfile.Load(paths().ConfigFile)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client := selfupdate.NewClient(repo())
	var rel selfupdate.Release
	var err error
	if *tag != "" {
		rel, err = client.ByTag(ctx, *tag)
	} else {
		rel, err = client.Latest(ctx)
	}
	if err != nil {
		return err
	}

	p := paths()
	dir := filepath.Dir(p.Bin)
	refused := readRefused(dir)

	decision, err := selfupdate.Decide(version, rel.TagName)
	if err != nil {
		return err
	}
	if *tag != "" {
		msg, install := describeExplicit(version, rel.TagName)
		fmt.Println(msg)
		if !install || *check {
			return nil
		}
	} else {
		switch decision {
		case selfupdate.UpToDate:
			fmt.Printf("Installed %s, latest release %s: up to date.\n", version, rel.TagName)
		case selfupdate.UpdateAvailable:
			fmt.Printf("Installed %s, latest release %s: update available.\n", version, rel.TagName)
		case selfupdate.DevBuild:
			fmt.Printf("Installed %s (development build), latest release %s.\n", version, rel.TagName)
		}
		if decision != selfupdate.UpToDate && refused == rel.TagName {
			fmt.Printf("%s was rolled back on this PC after failing to start: skipped.\n"+
				"Install it anyway with `printer update --version %s` once the cause is fixed.\n", rel.TagName, rel.TagName)
			return nil
		}
		if *check {
			if decision == selfupdate.UpdateAvailable {
				fmt.Println("Run `printer update` to install it.")
			}
			return nil
		}
		if decision == selfupdate.UpToDate {
			return nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	unlock, err := selfupdate.Lock(dir)
	if err != nil {
		return err
	}
	defer unlock()

	asset := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Downloading %s %s...\n", asset, rel.TagName)
	staged, err := selfupdate.Stage(ctx, client, rel, asset, dir)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(staged) }() // no-op once installed
	fmt.Println("sha256 checksum verified.")
	if got, err := selfupdate.Probe(ctx, staged); err != nil {
		return err
	} else if got != rel.TagName {
		return fmt.Errorf("downloaded binary reports version %q, expected %s", got, rel.TagName)
	}

	return installAndRestart(ctx, staged, p.Bin, rel.TagName, *wait)
}

// describeExplicit explains what `update --version` will do and whether to install.
func describeExplicit(current, target string) (string, bool) {
	if current == target {
		return fmt.Sprintf("%s is already installed.", target), false
	}
	cv, errC := selfupdate.ParseVersion(current)
	tv, errT := selfupdate.ParseVersion(target)
	switch {
	case errC != nil || errT != nil:
		return fmt.Sprintf("Installed %s, installing %s.", current, target), true
	case tv.Compare(cv) < 0:
		return fmt.Sprintf("Installed %s: downgrade to %s.", current, target), true
	default:
		return fmt.Sprintf("Installed %s: upgrade to %s.", current, target), true
	}
}

// The tag of a release rolled back by the automatic health check, so the
// daily timer does not reinstall it every night.
const refusedFile = ".printer-refused-version"

func readRefused(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, refusedFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeRefused(dir, tag string) {
	_ = os.WriteFile(filepath.Join(dir, refusedFile), []byte(tag+"\n"), 0o644)
}

func clearRefused(dir string) {
	_ = os.Remove(filepath.Join(dir, refusedFile))
}

// installAndRestart swaps the binary in and, when the service was running,
// restarts it and rolls back if the new version does not stay up.
func installAndRestart(ctx context.Context, staged, bin, newVersion string, wait time.Duration) error {
	sd := newSystemd()
	props, err := sd.Show(ctx, service.Agent)
	wasActive := err == nil && props.Active()

	if err := selfupdate.Swap(staged, bin); err != nil {
		return fmt.Errorf("install %s: %w", tildePath(bin), err)
	}
	fmt.Printf("Installed %s at %s (previous binary kept as %s).\n",
		newVersion, tildePath(bin), tildePath(bin+selfupdate.PrevSuffix))

	if !wasActive {
		clearRefused(filepath.Dir(bin))
		fmt.Println("The service was not running: it was not started (`printer start`).")
		return nil
	}
	healthErr := restartAndWatch("restart", wait)
	if healthErr == nil {
		clearRefused(filepath.Dir(bin))
		return nil
	}
	writeRefused(filepath.Dir(bin), newVersion)

	fmt.Fprintf(os.Stderr, "The new version does not stay up: %v\nRolling back...\n", healthErr)
	if err := selfupdate.Rollback(bin); err != nil {
		return fmt.Errorf("rollback failed: %w (the previous binary is %s)", err, tildePath(bin+selfupdate.PrevSuffix))
	}
	if err := restartAndWatch("restart", wait); err != nil {
		return fmt.Errorf("rolled back to the previous binary, but it does not stay up either: %w"+
			" — probably not the update (network, API, config): check `printer log -n 100`", err)
	}
	return fmt.Errorf("update to %s rolled back, the previous version is running again", newVersion)
}

func cmdRollback(args []string) error {
	p := paths()
	unlock, err := selfupdate.Lock(filepath.Dir(p.Bin))
	if err != nil {
		return err
	}
	defer unlock()
	if err := selfupdate.Rollback(p.Bin); err != nil {
		return err
	}
	fmt.Printf("Restored the previous binary (now: %s).\n", probeVersion(p.Bin))
	sd := newSystemd()
	if props, err := sd.Show(context.Background(), service.Agent); err == nil && props.Active() {
		return restartAndWatch("restart", 10*time.Second)
	}
	return nil
}

func probeVersion(bin string) string {
	v, err := selfupdate.Probe(context.Background(), bin)
	if err != nil {
		return "unknown version"
	}
	return v
}
