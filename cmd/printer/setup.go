package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	printagent "print-agent"
	"print-agent/internal/envfile"
	"print-agent/internal/receipt"
	"print-agent/internal/selfupdate"
	"print-agent/internal/service"
)

// cmdSetup installs or repairs everything; it is idempotent and never
// overwrites an existing config. install.sh downloads the binary and runs it.
func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	noStart := fs.Bool("no-start", false, "Do not (re)start the service")
	_ = fs.Parse(args)

	ctx := context.Background()
	p := paths()
	sd := service.New()

	fmt.Println("[1/6] Binary")
	if err := installSelf(p.Bin); err != nil {
		return err
	}
	if err := migrateLegacyBinary(p); err != nil {
		return err
	}

	fmt.Println("[2/6] Config")
	if err := ensureConfig(p); err != nil {
		return err
	}

	fmt.Println("[3/6] systemd user units")
	for name, content := range map[string][]byte{
		service.Agent:         printagent.ServiceUnit,
		service.UpdateService: printagent.UpdateServiceUnit,
		service.UpdateTimer:   printagent.UpdateTimerUnit,
	} {
		changed, err := writeIfChanged(filepath.Join(p.UnitDir, name), content, 0o644)
		if err != nil {
			return err
		}
		if changed {
			fmt.Printf("  wrote %s\n", tildePath(filepath.Join(p.UnitDir, name)))
		}
	}
	if err := sd.Ctl(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("%w\n  (run setup from a normal login session of the till user, not via sudo)", err)
	}
	data, _ := os.ReadFile(p.ConfigFile)
	_, problems := envfile.Validate(data)
	configOK := !envfile.HasFatal(problems)
	if configOK {
		if err := sd.Ctl(ctx, "enable", service.Agent); err != nil {
			return err
		}
		fmt.Printf("  %s enabled at boot\n", service.Agent)
	} else {
		// Enabling it now would crash-loop at every boot until the config is filled in.
		fmt.Printf("  %s will be enabled at boot once the config is valid\n", service.Agent)
	}

	fmt.Println("[4/6] Start at boot without login (linger)")
	if on, err := lingerEnabled(ctx); err == nil && on {
		fmt.Println("  already on")
	} else if u, err := user.Current(); err == nil {
		if out, err := exec.CommandContext(ctx, "loginctl", "enable-linger", u.Username).CombinedOutput(); err != nil {
			fmt.Printf("  could not enable it (%s): run `sudo loginctl enable-linger %s`\n",
				strings.TrimSpace(string(out)), u.Username)
		} else {
			fmt.Println("  enabled")
		}
	}

	fmt.Println("[5/6] Checks")
	runChecks(p)

	fmt.Println("[6/6] Service")
	if *noStart {
		fmt.Println("  not started (--no-start)")
		return nil
	}
	if !configOK {
		printProblems(problems)
		fmt.Println("\n  Fill in the config, the service starts when you save it:")
		fmt.Println("    printer config edit")
		return nil
	}
	return restartAndWatch("restart", 15*time.Second)
}

// installSelf copies the running binary to bin unless it already is bin.
func installSelf(bin string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if target, err := filepath.EvalSymlinks(bin); err == nil && target == exe {
		fmt.Printf("  %s %s already in place\n", tildePath(bin), version)
		return nil
	}
	dir := filepath.Dir(bin)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	in, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".printer-setup-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if err := selfupdate.Swap(tmp.Name(), bin); err != nil {
		return err
	}
	fmt.Printf("  installed %s %s\n", tildePath(bin), version)
	return nil
}

// migrateLegacyBinary handles a PC installed by the old install.sh: the
// binary ~/.local/bin/print-agent is kept as print-agent.legacy and replaced
// by a symlink to printer, so anything still calling print-agent keeps working.
func migrateLegacyBinary(p layout) error {
	info, err := os.Lstat(p.LegacyBin)
	if errors.Is(err, os.ErrNotExist) {
		return os.Symlink("printer", p.LegacyBin)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	backup := p.LegacyBin + ".legacy"
	if err := os.Rename(p.LegacyBin, backup); err != nil {
		return err
	}
	if err := os.Symlink("printer", p.LegacyBin); err != nil {
		return err
	}
	fmt.Printf("  old binary moved to %s, %s now points to printer\n", tildePath(backup), tildePath(p.LegacyBin))
	return nil
}

func ensureConfig(p layout) error {
	if err := os.MkdirAll(p.ConfigDir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(p.ConfigFile); err == nil {
		// It holds the API secret: readable by the till user only.
		_ = os.Chmod(p.ConfigFile, 0o600)
		fmt.Printf("  kept existing %s\n", tildePath(p.ConfigFile))
		return nil
	}
	if err := os.WriteFile(p.ConfigFile, printagent.EnvTemplate, 0o600); err != nil {
		return err
	}
	fmt.Printf("  created %s from the template\n", tildePath(p.ConfigFile))
	return nil
}

func writeIfChanged(path string, content []byte, mode os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func runChecks(p layout) {
	ok := true
	binDir := filepath.Dir(p.Bin)
	if !pathContains(os.Getenv("PATH"), binDir) {
		ok = false
		fmt.Printf("  %s is not in PATH yet: open a new terminal (Ubuntu adds it at login), or use %s\n",
			tildePath(binDir), tildePath(p.Bin))
	}
	if devices, _ := receipt.DetectPrinters(); len(devices) > 0 {
		for _, d := range devices {
			if !d.IsAvailable {
				ok = false
				fmt.Printf("  %s is not writable: `sudo usermod -aG lp $USER` then reboot\n", d.Path)
			}
		}
	} else {
		fmt.Println("  no printer detected right now (plugged in and powered?)")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		ok = false
		fmt.Println("  python3 not found: needed for labels and stickers (`sudo apt install python3-pil`)")
	}
	if _, err := exec.LookPath("brother_ql"); err != nil {
		fmt.Println("  brother_ql not found: needed only for Brother QL labels (see README)")
	}
	if ok {
		fmt.Println("  OK")
	}
}

func pathContains(pathEnv, dir string) bool {
	for _, d := range filepath.SplitList(pathEnv) {
		if filepath.Clean(d) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
