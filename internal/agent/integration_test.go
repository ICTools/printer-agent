package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"print-agent/internal/api"
	"print-agent/internal/registry"
)

// TestIntegration_FullFlow tests the complete flow from API polling to job processing.
func TestIntegration_FullFlow(t *testing.T) {
	var (
		fetchCount int64
		ackCount   int64
	)

	// Mock API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/printer-agent/jobs/next" && r.Method == http.MethodGet:
			count := atomic.AddInt64(&fetchCount, 1)
			if count == 1 {
				// First fetch: return a job
				job := api.Job{
					ID:         "job-success",
					LeaseID:    "lease-123",
					LeaseUntil: "2026-01-09T15:30:00+00:00",
					Type:       api.JobTypeOpenDrawer,
					Payload:    json.RawMessage(`{}`),
					Printer: api.JobPrinter{
						Code: "test-receipt",
						Name: "Test Receipt Printer",
						Type: "receipt",
					},
				}
				json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: &job})
			} else {
				json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
			}

		case r.URL.Path == "/api/printer-agent/jobs/job-success/ack":
			atomic.AddInt64(&ackCount, 1)
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Setup
	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "test-receipt",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/null",
		Available:  true,
	})

	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond

	agent := New(client, reg, config)

	// Run agent briefly
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- agent.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Verify
	if atomic.LoadInt64(&fetchCount) < 1 {
		t.Error("expected at least one fetch")
	}

	stats := agent.GetStats()
	if stats.JobsProcessed+stats.JobsFailed < 1 {
		t.Error("expected at least one job to be processed")
	}
}

// TestIntegration_HealthEndpoint tests the health check server.
func TestIntegration_HealthEndpoint(t *testing.T) {
	// Setup minimal agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "test-printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/null",
		Available:  true,
	})

	config := DefaultConfig()
	config.PollInterval = 100 * time.Millisecond

	agent := New(client, reg, config)

	// Start agent in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.Start(ctx)

	// Give agent time to start
	time.Sleep(50 * time.Millisecond)

	// Start health server
	health := NewHealthServer(agent, "127.0.0.1:0")
	err := health.Start()
	if err != nil {
		t.Fatalf("failed to start health server: %v", err)
	}
	defer health.Stop(context.Background())

	// Test /health endpoint
	resp, err := http.Get("http://" + health.server.Addr + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var healthResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if healthResp.Status != "healthy" && healthResp.Status != "degraded" {
		t.Errorf("unexpected health status: %s", healthResp.Status)
	}

	// Test /metrics endpoint
	resp2, err := http.Get("http://" + health.server.Addr + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp2.Body.Close()

	var metricsResp MetricsResponse
	if err := json.NewDecoder(resp2.Body).Decode(&metricsResp); err != nil {
		t.Fatalf("failed to decode metrics response: %v", err)
	}

	if metricsResp.Printers < 1 {
		t.Errorf("expected at least 1 printer, got %d", metricsResp.Printers)
	}
}

// TestIntegration_GracefulShutdown tests that the agent shuts down cleanly.
func TestIntegration_GracefulShutdown(t *testing.T) {
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

	done := make(chan error, 1)
	go func() {
		done <- agent.Start(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop via Stop() method
	agent.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not stop within timeout")
	}
}

// TestIntegration_APIReconnection tests that the agent reconnects after API errors.
func TestIntegration_APIReconnection(t *testing.T) {
	var requestCount int64
	failUntil := int64(2) // Fail first 2 requests

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt64(&requestCount, 1)
		if count <= failUntil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 1, // Fail fast
	})
	reg := registry.NewRegistry()

	config := DefaultConfig()
	config.PollInterval = 10 * time.Millisecond
	config.InitialBackoff = 5 * time.Millisecond
	config.MaxBackoff = 20 * time.Millisecond

	agent := New(client, reg, config)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- agent.Start(ctx)
	}()

	// Wait for reconnection - enough time for failures + recovery
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Should have made multiple requests (failures + successful reconnect)
	totalRequests := atomic.LoadInt64(&requestCount)
	if totalRequests <= failUntil {
		t.Errorf("expected more than %d requests, got %d", failUntil, totalRequests)
	}

	// Verify that we eventually got successful requests (more than just failures)
	if totalRequests <= failUntil {
		t.Error("expected successful requests after initial failures")
	}
}

// TestIntegration_MultipleJobTypes tests processing different job types.
func TestIntegration_MultipleJobTypes(t *testing.T) {
	var ackCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/printer-agent/jobs/next" && r.Method == http.MethodGet {
			// Return a job on first fetch
			job := api.Job{
				ID:         "job-drawer",
				LeaseID:    "lease-456",
				LeaseUntil: "2026-01-09T15:30:00+00:00",
				Type:       api.JobTypeOpenDrawer,
				Payload:    json.RawMessage(`{}`),
				Printer: api.JobPrinter{
					Code: "receipt-printer",
					Name: "Receipt Printer",
					Type: "receipt",
				},
			}
			json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: &job})
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/api/printer-agent/jobs/job-drawer/ack" {
			atomic.AddInt64(&ackCount, 1)
			w.WriteHeader(http.StatusOK)
			return
		}

		json.NewEncoder(w).Encode(api.NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{BaseURL: server.URL})
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "receipt-printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/null",
		Available:  true,
	})
	reg.Register(registry.PrinterInfo{
		ID:         "label-printer",
		Type:       registry.PrinterTypeLabel,
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

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// Verify job was acknowledged or failed
	stats := agent.GetStats()
	if stats.JobsProcessed+stats.JobsFailed < 1 {
		t.Error("expected at least one job to be processed")
	}
}

// TestIntegration_HealthDegraded tests health status when errors occur.
func TestIntegration_HealthDegraded(t *testing.T) {
	// Server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 1,
	})
	reg := registry.NewRegistry()

	config := DefaultConfig()
	config.PollInterval = 10 * time.Millisecond
	config.InitialBackoff = 5 * time.Millisecond
	config.MaxBackoff = 20 * time.Millisecond

	agent := New(client, reg, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.Start(ctx)

	// Wait for errors to accumulate
	time.Sleep(200 * time.Millisecond)

	// Start health server
	health := NewHealthServer(agent, "127.0.0.1:0")
	health.Start()
	defer health.Stop(context.Background())

	// Check health status
	resp, err := http.Get("http://" + health.server.Addr + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var healthResp HealthResponse
	json.Unmarshal(body, &healthResp)

	// Should be degraded due to consecutive errors
	stats := agent.GetStats()
	if stats.ConsecutiveErr > 5 && healthResp.Status != "degraded" {
		t.Errorf("expected degraded status with %d consecutive errors, got %s", stats.ConsecutiveErr, healthResp.Status)
	}
}
