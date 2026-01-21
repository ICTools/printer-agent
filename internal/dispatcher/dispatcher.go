package dispatcher

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"print-agent/internal/api"
	"print-agent/internal/label"
	"print-agent/internal/receipt"
	"print-agent/internal/registry"
)

// Config holds dispatcher configuration.
type Config struct {
	MaxRetries int
	RetryDelay time.Duration
}

// DefaultConfig returns a default dispatcher configuration.
func DefaultConfig() Config {
	return Config{
		MaxRetries: 3,
		RetryDelay: 2 * time.Second,
	}
}

// Dispatcher routes print jobs to the appropriate printer driver.
type Dispatcher struct {
	config   Config
	registry *registry.Registry
	mutexes  map[string]*sync.Mutex
	mu       sync.RWMutex // protects mutexes map
}

// NewDispatcher creates a new dispatcher with default config.
func NewDispatcher(reg *registry.Registry) *Dispatcher {
	return NewDispatcherWithConfig(reg, DefaultConfig())
}

// NewDispatcherWithConfig creates a new dispatcher with custom config.
func NewDispatcherWithConfig(reg *registry.Registry, config Config) *Dispatcher {
	return &Dispatcher{
		config:   config,
		registry: reg,
		mutexes:  make(map[string]*sync.Mutex),
	}
}

// Dispatch executes a print job on the appropriate printer with retry support.
func (d *Dispatcher) Dispatch(job api.Job) error {
	// Resolve printer
	printer, err := d.resolvePrinter(job)
	if err != nil {
		return fmt.Errorf("resolving printer: %w", err)
	}

	if !printer.Available {
		return fmt.Errorf("printer %s is not available", printer.ID)
	}

	// Acquire per-printer mutex
	mutex := d.getMutex(printer.ID)
	mutex.Lock()
	defer mutex.Unlock()

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= d.config.MaxRetries; attempt++ {
		lastErr = d.dispatchOnce(job, printer)
		if lastErr == nil {
			return nil
		}

		// Don't retry on certain errors
		if isNonRetryableError(lastErr) {
			return lastErr
		}

		// Wait before retry (except on last attempt)
		if attempt < d.config.MaxRetries {
			time.Sleep(d.retryDelay(attempt))
		}
	}

	return fmt.Errorf("after %d attempts: %w", d.config.MaxRetries, lastErr)
}

