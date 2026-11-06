package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"print-agent/internal/envfile"
	"print-agent/internal/receipt"
	"print-agent/internal/service"
)

// Set at build time: go build -ldflags "-X main.version=v1.2.3 -X main.commit=abc1234 -X main.date=..."
var (
	version = "dev"
	commit  = ""
	date    = ""
)

var managementCommands = map[string]func([]string) error{
	"version":    cmdVersion,
	"status":     cmdStatus,
	"start":      func(args []string) error { return cmdStartRestart("start", args) },
	"restart":    func(args []string) error { return cmdStartRestart("restart", args) },
	"stop":       cmdStop,
	"log":        cmdLog,
	"logs":       cmdLog,
	"update":     cmdUpdate,
	"rollback":   cmdRollback,
	"config":     cmdConfig,
	"test":       cmdTest,
	"autoupdate": cmdAutoUpdate,
	"setup":      cmdSetup,
}

// layout is where everything lives on the till PC. The systemd unit uses the
// same fixed paths (%h/.local/bin/printer, %h/.config/print-agent/env).
type layout struct {
	Bin        string // ~/.local/bin/printer
	LegacyBin  string // ~/.local/bin/print-agent (old installer, now a symlink)
	ConfigDir  string // ~/.config/print-agent
	ConfigFile string // ~/.config/print-agent/env
	UnitDir    string // ~/.config/systemd/user
}

func paths() layout {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return layout{
		Bin:        filepath.Join(home, ".local", "bin", "printer"),
		LegacyBin:  filepath.Join(home, ".local", "bin", "print-agent"),
		ConfigDir:  filepath.Join(home, ".config", "print-agent"),
		ConfigFile: filepath.Join(home, ".config", "print-agent", "env"),
		UnitDir:    filepath.Join(home, ".config", "systemd", "user"),
	}
}

// tildePath shortens a path under $HOME for display.
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func cmdVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	short := fs.Bool("short", false, "Print only the version")
	_ = fs.Parse(args)
	if *short {
		fmt.Println(version)
		return nil
	}
	fmt.Printf("printer %s", version)
	if commit != "" {
		fmt.Printf(" (commit %s", commit)
		if date != "" {
			fmt.Printf(", built %s", date)
		}
		fmt.Print(")")
	}
	fmt.Printf(" %s/%s %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	return nil
}

func cmdStatus(args []string) error {
	ctx := context.Background()
	p := paths()
	sd := service.New()

	fmt.Printf("printer %s (%s/%s)\n\n", version, runtime.GOOS, runtime.GOARCH)

	props, err := sd.Show(ctx, service.Agent)
	switch {
	case err != nil:
		fmt.Printf("Service   cannot query systemd: %v\n", err)
	case props["LoadState"] == "not-found":
		fmt.Println("Service   not installed — run `printer setup`")
	default:
		line := fmt.Sprintf("%s (%s)", props["ActiveState"], props["SubState"])
		if props.Active() && props["ActiveEnterTimestamp"] != "" {
			line += " since " + props["ActiveEnterTimestamp"]
		}
		fmt.Printf("Service   %s: %s\n", service.Agent, line)
		boot := "enabled at boot"
		if props["UnitFileState"] != "enabled" {
			boot = "NOT enabled at boot (" + props["UnitFileState"] + ") — run `printer setup`"
		}
		fmt.Printf("          %s, %d automatic restart(s)\n", boot, props.Restarts())
	}

	if linger, err := lingerEnabled(ctx); err == nil {
		if linger {
			fmt.Println("Boot      linger on: starts at boot without anyone logging in")
		} else {
			fmt.Println("Boot      linger OFF: starts only once the user logs in — run `printer setup`")
		}
	}

	if data, err := os.ReadFile(p.ConfigFile); err != nil {
		fmt.Printf("Config    %s missing — run `printer setup`\n", tildePath(p.ConfigFile))
	} else {
		entries, problems := envfile.Validate(data)
		state := "OK"
		if envfile.HasFatal(problems) {
			state = "INVALID — run `printer config check`"
		}
		api := envfile.Map(entries)["PRINT_AGENT_API_URL"]
		fmt.Printf("Config    %s: %s (API %s)\n", tildePath(p.ConfigFile), state, api)
	}

	devices, _ := receipt.DetectPrinters()
	if len(devices) == 0 {
		fmt.Println("Printers  none detected")
	}
	for i, d := range devices {
		label := "Printers "
		if i > 0 {
			label = "         "
		}
		state := "writable"
		if !d.IsAvailable {
			state = "NOT writable — check the lp group (see README)"
		}
		fmt.Printf("%s %s (%s)\n", label, d.Path, state)
	}

	if timer, err := sd.Show(ctx, service.UpdateTimer); err == nil && timer["LoadState"] == "loaded" {
		if timer["UnitFileState"] == "enabled" {
			fmt.Println("Updates   automatic daily update ON (`printer autoupdate off` to disable)")
		} else {
			fmt.Println("Updates   automatic update off — `printer update --check` to compare")
		}
	}

	if out, err := exec.Command("journalctl", "--user", "-u", service.Agent, "-n", "8",
		"--no-pager", "-o", "short-iso").Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
		fmt.Println("\nRecent logs (`printer log` to follow):")
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			fmt.Println("  " + l)
		}
	}
	return nil
}

