package dispatcher

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"print-agent/internal/api"
	"print-agent/internal/registry"
)

func TestDispatcher_ResolvePrinter_ByCode(t *testing.T) {
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "specific-printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/test",
		Available:  true,
	})

	d := NewDispatcher(reg)

	job := api.Job{
		ID:   "job-1",
		Type: api.JobTypeReceipt,
		Printer: api.JobPrinter{
			Code: "specific-printer",
			Name: "Test Printer",
			Type: "receipt",
		},
	}

	printer, err := d.resolvePrinter(job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if printer.ID != "specific-printer" {
		t.Errorf("expected specific-printer, got %s", printer.ID)
	}
}

func TestDispatcher_ResolvePrinter_ByType(t *testing.T) {
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "receipt-printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/receipt",
		Available:  true,
	})
	reg.Register(registry.PrinterInfo{
		ID:         "label-printer",
		Type:       registry.PrinterTypeLabel,
		DevicePath: "/dev/label",
		Available:  true,
	})

	d := NewDispatcher(reg)

	tests := []struct {
		jobType         api.JobType
		expectedPrinter string
	}{
		{api.JobTypeReceipt, "receipt-printer"},
		{api.JobTypeOpenDrawer, "receipt-printer"},
		{api.JobTypeLabel, "label-printer"},
		{api.JobTypeStickerImage, "label-printer"},
	}

	for _, tt := range tests {
		job := api.Job{Type: tt.jobType}
		printer, err := d.resolvePrinter(job)
		if err != nil {
			t.Errorf("job type %s: unexpected error: %v", tt.jobType, err)
			continue
		}
		if printer.ID != tt.expectedPrinter {
			t.Errorf("job type %s: expected %s, got %s", tt.jobType, tt.expectedPrinter, printer.ID)
		}
	}
}

func TestDispatcher_ResolvePrinter_NotFound(t *testing.T) {
	reg := registry.NewRegistry()
	d := NewDispatcher(reg)

	job := api.Job{
		ID:   "job-1",
		Type: api.JobTypeReceipt,
		Printer: api.JobPrinter{
			Code: "nonexistent",
		},
	}

	_, err := d.resolvePrinter(job)
	if err == nil {
		t.Fatal("expected error for nonexistent printer")
	}
}

func TestDispatcher_MutexPerPrinter(t *testing.T) {
	d := NewDispatcher(registry.NewRegistry())

	mutex1 := d.getMutex("printer-1")
	mutex2 := d.getMutex("printer-2")
	mutex1Again := d.getMutex("printer-1")

	if mutex1 == mutex2 {
		t.Error("different printers should have different mutexes")
	}
	if mutex1 != mutex1Again {
		t.Error("same printer should return same mutex")
	}
}

func TestDispatcher_ConcurrentMutexAccess(t *testing.T) {
	d := NewDispatcher(registry.NewRegistry())

	var wg sync.WaitGroup
	var count int64

	// Simulate concurrent access to the same printer
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mutex := d.getMutex("shared-printer")
			mutex.Lock()
			atomic.AddInt64(&count, 1)
			time.Sleep(time.Microsecond) // Simulate work
			mutex.Unlock()
		}()
	}

	wg.Wait()

	if count != 100 {
		t.Errorf("expected count 100, got %d", count)
	}
}

func TestDispatcher_Dispatch_UnknownJobType(t *testing.T) {
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/null",
		Available:  true,
	})

	d := NewDispatcher(reg)

	job := api.Job{
		ID:   "job-1",
		Type: "unknown-type",
		Printer: api.JobPrinter{
			Code: "printer",
		},
	}

	err := d.Dispatch(job)
	if err == nil {
		t.Fatal("expected error for unknown job type")
	}
}

