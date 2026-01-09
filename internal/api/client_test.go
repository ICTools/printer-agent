package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchNextJob_Success(t *testing.T) {
	job := Job{
		ID:         "job-1",
		LeaseID:    "lease-123",
		LeaseUntil: "2026-01-09T15:30:00+00:00",
		Type:       JobTypeReceipt,
		Payload:    json.RawMessage(`{"barcode":"TEST123"}`),
		RetryCount: 0,
		Printer: JobPrinter{
			Code: "USB001",
			Name: "EPSON TM-T20",
			Type: "receipt",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/printer-agent/jobs/next" {
			t.Errorf("expected path /api/printer-agent/jobs/next, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NextJobResponse{Success: true, Data: &job})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})

	result, err := client.FetchNextJob(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected job, got nil")
	}
	if result.ID != "job-1" {
		t.Errorf("expected job-1, got %s", result.ID)
	}
	if result.LeaseID != "lease-123" {
		t.Errorf("expected lease-123, got %s", result.LeaseID)
	}
	if result.PrinterCode() != "USB001" {
		t.Errorf("expected USB001, got %s", result.PrinterCode())
	}
}

func TestFetchNextJob_NoJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})

	result, err := client.FetchNextJob(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got job %s", result.ID)
	}
}

func TestFetchNextJob_WithParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "receipt" {
			t.Errorf("expected type=receipt, got %s", r.URL.Query().Get("type"))
		}
		if r.URL.Query().Get("printer_code") != "USB001" {
			t.Errorf("expected printer_code=USB001, got %s", r.URL.Query().Get("printer_code"))
		}
		if r.URL.Query().Get("lease_duration") != "120" {
			t.Errorf("expected lease_duration=120, got %s", r.URL.Query().Get("lease_duration"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})

	_, err := client.FetchNextJob(context.Background(), &FetchNextJobParams{
		Type:          "receipt",
		PrinterCode:   "USB001",
		LeaseDuration: 120,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchNextJob_ServerError_Retries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 3,
	})

	_, err := client.FetchNextJob(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestFetchNextJob_MaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 2,
	})

	_, err := client.FetchNextJob(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
}

func TestFetchNextJob_WithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			t.Errorf("expected Bearer test-api-key, got %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NextJobResponse{Success: true, Data: nil})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-api-key",
	})

	_, err := client.FetchNextJob(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAckJob_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/printer-agent/jobs/job-123/ack" {
			t.Errorf("expected path /api/printer-agent/jobs/job-123/ack, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		var body AckJobRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.LeaseID != "lease-456" {
			t.Errorf("expected lease_id lease-456, got %s", body.LeaseID)
		}
		if !body.Success {
			t.Errorf("expected success=true")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})

	err := client.AckJob(context.Background(), "job-123", "lease-456", true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAckJob_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body AckJobRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.Success {
			t.Errorf("expected success=false")
		}
		if body.ErrorMessage != "printer offline" {
			t.Errorf("expected error_message 'printer offline', got %s", body.ErrorMessage)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})

	err := client.AckJob(context.Background(), "job-456", "lease-789", false, "printer offline")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchNextJob_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.FetchNextJob(ctx, nil)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