func lingerEnabled(ctx context.Context) (bool, error) {
	u, err := user.Current()
	if err != nil {
		return false, err
	}
	out, err := exec.CommandContext(ctx, "loginctl", "show-user", u.Username, "-p", "Linger", "--value").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "yes", nil
}

// checkConfig refuses to (re)start the service with a config that cannot work.
func checkConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config %s: %w (run `printer setup`)", tildePath(path), err)
	}
	_, problems := envfile.Validate(data)
	if envfile.HasFatal(problems) {
		printProblems(problems)
		return fmt.Errorf("config %s is invalid: fix it with `printer config edit`", tildePath(path))
	}
	return nil
}

func printProblems(problems []envfile.Problem) {
	for _, pr := range problems {
		fmt.Fprintln(os.Stderr, "  "+pr.String())
	}
}

func cmdStartRestart(action string, args []string) error {
	fs := flag.NewFlagSet(action, flag.ExitOnError)
	wait := fs.Duration("wait", 10*time.Second, "How long the service must stay up to be reported healthy")
	_ = fs.Parse(args)
	if err := checkConfig(paths().ConfigFile); err != nil {
		return err
	}
	return restartAndWatch(action, *wait)
}

func restartAndWatch(action string, wait time.Duration) error {
	ctx := context.Background()
	sd := service.New()
	if err := sd.Ctl(ctx, action, service.Agent); err != nil {
		return err
	}
	fmt.Printf("%s: %s, checking it stays up for %s...\n", service.Agent, action, wait)
	if err := sd.WaitHealthy(ctx, service.Agent, wait, 500*time.Millisecond); err != nil {
		return fmt.Errorf("%w — see `printer log -n 50`", err)
	}
	fmt.Println("OK: the agent is running.")
	return nil
}

func cmdStop(args []string) error {
	if err := service.New().Ctl(context.Background(), "stop", service.Agent); err != nil {
		return err
	}
	fmt.Println("Stopped. It will start again at the next boot (or `printer start`).")
	return nil
}

func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	lines := fs.Int("n", 0, "Print the last N lines (without -f: do not follow)")
	follow := fs.Bool("f", false, "Follow new lines (default when -n is not given)")
	since := fs.String("since", "", "Only lines since, e.g. today, \"1 hour ago\", 2026-11-06")
	errorsOnly := fs.Bool("errors", false, "Only errors")
	_ = fs.Parse(args)

	jargs := []string{"journalctl", "--user", "-u", service.Agent, "--no-pager", "-o", "short-iso"}
	switch {
	case *lines > 0:
		jargs = append(jargs, "-n", fmt.Sprint(*lines))
		if *follow {
			jargs = append(jargs, "-f")
		}
	case *since != "":
		if *follow {
			jargs = append(jargs, "-f")
		}
	default:
		jargs = append(jargs, "-n", "50", "-f")
	}
	if *since != "" {
		jargs = append(jargs, "--since", *since)
	}
	if *errorsOnly {
		jargs = append(jargs, "-p", "err")
	}
	bin, err := exec.LookPath("journalctl")
	if err != nil {
		return err
	}
	return syscall.Exec(bin, jargs, os.Environ())
}

