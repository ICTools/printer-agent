package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ClientConfig holds configuration for the API client.
type ClientConfig struct {
	BaseURL    string
	APIKey     string // Deprecated: use Authenticator instead
	Timeout    time.Duration
	MaxRetries int
	Insecure   bool // Skip TLS certificate verification (for local testing)
}

// Client is an HTTP client for the print job API.
type Client struct {
	config        ClientConfig
	httpClient    *http.Client
	authenticator *Authenticator
}

// NewClient creates a new API client with the given configuration.
func NewClient(config ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	// Skip TLS verification for local testing
	if config.Insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return &Client{
		config:     config,
		httpClient: httpClient,
	}
}

// NewClientWithAuth creates a new API client with JWT authentication.
func NewClientWithAuth(config ClientConfig, auth *Authenticator) *Client {
	c := NewClient(config)
	c.authenticator = auth
	return c
}

// SetAuthenticator sets the authenticator for the client.
func (c *Client) SetAuthenticator(auth *Authenticator) {
	c.authenticator = auth
}

// FetchNextJob retrieves the next available print job from the API.
// Returns nil if no job is available.
func (c *Client) FetchNextJob(ctx context.Context, params *FetchNextJobParams) (*Job, error) {
	url := c.config.BaseURL + "/api/printer-agent/jobs/next"

	// Add query parameters
	if params != nil {
		query := make([]string, 0)
		if params.Type != "" {
			query = append(query, "type="+params.Type)
		}
		if params.PrinterCode != "" {
			query = append(query, "printer_code="+params.PrinterCode)
		}
		if params.LeaseDuration > 0 {
			query = append(query, fmt.Sprintf("lease_duration=%d", params.LeaseDuration))
		}
		if len(query) > 0 {
			url += "?"
			for i, q := range query {
				if i > 0 {
					url += "&"
				}
				url += q
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("fetching next job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var nextJobResp NextJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&nextJobResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return nextJobResp.Data, nil // Returns nil if no job available
}

// AckJobRequest is the request body for acknowledging a job.
type AckJobRequest struct {
	LeaseID      string `json:"lease_id"`
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// AckJob acknowledges that a job has been processed (success or failure).
func (c *Client) AckJob(ctx context.Context, jobID string, leaseID string, success bool, errorMessage string) error {
	url := c.config.BaseURL + "/api/printer-agent/jobs/" + jobID + "/ack"

	body, err := json.Marshal(AckJobRequest{
		LeaseID:      leaseID,
		Success:      success,
		ErrorMessage: errorMessage,
	})
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return fmt.Errorf("acking job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// setHeaders sets common headers on a request.
func (c *Client) setHeaders(req *http.Request) {
	c.setHeadersWithContext(req.Context(), req)
}

// setHeadersWithContext sets common headers on a request, using context for auth.
func (c *Client) setHeadersWithContext(ctx context.Context, req *http.Request) {
	req.Header.Set("Accept", "application/json")

	// Use authenticator if available, otherwise fall back to static API key
	if c.authenticator != nil {
		if token, err := c.authenticator.GetToken(ctx); err == nil && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	} else if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
}

// doWithRetry executes the request with retry logic for transient errors.
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Retry on 5xx errors
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}