func TestDispatcher_Dispatch_PrinterNotAvailable(t *testing.T) {
	reg := registry.NewRegistry()
	reg.Register(registry.PrinterInfo{
		ID:         "offline-printer",
		Type:       registry.PrinterTypeReceipt,
		DevicePath: "/dev/nonexistent",
		Available:  false,
	})

	d := NewDispatcher(reg)

	payload, _ := json.Marshal(api.ReceiptPayload{})
	job := api.Job{
		ID:      "job-1",
		Type:    api.JobTypeReceipt,
		Payload: payload,
		Printer: api.JobPrinter{
			Code: "offline-printer",
		},
	}

	err := d.Dispatch(job)
	if err == nil {
		t.Fatal("expected error for unavailable printer")
	}
}

func TestDispatcher_RetryConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxRetries != 3 {
		t.Errorf("expected 3 max retries, got %d", config.MaxRetries)
	}
	if config.RetryDelay != 2*time.Second {
		t.Errorf("expected 2s retry delay, got %s", config.RetryDelay)
	}
}

func TestDispatcher_RetryDelay_ExponentialBackoff(t *testing.T) {
	config := Config{
		MaxRetries: 5,
		RetryDelay: 1 * time.Second,
	}
	d := NewDispatcherWithConfig(registry.NewRegistry(), config)

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 1 * time.Second},  // 1 * 2^0 = 1s
		{2, 2 * time.Second},  // 1 * 2^1 = 2s
		{3, 4 * time.Second},  // 1 * 2^2 = 4s
		{4, 8 * time.Second},  // 1 * 2^3 = 8s
		{5, 16 * time.Second}, // 1 * 2^4 = 16s
	}

	for _, tt := range tests {
		got := d.retryDelay(tt.attempt)
		if got != tt.expected {
			t.Errorf("attempt %d: expected %s, got %s", tt.attempt, tt.expected, got)
		}
	}
}

func TestDispatcher_NonRetryableErrors(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{fmt.Errorf("parsing error"), true},
		{fmt.Errorf("unknown job type: foo"), true},
		{fmt.Errorf("no image data"), true},
		{fmt.Errorf("device not found"), false},
		{fmt.Errorf("connection timeout"), false},
	}

	for _, tt := range tests {
		got := isNonRetryableError(tt.err)
		if got != tt.expected {
			t.Errorf("isNonRetryableError(%v) = %v, want %v", tt.err, got, tt.expected)
		}
	}
}

func TestDownloadImage_Success(t *testing.T) {
	// Create a test server that serves a PNG image
	imageData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imageData)
	}))
	defer server.Close()

	path, cleanup, err := downloadImage(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	// Verify file was created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist")
	}

	// Verify file contents
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(content) != len(imageData) {
		t.Errorf("expected %d bytes, got %d", len(imageData), len(content))
	}
}

func TestDownloadImage_ContentTypeDetection(t *testing.T) {
	tests := []struct {
		contentType string
		expectedExt string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/jpg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"application/octet-stream", ".png"}, // default
	}

	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tt.contentType)
			w.Write([]byte("fake image data"))
		}))

		path, cleanup, err := downloadImage(server.URL)
		server.Close()

		if err != nil {
			t.Errorf("content-type %s: unexpected error: %v", tt.contentType, err)
			continue
		}

		// Check extension
		if len(path) < len(tt.expectedExt) {
			t.Errorf("content-type %s: path too short: %s", tt.contentType, path)
		} else if path[len(path)-len(tt.expectedExt):] != tt.expectedExt {
			t.Errorf("content-type %s: expected extension %s, got path %s", tt.contentType, tt.expectedExt, path)
		}

		cleanup()
	}
}

func TestDownloadImage_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, _, err := downloadImage(server.URL)
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestDownloadImage_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := downloadImage(server.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestDownloadImage_InvalidURL(t *testing.T) {
	_, _, err := downloadImage("not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestDownloadImage_Cleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake image"))
	}))
	defer server.Close()

	path, cleanup, err := downloadImage(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should exist before cleanup
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist before cleanup")
	}

	// Call cleanup
	cleanup()

	// File should not exist after cleanup
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted after cleanup")
	}
}