func cmdConfig(args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub = args[0]
	}
	p := paths()
	switch sub {
	case "show":
		return configShow(p.ConfigFile)
	case "path":
		fmt.Println(p.ConfigFile)
		return nil
	case "check":
		data, err := os.ReadFile(p.ConfigFile)
		if err != nil {
			return err
		}
		_, problems := envfile.Validate(data)
		printProblems(problems)
		if envfile.HasFatal(problems) {
			return errors.New("config is invalid")
		}
		fmt.Printf("%s: OK\n", tildePath(p.ConfigFile))
		return nil
	case "edit":
		return configEdit(p.ConfigFile)
	default:
		return fmt.Errorf("unknown config command %q (show, edit, check, path)", sub)
	}
}

func configShow(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w (run `printer setup`)", err)
	}
	entries, problems := envfile.Validate(data)
	fmt.Printf("# %s (secrets masked)\n", tildePath(path))
	for _, e := range entries {
		fmt.Printf("%s=%s\n", e.Key, envfile.Mask(e.Key, e.Value))
	}
	if len(problems) > 0 {
		fmt.Println()
		printProblems(problems)
	}
	return nil
}

func editorCommand() []string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return strings.Fields(v)
		}
	}
	for _, e := range []string{"nano", "vi"} {
		if _, err := exec.LookPath(e); err == nil {
			return []string{e}
		}
	}
	return []string{"vi"}
}

func configEdit(path string) error {
	before, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w (run `printer setup`)", err)
	}
	in := bufio.NewReader(os.Stdin)
	for {
		ed := editorCommand()
		cmd := exec.Command(ed[0], append(ed[1:], path)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("editor %s: %w", ed[0], err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, problems := envfile.Validate(after)
		printProblems(problems)
		if !envfile.HasFatal(problems) {
			if bytes.Equal(before, after) {
				fmt.Println("No change.")
				return nil
			}
			break
		}
		fmt.Print("The config is invalid. Edit it again? [Y/n] ")
		answer, _ := in.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a == "n" || a == "no" || a == "non" {
			return errors.New("config left invalid: the service was not restarted")
		}
	}
	fmt.Println("Config saved, restarting the service...")
	if err := service.New().Ctl(context.Background(), "enable", service.Agent); err != nil {
		return err
	}
	return restartAndWatch("restart", 10*time.Second)
}

func cmdTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	device := fs.String("device", "", "Receipt printer device (default: RECEIPT_PRINTER_DEVICE or auto-detect)")
	_ = fs.Parse(args)
	// Read the same settings as the service (RECEIPT_PRINTER_DEVICE...).
	_ = envfile.Load(paths().ConfigFile)

	pr := receipt.NewPrinter(*device)
	host, _ := os.Hostname()
	lines := []string{
		"Version : " + version,
		"Poste   : " + host,
		"Date    : " + time.Now().Format("02/01/2006 15:04:05"),
		"Device  : " + pr.Device,
	}
	if u, err := url.Parse(os.Getenv("PRINT_AGENT_API_URL")); err == nil && u.Host != "" {
		lines = append(lines, "API     : "+u.Host)
	}
	if err := pr.PrintTestPage(lines); err != nil {
		return fmt.Errorf("print on %s: %w (see `printer status`, lp group)", pr.Device, err)
	}
	fmt.Printf("Test ticket sent to %s.\n", pr.Device)
	return nil
}

func cmdAutoUpdate(args []string) error {
	ctx := context.Background()
	sd := service.New()
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "on":
		if err := sd.Ctl(ctx, "enable", "--now", service.UpdateTimer); err != nil {
			return fmt.Errorf("%w (run `printer setup` first)", err)
		}
		fmt.Println("Automatic update ON: every day around 05:00 when the PC is on.")
	case "off":
		if err := sd.Ctl(ctx, "disable", "--now", service.UpdateTimer); err != nil {
			return err
		}
		fmt.Println("Automatic update OFF. Update by hand with `printer update`.")
	case "status":
		props, err := sd.Show(ctx, service.UpdateTimer)
		if err != nil {
			return err
		}
		if props["LoadState"] != "loaded" {
			fmt.Println("Update timer not installed: run `printer setup`.")
			return nil
		}
		if props["UnitFileState"] == "enabled" {
			fmt.Println("Automatic update ON (daily around 05:00).")
		} else {
			fmt.Println("Automatic update OFF (`printer autoupdate on` to enable).")
		}
		fmt.Println("Last runs: `journalctl --user -u " + service.UpdateService + "`")
	default:
		return fmt.Errorf("unknown autoupdate command %q (on, off, status)", sub)
	}
	return nil
}
