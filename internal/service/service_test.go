package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// scripted returns a Runner answering each `show` call with the next state.
func scripted(states ...string) (Runner, *[]string) {
	var calls []string
	i := 0
	return func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) > 1 && args[1] == "show" {
			s := states[min(i, len(states)-1)]
			i++
			return s, nil
		}
		return "", nil
	}, &calls
}

func state(active, sub string, restarts int) string {
	return fmt.Sprintf("ActiveState=%s\nSubState=%s\nNRestarts=%d\nResult=success\n", active, sub, restarts)
}

func newTest(r Runner) *Systemd { return &Systemd{Run: r, Sleep: func(time.Duration) {}} }

func TestWaitHealthyStable(t *testing.T) {
	r, _ := scripted(state("active", "running", 2))
	if err := newTest(r).WaitHealthy(context.Background(), Agent, 3*time.Second, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWaitHealthyDetectsCrashLoop(t *testing.T) {
	r, _ := scripted(
		state("active", "running", 0),
		state("active", "running", 0),
		state("activating", "auto-restart", 1),
	)
	err := newTest(r).WaitHealthy(context.Background(), Agent, 10*time.Second, time.Second)
	if err == nil || !strings.Contains(err.Error(), "restarted") {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitHealthyDetectsRestartBetweenPolls(t *testing.T) {
	r, _ := scripted(state("active", "running", 0), state("active", "running", 1))
	if err := newTest(r).WaitHealthy(context.Background(), Agent, 10*time.Second, time.Second); err == nil {
		t.Fatal("a restart between two polls must be detected")
	}
}

func TestWaitHealthyDetectsFailed(t *testing.T) {
	r, _ := scripted(state("active", "running", 0), state("failed", "failed", 0))
	if err := newTest(r).WaitHealthy(context.Background(), Agent, 10*time.Second, time.Second); err == nil {
		t.Fatal("failed state must be an error")
	}
}

func TestShowAndCtl(t *testing.T) {
	r, calls := scripted("ActiveState=active\nUnitFileState=enabled\nNRestarts=3\n")
	s := newTest(r)
	p, err := s.Show(context.Background(), Agent)
	if err != nil || !p.Active() || p["UnitFileState"] != "enabled" || p.Restarts() != 3 {
		t.Fatalf("props = %v, %v", p, err)
	}
	if err := s.Ctl(context.Background(), "restart", Agent); err != nil {
		t.Fatal(err)
	}
	if last := (*calls)[len(*calls)-1]; last != "systemctl --user restart print-agent.service" {
		t.Fatalf("call = %q", last)
	}
}
