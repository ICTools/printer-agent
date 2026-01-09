package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	BaseURL   string
	APIKey    string
	APISecret string
	Insecure  bool // Skip TLS certificate verification (for local testing)
}

// TokenResponse is the response from the authentication endpoint.
type TokenResponse struct {
	Token     string       `json:"token"`
	ExpiresIn int          `json:"expires_in"`
	ExpiresAt int64        `json:"expires_at"`
	Type      string       `json:"type"`
	Agent     AgentInfo    `json:"agent"`
	Mercure   *MercureInfo `json:"mercure,omitempty"`
}

// MercureInfo contains Mercure SSE configuration.
type MercureInfo struct {
	Token string `json:"token"`
	URL   string `json:"url"`
	Topic string `json:"topic"`
}

// AgentInfo contains information about the authenticated agent.
type AgentInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Store string `json:"store"`
}

// Authenticator handles JWT authentication and token refresh.
type Authenticator struct {
	config     AuthConfig
	httpClient *http.Client

	mu           sync.RWMutex
	token        string
	expiresAt    time.Time
	agentInfo    *AgentInfo
	mercureInfo  *MercureInfo
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(config AuthConfig) *Authenticator {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Skip TLS verification for local testing
	if config.Insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return &Authenticator{
		config:     config,
		httpClient: httpClient,
	}
}

// Authenticate obtains a JWT token from the authentication endpoint.
func (a *Authenticator) Authenticate(ctx context.Context) (*TokenResponse, error) {
	url := a.config.BaseURL + "/api/authentication_token"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating auth request: %w", err)
	}

	req.Header.Set("X-Api-Key", a.config.APIKey)
	req.Header.Set("X-Api-Secret", a.config.APISecret)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("authentication failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding auth response: %w", err)
	}

	// Store token and Mercure info
	a.mu.Lock()
	a.token = tokenResp.Token
	a.expiresAt = time.Unix(tokenResp.ExpiresAt, 0)
	a.agentInfo = &tokenResp.Agent
	a.mercureInfo = tokenResp.Mercure
	a.mu.Unlock()

	return &tokenResp, nil
}

// GetToken returns the current token, refreshing if necessary.
func (a *Authenticator) GetToken(ctx context.Context) (string, error) {
	a.mu.RLock()
	token := a.token
	expiresAt := a.expiresAt
	a.mu.RUnlock()

	// Refresh if token expires in less than 5 minutes
	if token == "" || time.Until(expiresAt) < 5*time.Minute {
		_, err := a.Authenticate(ctx)
		if err != nil {
			return "", err
		}
		a.mu.RLock()
		token = a.token
		a.mu.RUnlock()
	}

	return token, nil
}

// GetAgentInfo returns information about the authenticated agent.
func (a *Authenticator) GetAgentInfo() *AgentInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.agentInfo
}

// GetMercureInfo returns the Mercure SSE configuration.
// Returns nil if Mercure is not configured on the server.
func (a *Authenticator) GetMercureInfo() *MercureInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mercureInfo
}

// HasMercure returns true if Mercure SSE is available.
func (a *Authenticator) HasMercure() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mercureInfo != nil && a.mercureInfo.URL != "" && a.mercureInfo.Token != ""
}

// IsAuthenticated returns true if we have a valid token.
func (a *Authenticator) IsAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.token != "" && time.Now().Before(a.expiresAt)
}

// TokenExpiresAt returns when the current token expires.
func (a *Authenticator) TokenExpiresAt() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.expiresAt
}

// Ping sends a heartbeat to the server to indicate the agent is still connected.
func (a *Authenticator) Ping(ctx context.Context) error {
	token, err := a.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("getting token for ping: %w", err)
	}

	url := a.config.BaseURL + "/api/printer-agent/ping"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("creating ping request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending ping: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ping failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ServerPrinter represents a printer as stored on the server.
type ServerPrinter struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	TypeLabel   string `json:"typeLabel"`
	Description string `json:"description"`
	IsActive    bool   `json:"isActive"`
}

// GetPrintersResponse is the response from GET /printers.
type GetPrintersResponse struct {
	Success bool            `json:"success"`
	Data    []ServerPrinter `json:"data"`
}

// AgentStatus represents the agent status from the server.
type AgentStatus struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Code          string      `json:"code"`
	IsActive      bool        `json:"isActive"`
	IsOnline      bool        `json:"isOnline"`
	LastPingAt    string      `json:"lastPingAt"`
	Store         StoreInfo   `json:"store"`
	PrintersCount int         `json:"printersCount"`
}

// StoreInfo contains store information.
type StoreInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetStatusResponse is the response from GET /status.
type GetStatusResponse struct {
	Success bool        `json:"success"`
	Data    AgentStatus `json:"data"`
}

// PrinterSyncInfo represents a printer to sync with the server.
type PrinterSyncInfo struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"` // Valid types: receipt, label, a4
	Description string `json:"description"`
}

// SyncPrintersRequest is the request body for syncing printers.
type SyncPrintersRequest struct {
	Printers []PrinterSyncInfo `json:"printers"`
}

// SyncPrintersResponse is the response from the sync endpoint.
type SyncPrintersResponse struct {
	Success bool                 `json:"success"`
	Data    SyncPrintersDataResp `json:"data"`
}

// SyncPrintersDataResp contains sync statistics.
type SyncPrintersDataResp struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
	Total   int `json:"total"`
}

// SyncPrinters sends the list of connected printers to the server.
func (a *Authenticator) SyncPrinters(ctx context.Context, printers []PrinterSyncInfo) (*SyncPrintersResponse, error) {
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting token for sync: %w", err)
	}

	url := a.config.BaseURL + "/api/printer-agent/printers"

	body, err := json.Marshal(SyncPrintersRequest{Printers: printers})
	if err != nil {
		return nil, fmt.Errorf("marshaling sync request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating sync request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sync failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var syncResp SyncPrintersResponse
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return nil, fmt.Errorf("decoding sync response: %w", err)
	}

	return &syncResp, nil
}

// GetPrinters retrieves the list of printers registered on the server for this agent.
func (a *Authenticator) GetPrinters(ctx context.Context) ([]ServerPrinter, error) {
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting token: %w", err)
	}

	url := a.config.BaseURL + "/api/printer-agent/printers"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var printersResp GetPrintersResponse
	if err := json.NewDecoder(resp.Body).Decode(&printersResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return printersResp.Data, nil
}

// GetStatus retrieves the agent status from the server (self-check).
func (a *Authenticator) GetStatus(ctx context.Context) (*AgentStatus, error) {
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting token: %w", err)
	}

	url := a.config.BaseURL + "/api/printer-agent/status"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var statusResp GetStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &statusResp.Data, nil
}
