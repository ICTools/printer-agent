package agent

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalContext returns a context that is canceled when SIGINT or SIGTERM is received.
// It also returns a cancel function to manually cancel the context.
func SignalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	return ctx, cancel
}

// WaitForSignal blocks until SIGINT or SIGTERM is received.
func WaitForSignal() os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	signal.Stop(sigCh)
	return sig
}
