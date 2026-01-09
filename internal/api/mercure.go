package api

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MercureEvent represents an event received from the Mercure hub.
type MercureEvent struct {
	Type        string `json:"event"`
	JobID       string `json:"job_id"`
	JobType     string `json:"type"`
	PrinterCode string `json:"printer_code"`
}

// MercureClient handles SSE subscription to the Mercure hub.
type MercureClient struct {
	hubURL     string
	token      string
	topic      string
	httpClient *http.Client
}

// NewMercureClient creates a new Mercure SSE client.
func NewMercureClient(hubURL, token, topic string, insecure bool) *MercureClient {
	httpClient := &http.Client{
		// No timeout - SSE connections are long-lived
		Timeout: 0,
	}

	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return &MercureClient{
		hubURL:     hubURL,
		token:      token,
		topic:      topic,
		httpClient: httpClient,
	}
}

// Subscribe connects to the Mercure hub and streams events.
// It blocks until the context is canceled or an error occurs.
// Events are sent to the provided channel.
func (m *MercureClient) Subscribe(ctx context.Context, events chan<- MercureEvent) error {
	// Build subscription URL with topic
	subURL, err := url.Parse(m.hubURL)
	if err != nil {
		return fmt.Errorf("parsing hub URL: %w", err)
	}

	query := subURL.Query()
	query.Set("topic", m.topic)
	subURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL.String(), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Set authorization header with Mercure token
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to hub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("subscription failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Read SSE stream
	return m.readStream(ctx, resp.Body, events)
}

// readStream parses the SSE stream and sends events to the channel.
func (m *MercureClient) readStream(ctx context.Context, body io.Reader, events chan<- MercureEvent) error {
	scanner := bufio.NewScanner(body)
	var eventData strings.Builder

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading stream: %w", err)
			}
			// Connection closed by server
			return fmt.Errorf("connection closed by server")
		}

		line := scanner.Text()

		// Empty line marks end of event
		if line == "" {
			if eventData.Len() > 0 {
				var event MercureEvent
				if err := json.Unmarshal([]byte(eventData.String()), &event); err == nil {
					select {
					case events <- event:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				eventData.Reset()
			}
			continue
		}

		// Parse SSE format
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			eventData.WriteString(data)
		}
		// Ignore other SSE fields (event:, id:, retry:) for now
	}
}

// SubscribeWithReconnect subscribes to the hub and automatically reconnects on failure.
// It uses exponential backoff between reconnection attempts.
func (m *MercureClient) SubscribeWithReconnect(ctx context.Context, events chan<- MercureEvent, onConnect func(), onDisconnect func(error)) {
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if onConnect != nil {
			onConnect()
		}

		err := m.Subscribe(ctx, events)

		if ctx.Err() != nil {
			// Context canceled, exit gracefully
			return
		}

		if onDisconnect != nil {
			onDisconnect(err)
		}

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
