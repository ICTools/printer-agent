package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"print-agent/internal/api"
	"print-agent/internal/dispatcher"
	"print-agent/internal/registry"
)

// Config holds the agent configuration.
type Config struct {
	PollInterval         time.Duration
	FallbackPollInterval time.Duration // Used when SSE is active (less frequent)
	PingInterval         time.Duration
	SyncInterval         time.Duration
	MaxBackoff           time.Duration
	InitialBackoff       time.Duration
	BackoffFactor        float64
	Verbose              bool
	DryRun               bool // If true, jobs are logged but not executed
	DisableSSE           bool // If true, always use polling instead of SSE
	Insecure             bool // Skip TLS certificate verification
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		PollInterval:         2 * time.Second,  // Poll every 2s for jobs (when SSE not available)
		FallbackPollInterval: 30 * time.Second, // Fallback poll when SSE is active
		PingInterval:         30 * time.Second, // Heartbeat every ~30s
		SyncInterval:         10 * time.Second,
		MaxBackoff:           60 * time.Second,
		InitialBackoff:       1 * time.Second,
		BackoffFactor:        2.0,
		Verbose:              false,
		DryRun:               false,
		DisableSSE:           false,
		Insecure:             false,
	}
}

// Stats holds agent statistics.
type Stats struct {
	mu             sync.RWMutex
	JobsProcessed  int64
	JobsFailed     int64
	LastPollAt     time.Time
	LastJobAt      time.Time
	ConsecutiveErr int
}

// Agent is the main print agent that polls for jobs.
type Agent struct {
	config        Config
	client        *api.Client
	authenticator *api.Authenticator
	registry      *registry.Registry
	dispatcher    *dispatcher.Dispatcher
	stats         Stats

	stopCh    chan struct{}
	wg        sync.WaitGroup
	jobNotify chan struct{} // Signals when a new job is available (from SSE)
	sseActive bool          // True when SSE is connected
	sseMu     sync.RWMutex
}

// New creates a new agent.
func New(client *api.Client, reg *registry.Registry, config Config) *Agent {
	return &Agent{
		config:     config,
		client:     client,
		registry:   reg,
		dispatcher: dispatcher.NewDispatcher(reg),
		stopCh:     make(chan struct{}),
		jobNotify:  make(chan struct{}, 1), // Buffered to avoid blocking SSE loop
	}
}

// NewWithAuth creates a new agent with JWT authentication.
func NewWithAuth(client *api.Client, auth *api.Authenticator, reg *registry.Registry, config Config) *Agent {
	a := New(client, reg, config)
	a.authenticator = auth
	return a
}

// Start begins the polling loop. It blocks until Stop is called or context is canceled.
func (a *Agent) Start(ctx context.Context) error {
	a.logInfo("Agent starting...")

	// Authenticate if we have an authenticator
	if a.authenticator != nil {
		a.logInfo("Authenticating...")
		tokenResp, err := a.authenticator.Authenticate(ctx)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		a.logInfo("Authenticated as agent: %s (%s)", tokenResp.Agent.Name, tokenResp.Agent.Store)
		a.logInfo("Token expires at: %s", a.authenticator.TokenExpiresAt().Format("15:04:05"))

		// Start ping loop
		a.wg.Add(1)
		go a.pingLoop(ctx)

		// Start sync loop
		a.wg.Add(1)
		go a.syncLoop(ctx)
	}

	// Detect printers at startup (uses DetectChanges to establish baseline and sync if needed)
	a.logInfo("Scanning for printers...")
	changes := a.registry.DetectChanges()
	printers := a.registry.GetAvailablePrinters()

	a.logInfo("Detected %d printer(s)", len(printers))
	for _, p := range printers {
		a.logInfo("  - %s (%s) [%s]", p.ID, p.Type, p.DevicePath)
	}

	if len(printers) == 0 {
		a.logInfo("Warning: no printers detected, jobs may fail")
	}

	// Only sync with server if printers were detected (changes on first run)
	if a.authenticator != nil && changes.Changed {
		a.syncPrinters(ctx)
	}

	// Start SSE loop if Mercure is available and not disabled
	useSSE := false
	if a.authenticator != nil && !a.config.DisableSSE && a.authenticator.HasMercure() {
		a.logInfo("Mercure SSE available, starting event-driven mode")
		a.wg.Add(1)
		go a.sseLoop(ctx)
		useSSE = true
	} else if a.config.DisableSSE {
		a.logInfo("SSE disabled by configuration, using polling")
	} else {
		a.logInfo("Mercure not available, using polling mode")
	}

	a.wg.Add(1)
	go a.pollLoop(ctx, useSSE)

	// Wait for stop signal or context cancellation
	select {
	case <-ctx.Done():
		a.logInfo("Context canceled, shutting down...")
	case <-a.stopCh:
		a.logInfo("Stop signal received, shutting down...")
	}

	// Wait for poll loop to finish
	a.wg.Wait()
	a.logInfo("Agent stopped")

	return nil
}

