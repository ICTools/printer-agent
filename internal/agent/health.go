package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HealthServer provides HTTP endpoints for health checks and metrics.
type HealthServer struct {
	agent  *Agent
	server *http.Server
	addr   string
}

// HealthResponse is the response for the /health endpoint.
type HealthResponse struct {
	Status    string    `json:"status"`
	Uptime    string    `json:"uptime"`
	StartedAt time.Time `json:"started_at"`
}

// MetricsResponse is the response for the /metrics endpoint.
type MetricsResponse struct {
	JobsProcessed  int64     `json:"jobs_processed"`
	JobsFailed     int64     `json:"jobs_failed"`
	LastPollAt     time.Time `json:"last_poll_at,omitempty"`
	LastJobAt      time.Time `json:"last_job_at,omitempty"`
	ConsecutiveErr int       `json:"consecutive_errors"`
	Printers       int       `json:"printers_registered"`
	PrintersOnline int       `json:"printers_online"`
}

// StatusResponse is the response for the /status endpoint (combined local + server).
type StatusResponse struct {
	Local  LocalStatus  `json:"local"`
	Server ServerStatus `json:"server,omitempty"`
}

// LocalStatus contains local agent status.
type LocalStatus struct {
	Status         string `json:"status"`
	PrintersOnline int    `json:"printers_online"`
	JobsProcessed  int64  `json:"jobs_processed"`
	JobsFailed     int64  `json:"jobs_failed"`
}

// ServerStatus contains server-side agent status.
type ServerStatus struct {
	Available     bool   `json:"available"`
	AgentName     string `json:"agent_name,omitempty"`
	StoreName     string `json:"store_name,omitempty"`
	IsOnline      bool   `json:"is_online,omitempty"`
	LastPingAt    string `json:"last_ping_at,omitempty"`
	PrintersCount int    `json:"printers_count,omitempty"`
	Error         string `json:"error,omitempty"`
}

// NewHealthServer creates a new health server.
func NewHealthServer(agent *Agent, addr string) *HealthServer {
	return &HealthServer{
		agent: agent,
		addr:  addr,
	}
}

// Start starts the health server in a goroutine.
func (h *HealthServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/metrics", h.handleMetrics)
	mux.HandleFunc("/status", h.handleStatus)
	mux.HandleFunc("/", h.handleRoot)

	h.server = &http.Server{
		Addr:         h.addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Create listener first to get the actual address (important for port 0)
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Update server address with actual bound address
	h.server.Addr = ln.Addr().String()

	go func() {
		if err := h.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			h.agent.logError("Health server error: %v", err)
		}
	}()

	h.agent.logInfo("Health server listening on %s", h.server.Addr)
	return nil
}

// Stop gracefully stops the health server.
func (h *HealthServer) Stop(ctx context.Context) error {
	if h.server == nil {
		return nil
	}
	return h.server.Shutdown(ctx)
}

// handleRoot handles the root endpoint.
func (h *HealthServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "print-agent\n\nEndpoints:\n  /health  - Health check\n  /metrics - Agent metrics\n  /status  - Combined local + server status\n")
}

// handleHealth handles the /health endpoint.
func (h *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := h.agent.GetStats()

	status := "healthy"
	if stats.ConsecutiveErr > 5 {
		status = "degraded"
	}

	resp := HealthResponse{
		Status:    status,
		Uptime:    time.Since(stats.LastPollAt).Round(time.Second).String(),
		StartedAt: stats.LastPollAt,
	}

	w.Header().Set("Content-Type", "application/json")
	if status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(resp)
}

// handleMetrics handles the /metrics endpoint.
func (h *HealthServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	stats := h.agent.GetStats()

	printers := h.agent.registry.List()
	online := 0
	for _, p := range printers {
		if p.Available {
			online++
		}
	}

	resp := MetricsResponse{
		JobsProcessed:  stats.JobsProcessed,
		JobsFailed:     stats.JobsFailed,
		LastPollAt:     stats.LastPollAt,
		LastJobAt:      stats.LastJobAt,
		ConsecutiveErr: stats.ConsecutiveErr,
		Printers:       len(printers),
		PrintersOnline: online,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleStatus handles the /status endpoint (combined local + server status).
func (h *HealthServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.agent.GetStats()

	// Count online printers
	printers := h.agent.registry.GetAvailablePrinters()

	// Local status
	localStatus := "healthy"
	if stats.ConsecutiveErr > 5 {
		localStatus = "degraded"
	}

	resp := StatusResponse{
		Local: LocalStatus{
			Status:         localStatus,
			PrintersOnline: len(printers),
			JobsProcessed:  stats.JobsProcessed,
			JobsFailed:     stats.JobsFailed,
		},
	}

	// Fetch server status if authenticator is available
	if h.agent.authenticator != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		serverStatus, err := h.agent.authenticator.GetStatus(ctx)
		if err != nil {
			resp.Server = ServerStatus{
				Available: false,
				Error:     err.Error(),
			}
		} else {
			resp.Server = ServerStatus{
				Available:     true,
				AgentName:     serverStatus.Name,
				StoreName:     serverStatus.Store.Name,
				IsOnline:      serverStatus.IsOnline,
				LastPingAt:    serverStatus.LastPingAt,
				PrintersCount: serverStatus.PrintersCount,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
