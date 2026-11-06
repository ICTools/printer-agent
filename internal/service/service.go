// Package service drives the agent's systemd user units through systemctl.
package service

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Unit names.
const (
	Agent         = "print-agent.service"
	UpdateService = "printer-update.service"
	UpdateTimer   = "printer-update.timer"
)

// Runner runs a command and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// ExecRunner runs real commands.
func ExecRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Systemd calls `systemctl --user`.
type Systemd struct {
	Run   Runner
	Sleep func(time.Duration)
}

// New returns a Systemd running real commands.
func New() *Systemd { return &Systemd{Run: ExecRunner, Sleep: time.Sleep} }

// Ctl runs `systemctl --user <args>`.
func (s *Systemd) Ctl(ctx context.Context, args ...string) error {
	_, err := s.Run(ctx, "systemctl", append([]string{"--user"}, args...)...)
	return err
}

// Props are unit properties from `systemctl --user show`.
type Props map[string]string

// Active reports whether the unit is up and its process running.
func (p Props) Active() bool { return p["ActiveState"] == "active" }

// Restarts is the number of automatic restarts systemd made.
func (p Props) Restarts() int {
	n, _ := strconv.Atoi(p["NRestarts"])
	return n
}

// Show returns the properties of a unit.
func (s *Systemd) Show(ctx context.Context, unit string) (Props, error) {
	out, err := s.Run(ctx, "systemctl", "--user", "show", unit, "--no-pager",
		"-p", "ActiveState", "-p", "SubState", "-p", "NRestarts", "-p", "UnitFileState",
		"-p", "LoadState", "-p", "ActiveEnterTimestamp", "-p", "MainPID", "-p", "Result",
		"-p", "NextElapseUSecRealtime", "-p", "LastTriggerUSec")
	if err != nil {
		return nil, err
	}
	props := Props{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	return props, nil
}

// WaitHealthy checks that the unit, just (re)started, stays active for the
// whole window: the agent exits on a bad config or rejected credentials and
// systemd then schedules an automatic restart, which this detects.
func (s *Systemd) WaitHealthy(ctx context.Context, unit string, window, poll time.Duration) error {
	start, err := s.Show(ctx, unit)
	if err != nil {
		return err
	}
	baseline := start.Restarts()
	for waited := time.Duration(0); ; waited += poll {
		p, err := s.Show(ctx, unit)
		if err != nil {
			return err
		}
		switch {
		case p["ActiveState"] == "failed":
			return fmt.Errorf("%s failed (%s)", unit, p["Result"])
		case p.Restarts() > baseline || p["SubState"] == "auto-restart":
			return fmt.Errorf("%s exited and is being restarted by systemd (%s)", unit, p["Result"])
		case p["ActiveState"] == "inactive" || p["ActiveState"] == "deactivating":
			return fmt.Errorf("%s stopped (%s)", unit, p["Result"])
		}
		if waited >= window {
			if !p.Active() {
				return fmt.Errorf("%s not active after %s (%s/%s)", unit, window, p["ActiveState"], p["SubState"])
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.Sleep(poll)
	}
}