// Stop signals the agent to stop gracefully.
func (a *Agent) Stop() {
	close(a.stopCh)
}

// GetStats returns a copy of the current stats.
func (a *Agent) GetStats() Stats {
	a.stats.mu.RLock()
	defer a.stats.mu.RUnlock()
	return Stats{
		JobsProcessed:  a.stats.JobsProcessed,
		JobsFailed:     a.stats.JobsFailed,
		LastPollAt:     a.stats.LastPollAt,
		LastJobAt:      a.stats.LastJobAt,
		ConsecutiveErr: a.stats.ConsecutiveErr,
	}
}

// pollLoop is the main polling loop.
// If useSSE is true, it uses a longer interval and also listens for job notifications.
func (a *Agent) pollLoop(ctx context.Context, useSSE bool) {
	defer a.wg.Done()

	currentBackoff := a.config.InitialBackoff

	// Use longer interval when SSE is active (fallback safety net)
	interval := a.config.PollInterval
	if useSSE {
		interval = a.config.FallbackPollInterval
		a.logInfo("Poll interval: %s (fallback, SSE is primary)", interval)
	} else {
		a.logInfo("Poll interval: %s", interval)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial poll
	a.poll(ctx, &currentBackoff)

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-a.jobNotify:
			// SSE notified us of a new job, fetch immediately
			a.logVerbose("SSE notification received, fetching job")
			a.poll(ctx, &currentBackoff)
		case <-ticker.C:
			a.poll(ctx, &currentBackoff)
		}
	}
}

// sseLoop connects to Mercure and listens for job events.
func (a *Agent) sseLoop(ctx context.Context) {
	defer a.wg.Done()

	mercureInfo := a.authenticator.GetMercureInfo()

	client := api.NewMercureClient(mercureInfo.URL, mercureInfo.Token, mercureInfo.Topic, a.config.Insecure)
	events := make(chan api.MercureEvent, 10)

	// Handle connection state changes
	onConnect := func() {
		a.logVerbose("SSE connecting to Mercure hub...")
	}

	onDisconnect := func(err error) {
		a.sseMu.Lock()
		a.sseActive = false
		a.sseMu.Unlock()
		if err != nil {
			a.logError("SSE disconnected: %v", err)
		} else {
			a.logInfo("SSE disconnected")
		}
	}

	// Start subscription in background
	go client.SubscribeWithReconnect(ctx, events, onConnect, onDisconnect)

	// Process events
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case event := <-events:
			// Mark SSE as active on first event
			a.sseMu.Lock()
			if !a.sseActive {
				a.sseActive = true
				a.logInfo("SSE connected to Mercure hub")
			}
			a.sseMu.Unlock()

			a.logInfo("SSE event: %s (job=%s, type=%s, printer=%s)",
				event.Type, event.JobID, event.JobType, event.PrinterCode)

			// Notify poll loop that a job is available
			select {
			case a.jobNotify <- struct{}{}:
			default:
				// Channel full, poll loop will pick it up
			}
		}
	}
}

// IsSSEActive returns true if SSE is currently connected.
func (a *Agent) IsSSEActive() bool {
	a.sseMu.RLock()
	defer a.sseMu.RUnlock()
	return a.sseActive
}

// pingLoop sends periodic pings to the server.
func (a *Agent) pingLoop(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.PingInterval)
	defer ticker.Stop()

	// Initial ping
	a.ping(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.ping(ctx)
		}
	}
}

// ping sends a heartbeat to the server.
func (a *Agent) ping(ctx context.Context) {
	if a.authenticator == nil {
		return
	}

	if err := a.authenticator.Ping(ctx); err != nil {
		a.logError("Ping failed: %v", err)
	} else {
		a.logVerbose("Ping OK")
	}
}

// syncLoop periodically checks for printer changes and syncs with server.
func (a *Agent) syncLoop(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.checkAndSyncPrinters(ctx)
		}
	}
}

// checkAndSyncPrinters detects printer changes and syncs if needed.
func (a *Agent) checkAndSyncPrinters(ctx context.Context) {
	changes := a.registry.DetectChanges()

	if changes.Changed {
		for _, p := range changes.Added {
			a.logInfo("Printer connected: %s (%s)", p.ID, p.Type)
		}
		for _, p := range changes.Removed {
			a.logInfo("Printer disconnected: %s (%s)", p.ID, p.Type)
		}
		a.syncPrinters(ctx)
	}
}

