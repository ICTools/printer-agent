package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"print-agent/internal/api"
	"print-agent/internal/registry"
)

func TestAgent_StartStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond

	agent := New(client, reg, config)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		done <- agent.Start(ctx)
	}()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	// Stop via context
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not stop in time")
	}
}

func TestAgent_StopMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond

	agent := New(client, reg, config)

	done := make(chan error)
	go func() {
		done <- agent.Start(context.Background())
	}()

	// Let it run
	time.Sleep(100 * time.Millisecond)

	// Stop via Stop method
	agent.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not stop in time")
	}
}

func TestAgent_ProcessesJobs(t *testing.T) {
	var fetchCount int64
	var ackCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/printer-agent/jobs/next" && r.Method == http.MethodGet:
			count := atomic.AddInt64(&fetchCount, 1)
			if count == 1 {
				// First fetch: return a job
				// Use open-drawer since it's simplest and doesn't need actual printer
				job := api.Job{
					ID:         "test-job-1",
					LeaseID:    "lease-123",
					LeaseUntil: "2026-01-09T15:30:00+00:00",
					Type:       api.JobTypeOpenDrawer,
					Payload:    json.RawMessage(`{}`),
					Printer: api.JobPrinter{
						Code: "test-printer",
						Name: "Test Printer",
						Type: "receipt",
					},
				}
				json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: &job})
			} else {
				// Subsequent fetches: no jobs
				json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
			}

		case r.URL.Path == "/api/printer-agent/jobs/test-job-1/ack" && r.Method == http.MethodPost:
			atomic.AddInt64(&ackCount, 1)
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()

	// Register a test printer (it won't actually work but dispatcher will try)
	reg.Register(registry.PrinterInfo{
		ID:         "test-printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/null",
		Available:  true,
	})

	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond

	agent := New(client, reg, config)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		done <- agent.Start(ctx)
	}()

	// Wait for job to be processed
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-done

	if atomic.LoadInt64(&fetchCount) < 1 {
		t.Error("expected at least one fetch")
	}

	// Job processing was attempted (either ack or fail was called)
	stats := agent.GetStats()
	if stats.JobsProcessed+stats.JobsFailed < 1 {
		t.Error("expected at least one job to be processed or failed")
	}
}

func TestAgent_BackoffOnError(t *testing.T) {
	var fetchCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fetchCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 1, // Fail fast for testing
	})
	reg := registry.NewRegistry()

	config := DefaultConfig()
	config.PollInterval = 20 * time.Millisecond
	config.InitialBackoff = 50 * time.Millisecond
	config.MaxBackoff = 100 * time.Millisecond

	agent := New(client, reg, config)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		done <- agent.Start(ctx)
	}()

	// Let it run and hit errors
	time.Sleep(300 * time.Millisecond)

	cancel()
	<-done

	stats := agent.GetStats()
	if stats.ConsecutiveErr == 0 {
		t.Error("expected consecutive errors to be tracked")
	}
}

func TestAgent_GetStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()

	agent := New(client, reg, DefaultConfig())

	stats := agent.GetStats()
	if stats.JobsProcessed != 0 {
		t.Errorf("expected 0 jobs processed, got %d", stats.JobsProcessed)
	}
	if stats.JobsFailed != 0 {
		t.Errorf("expected 0 jobs failed, got %d", stats.JobsFailed)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.PollInterval != 2*time.Second {
		t.Errorf("expected 2s poll interval, got %s", config.PollInterval)
	}
	if config.PingInterval != 30*time.Second {
		t.Errorf("expected 30s ping interval, got %s", config.PingInterval)
	}
	if config.MaxBackoff != 60*time.Second {
		t.Errorf("expected 60s max backoff, got %s", config.MaxBackoff)
	}
	if config.InitialBackoff != 1*time.Second {
		t.Errorf("expected 1s initial backoff, got %s", config.InitialBackoff)
	}
	if config.BackoffFactor != 2.0 {
		t.Errorf("expected 2.0 backoff factor, got %f", config.BackoffFactor)
	}
}