// dispatchOnce executes a single dispatch attempt.
func (d *Dispatcher) dispatchOnce(job api.Job, printer *registry.PrinterInfo) error {
	switch job.Type {
	case api.JobTypeReceipt:
		return d.dispatchReceipt(job, printer)
	case api.JobTypeLabel:
		return d.dispatchLabel(job, printer)
	case api.JobTypeStickerImage:
		return d.dispatchStickerImage(job, printer)
	case api.JobTypeOpenDrawer:
		return d.dispatchOpenDrawer(printer)
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

// retryDelay returns the delay for a given attempt (with exponential backoff).
func (d *Dispatcher) retryDelay(attempt int) time.Duration {
	// Exponential backoff: delay * 2^(attempt-1)
	multiplier := 1 << uint(attempt-1)
	return d.config.RetryDelay * time.Duration(multiplier)
}

// isNonRetryableError returns true if the error should not be retried.
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Don't retry parsing errors or invalid job types
	return strings.Contains(msg, "parsing") ||
		strings.Contains(msg, "unknown job type") ||
		strings.Contains(msg, "no image data")
}

// resolvePrinter finds the appropriate printer for a job.
func (d *Dispatcher) resolvePrinter(job api.Job) (*registry.PrinterInfo, error) {
	// If printer code is specified, use it
	if job.PrinterCode() != "" {
		return d.registry.Get(job.PrinterCode())
	}

	// Otherwise, find by type
	switch job.Type {
	case api.JobTypeReceipt, api.JobTypeOpenDrawer:
		return d.registry.GetByType(registry.PrinterTypeReceipt)
	case api.JobTypeLabel, api.JobTypeStickerImage:
		return d.registry.GetByType(registry.PrinterTypeLabel)
	default:
		return nil, fmt.Errorf("cannot determine printer type for job type: %s", job.Type)
	}
}

// getMutex returns the mutex for a printer, creating it if necessary.
func (d *Dispatcher) getMutex(printerID string) *sync.Mutex {
	d.mu.RLock()
	mutex, ok := d.mutexes[printerID]
	d.mu.RUnlock()

	if ok {
		return mutex
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check after acquiring write lock
	if mutex, ok := d.mutexes[printerID]; ok {
		return mutex
	}

	mutex = &sync.Mutex{}
	d.mutexes[printerID] = mutex
	return mutex
}

// dispatchReceipt prints a receipt.
func (d *Dispatcher) dispatchReceipt(job api.Job, printer *registry.PrinterInfo) error {
	payload, err := job.ParseReceiptPayload()
	if err != nil {
		return fmt.Errorf("parsing receipt payload: %w", err)
	}

	// Check if this is a put_aside ticket
	if payload.Type == "put_aside" {
		return d.dispatchPutAside(job, printer)
	}

	// Convert API items to receipt items
	items := make([]receipt.ReceiptItem, len(payload.Items))
	for i, item := range payload.Items {
		items[i] = receipt.ReceiptItem{
			Name:      item.Name,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice,
		}
	}

	r := receipt.Receipt{
		StoreAddress1: payload.StoreAddress1,
		StoreAddress2: payload.StoreAddress2,
		StorePhone:    payload.StorePhone,
		StoreVAT:      payload.StoreVAT,
		StoreSocial:   payload.StoreSocial,
		StoreWebsite:  payload.StoreWebsite,
		Barcode:       payload.Barcode,
		Items:         items,
		Payments:      payload.Payments,
		CreatedAt:     time.Now(),
	}

	p := receipt.NewPrinter(printer.DevicePath)
	return p.PrintReceipt(r)
}

// dispatchPutAside prints a "put aside" ticket for reserved products.
func (d *Dispatcher) dispatchPutAside(job api.Job, printer *registry.PrinterInfo) error {
	payload, err := job.ParsePutAsidePayload()
	if err != nil {
		return fmt.Errorf("parsing put_aside payload: %w", err)
	}

	pa := receipt.PutAside{
		CustomerName:   payload.CustomerName,
		CustomerPhone:  payload.CustomerPhone,
		ProductName:    payload.ProductName,
		ProductBarcode: payload.ProductBarcode,
		Quantity:       payload.Quantity,
		OrderBarcode:   payload.OrderBarcode,
		OrderDate:      payload.OrderDate,
	}

	p := receipt.NewPrinter(printer.DevicePath)
	return p.PrintPutAsideTicket(pa)
}

// dispatchLabel prints a price label.
func (d *Dispatcher) dispatchLabel(job api.Job, printer *registry.PrinterInfo) error {
	payload, err := job.ParseLabelPayload()
	if err != nil {
		return fmt.Errorf("parsing label payload: %w", err)
	}

	opts := label.LabelOptions{
		Name:      payload.Name,
		PriceText: payload.PriceText,
		Barcode:   payload.Barcode,
		Footer:    payload.Footer,
	}

	return label.PrintLabel(opts)
}

// dispatchStickerImage prints a sticker image.
func (d *Dispatcher) dispatchStickerImage(job api.Job, printer *registry.PrinterInfo) error {
	payload, err := job.ParseStickerImagePayload()
	if err != nil {
		return fmt.Errorf("parsing sticker image payload: %w", err)
	}

	var imagePath string
	var cleanupFunc func()

	// Handle base64 image data (priority over URL)
	if payload.ImageData != "" {
		decoded, err := base64.StdEncoding.DecodeString(payload.ImageData)
		if err != nil {
			return fmt.Errorf("decoding base64 image: %w", err)
		}

		tmpFile, err := os.CreateTemp("", "sticker-*.png")
		if err != nil {
			return fmt.Errorf("creating temp file: %w", err)
		}
		imagePath = tmpFile.Name()
		cleanupFunc = func() { os.Remove(imagePath) }

		if _, err := tmpFile.Write(decoded); err != nil {
			tmpFile.Close()
			cleanupFunc()
			return fmt.Errorf("writing temp file: %w", err)
		}
		tmpFile.Close()
	} else if payload.ImageURL != "" {
		// Download image from URL
		path, cleanup, err := downloadImage(payload.ImageURL)
		if err != nil {
			return fmt.Errorf("downloading image: %w", err)
		}
		imagePath = path
		cleanupFunc = cleanup
	} else {
		return fmt.Errorf("no image data or URL provided")
	}

	// Ensure cleanup after printing
	if cleanupFunc != nil {
		defer cleanupFunc()
	}

	opts := label.StickerImageOptions{
		ImagePath:  imagePath,
		DevicePath: printer.DevicePath,
	}

	return label.PrintStickerImage(opts)
}

// downloadImage downloads an image from a URL and returns the path to the temp file.
// Returns the file path, a cleanup function, and any error.
func downloadImage(imageURL string) (string, func(), error) {
	// Create HTTP client with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("creating request: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("fetching image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Determine file extension from Content-Type or URL
	ext := ".png"
	contentType := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		ext = ".jpg"
	case strings.Contains(contentType, "png"):
		ext = ".png"
	case strings.Contains(contentType, "gif"):
		ext = ".gif"
	case strings.Contains(contentType, "webp"):
		ext = ".webp"
	case strings.HasSuffix(strings.ToLower(imageURL), ".jpg"), strings.HasSuffix(strings.ToLower(imageURL), ".jpeg"):
		ext = ".jpg"
	case strings.HasSuffix(strings.ToLower(imageURL), ".gif"):
		ext = ".gif"
	case strings.HasSuffix(strings.ToLower(imageURL), ".webp"):
		ext = ".webp"
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "sticker-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Limit download size to 10MB
	limitedReader := io.LimitReader(resp.Body, 10*1024*1024)

	_, err = io.Copy(tmpFile, limitedReader)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("saving image: %w", err)
	}
	tmpFile.Close()

	cleanup := func() {
		os.Remove(tmpPath)
	}

	return tmpPath, cleanup, nil
}

// dispatchOpenDrawer opens the cash drawer.
func (d *Dispatcher) dispatchOpenDrawer(printer *registry.PrinterInfo) error {
	p := receipt.NewPrinter(printer.DevicePath)
	return p.OpenDrawer()
}

// TempImageDir returns the directory for temporary images.
func TempImageDir() string {
	dir := filepath.Join(os.TempDir(), "print-agent")
	os.MkdirAll(dir, 0755)
	return dir
}