// syncPrinters sends the current printer list to the server.
func (a *Agent) syncPrinters(ctx context.Context) {
	if a.authenticator == nil {
		return
	}

	printers := a.registry.GetAvailablePrinters()
	syncInfo := make([]api.PrinterSyncInfo, 0, len(printers))

	for _, p := range printers {
		syncInfo = append(syncInfo, api.PrinterSyncInfo{
			Code:        p.ID,
			Name:        p.ID,
			Type:        string(p.Type),
			Description: p.DevicePath,
		})
	}

	a.logInfo("Syncing %d printer(s) with server...", len(syncInfo))

	resp, err := a.authenticator.SyncPrinters(ctx, syncInfo)
	if err != nil {
		a.logError("Printer sync failed: %v", err)
	} else {
		a.logInfo("Printer sync OK (created: %d, updated: %d, removed: %d, total: %d)",
			resp.Data.Created, resp.Data.Updated, resp.Data.Removed, resp.Data.Total)
	}
}

// poll fetches and processes the next job.
func (a *Agent) poll(ctx context.Context, currentBackoff *time.Duration) {
	a.stats.mu.Lock()
	a.stats.LastPollAt = time.Now()
	a.stats.mu.Unlock()

	// Refresh printer availability
	a.registry.RefreshAvailability()

	// Fetch next job
	job, err := a.client.FetchNextJob(ctx, nil)
	if err != nil {
		a.stats.mu.Lock()
		a.stats.ConsecutiveErr++
		a.stats.mu.Unlock()

		a.logError("Failed to fetch job: %v", err)

		// Apply backoff
		a.logInfo("Backing off for %s", *currentBackoff)
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-time.After(*currentBackoff):
		}

		// Increase backoff
		*currentBackoff = time.Duration(float64(*currentBackoff) * a.config.BackoffFactor)
		if *currentBackoff > a.config.MaxBackoff {
			*currentBackoff = a.config.MaxBackoff
		}
		return
	}

	// Reset backoff on success
	*currentBackoff = a.config.InitialBackoff
	a.stats.mu.Lock()
	a.stats.ConsecutiveErr = 0
	a.stats.mu.Unlock()

	if job == nil {
		a.logVerbose("No pending jobs")
		return
	}

	a.processJob(ctx, job)
}

// processJob dispatches a single job and reports the result.
func (a *Agent) processJob(ctx context.Context, job *api.Job) {
	a.logInfo("Processing job %s (type: %s, printer: %s, retry: %d)",
		job.ID, job.Type, job.PrinterCode(), job.RetryCount)

	// Log payload in verbose mode or dry-run mode
	if a.config.Verbose || a.config.DryRun {
		a.logInfo("  Lease ID: %s", job.LeaseID)
		a.logInfo("  Lease until: %s", job.LeaseUntil)
		a.logInfo("  Printer: %s (%s)", job.Printer.Name, job.Printer.Type)
		a.logInfo("  Payload: %s", string(job.Payload))
	}

	// Dry-run mode: log and ack without executing
	if a.config.DryRun {
		a.logInfo("[DRY-RUN] Job %s would be executed (skipped)", job.ID)

		a.stats.mu.Lock()
		a.stats.LastJobAt = time.Now()
		a.stats.JobsProcessed++
		a.stats.mu.Unlock()

		// Acknowledge success (so the server doesn't resend the job)
		if ackErr := a.client.AckJob(ctx, job.ID, job.LeaseID, true, ""); ackErr != nil {
			a.logError("Failed to acknowledge job: %v", ackErr)
		}
		return
	}

	err := a.dispatcher.Dispatch(*job)

	a.stats.mu.Lock()
	a.stats.LastJobAt = time.Now()
	if err != nil {
		a.stats.JobsFailed++
	} else {
		a.stats.JobsProcessed++
	}
	a.stats.mu.Unlock()

	if err != nil {
		a.logError("Job %s failed: %v", job.ID, err)

		// Report failure to API with lease_id
		if ackErr := a.client.AckJob(ctx, job.ID, job.LeaseID, false, err.Error()); ackErr != nil {
			a.logError("Failed to ack job failure: %v", ackErr)
		}
		return
	}

	a.logInfo("Job %s completed successfully", job.ID)

	// Acknowledge success with lease_id
	if ackErr := a.client.AckJob(ctx, job.ID, job.LeaseID, true, ""); ackErr != nil {
		a.logError("Failed to acknowledge job: %v", ackErr)
	}
}

// Logging helpers

func (a *Agent) logInfo(format string, args ...interface{}) {
	log.Printf("[INFO] "+format, args...)
}

func (a *Agent) logError(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

func (a *Agent) logVerbose(format string, args ...interface{}) {
	if a.config.Verbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// String returns a string representation of the agent status.
func (a *Agent) String() string {
	stats := a.GetStats()
	return fmt.Sprintf("Agent[processed=%d, failed=%d, lastPoll=%s]",
		stats.JobsProcessed, stats.JobsFailed, stats.LastPollAt.Format(time.RFC3339))
}
