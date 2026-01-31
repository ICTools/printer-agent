# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Go-based print job distribution agent for POS systems. Polls or listens (SSE/Mercure) to a remote API for print jobs and dispatches them to connected printers (receipts, labels, stickers).

## Commands

```bash
# Build
go build -o print-agent ./cmd/print-agent

# Run all tests
go test ./...

# Run a single package's tests
go test ./internal/agent
go test ./internal/dispatcher

# Run a specific test
go test ./internal/agent -run TestAgentStart

# Run with verbose output
go test -v ./internal/agent
```

## Architecture

**Dual-mode agent**: SSE via Mercure (primary, real-time) + HTTP polling (fallback). Falls back to polling if Mercure is unavailable; polling interval widens (2s → 30s) when SSE is active.

**Entry point**: `cmd/print-agent/main.go` — CLI with subcommands (`run`, `detect`, `receipt-test`, `open-drawer`, `label`, `sticker-address`, `sticker-image`).

**Core loop** (`internal/agent/agent.go`): `Start()` runs concurrent goroutines for SSE listening, polling, heartbeat pings (30s), and printer sync (10s). Jobs are fetched via `FetchNextJob()`, dispatched, then acknowledged.

**Key modules**:
- `internal/api/` — HTTP client, JWT auth with auto-refresh (<5min), Mercure SSE client, job models/parsing
- `internal/dispatcher/` — Routes jobs to printer drivers by type. Per-printer mutexes prevent concurrent access. 3-attempt retry with exponential backoff.
- `internal/registry/` — Detects printers at `/dev/usb/*`, `/dev/lp*`, `/dev/ttyUSB*`. Tracks availability changes and syncs with server.
- `internal/receipt/` — ESC/POS receipt printer driver (native Go)
- `internal/label/` — Wraps Python scripts (`print.py`, `print_sticker.py`) for label/sticker printing via subprocess

**Job types**: `receipt`, `put_aside`, `label`, `sticker_image`, `open_drawer`

**API contract**: Auth via `POST /api/authentication_token` (X-Api-Key/X-Api-Secret headers), jobs via `/api/printer-agent/jobs/next` with lease-based locking, ack via `/api/printer-agent/jobs/{id}/ack`.
