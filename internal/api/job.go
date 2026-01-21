package api

import (
	"encoding/json"
	"time"
)

// JobType represents the type of print job.
type JobType string

const (
	JobTypeReceipt      JobType = "receipt"
	JobTypeLabel        JobType = "label"
	JobTypeStickerImage JobType = "sticker_image"
	JobTypeOpenDrawer   JobType = "open_drawer"
)

// JobPrinter contains printer info associated with a job.
type JobPrinter struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Job represents a print job fetched from the remote API.
type Job struct {
	ID         string          `json:"job_id"`
	LeaseID    string          `json:"lease_id"`
	LeaseUntil string          `json:"lease_until"`
	Type       JobType         `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	RetryCount int             `json:"retry_count"`
	Printer    JobPrinter      `json:"printer"`
	CreatedAt  time.Time       `json:"created_at,omitempty"`
}

// PrinterCode returns the printer code for this job.
func (j *Job) PrinterCode() string {
	return j.Printer.Code
}

// NextJobResponse is the response from GET /jobs/next.
type NextJobResponse struct {
	Success bool `json:"success"`
	Data    *Job `json:"data"` // nil if no job available
}

// FetchNextJobParams contains optional parameters for fetching next job.
type FetchNextJobParams struct {
	Type          string // Filter by job type (receipt, label, sticker_image)
	PrinterCode   string // Filter by specific printer
	LeaseDuration int    // Lease duration in seconds (default 60, min 10, max 300)
}

// ReceiptPayload is the payload for a receipt print job.
type ReceiptPayload struct {
	Type          string        `json:"type,omitempty"` // "put_aside" for put-aside tickets
	StoreAddress1 string        `json:"store_address_1,omitempty"`
	StoreAddress2 string        `json:"store_address_2,omitempty"`
	StorePhone    string        `json:"store_phone,omitempty"`
	StoreVAT      string        `json:"store_vat,omitempty"`
	StoreSocial   string        `json:"store_social,omitempty"`
	StoreWebsite  string        `json:"store_website,omitempty"`
	Barcode       string        `json:"barcode,omitempty"`
	Items         []ReceiptItem `json:"items"`
	Payments      []string      `json:"payments,omitempty"`
}

// PutAsidePayload is the payload for a "put aside" ticket.
type PutAsidePayload struct {
	Type           string `json:"type"`                      // should be "put_aside"
	CustomerName   string `json:"customer_name"`             // Client name
	CustomerPhone  string `json:"customer_phone,omitempty"`  // Client phone (optional)
	ProductName    string `json:"product_name"`              // Product description
	ProductBarcode string `json:"product_barcode,omitempty"` // Product barcode (optional)
	Quantity       int    `json:"quantity,omitempty"`        // Quantity (default 1)
	OrderID        string `json:"order_id,omitempty"`        // Order ID
	OrderBarcode   string `json:"order_barcode"`             // Order reference (e.g. CMD-2024-001234)
	OrderDate      string `json:"order_date"`                // Order date (e.g. 15/01/2024)
}

// ReceiptItem is a line item on a receipt.
type ReceiptItem struct {
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice string `json:"unit_price"`
}

// LabelPayload is the payload for a label print job.
type LabelPayload struct {
	Name      string `json:"name"`
	PriceText string `json:"price_text"`
	Barcode   string `json:"barcode"`
	Footer    string `json:"footer,omitempty"`
}

// StickerImagePayload is the payload for a sticker image print job.
type StickerImagePayload struct {
	ImageURL  string `json:"image_url,omitempty"`
	ImageData string `json:"image_data,omitempty"` // base64 encoded
}

// OpenDrawerPayload is the payload for an open drawer job (usually empty).
type OpenDrawerPayload struct{}

// ParseReceiptPayload parses the job payload as a ReceiptPayload.
func (j *Job) ParseReceiptPayload() (*ReceiptPayload, error) {
	var p ReceiptPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ParseLabelPayload parses the job payload as a LabelPayload.
func (j *Job) ParseLabelPayload() (*LabelPayload, error) {
	var p LabelPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ParseStickerImagePayload parses the job payload as a StickerImagePayload.
func (j *Job) ParseStickerImagePayload() (*StickerImagePayload, error) {
	var p StickerImagePayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ParsePutAsidePayload parses the job payload as a PutAsidePayload.
func (j *Job) ParsePutAsidePayload() (*PutAsidePayload, error) {
	var p PutAsidePayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return nil, err
	}
	// Default quantity to 1 if not specified
	if p.Quantity <= 0 {
		p.Quantity = 1
	}
	return &p, nil
}
