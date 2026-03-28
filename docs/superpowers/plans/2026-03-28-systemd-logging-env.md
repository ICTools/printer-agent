# Systemd Service, Journald Logging & Env File — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run print-agent as a user-level systemd service at boot, with logs in journald and configuration via an `EnvironmentFile=`.

**Architecture:** Three static files (service unit, env template, deploy docs) — no Go code changes. The systemd unit uses `EnvironmentFile=` to load env vars, the Go binary reads them via existing `os.Getenv()` calls, and journald captures stdout/stderr automatically.

**Tech Stack:** systemd (user-level), journald, bash

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `deploy/print-agent.service` | systemd user-level unit file |
| Create | `.env.example` | Template of all supported environment variables |
| Create | `deploy/install.sh` | One-shot install script (copies files, enables service, enables linger) |
| Create | `deploy/README.md` | Installation and management instructions |

No Go source files are modified.

---

### Task 1: Create the systemd service unit

**Files:**
- Create: `deploy/print-agent.service`

- [ ] **Step 1: Create the service file**

```ini
[Unit]
Description=Print Agent — POS print job dispatcher
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%h/.config/print-agent/env
ExecStart=%h/Documents/printer-agent/print-agent run
Restart=on-failure
RestartSec=5
# Stop gracefully (agent handles SIGTERM)
KillSignal=SIGTERM
TimeoutStopSec=10

[Install]
WantedBy=default.target
```

Notes on systemd specifiers:
- `%h` expands to the user's home directory at runtime — avoids hardcoding `/home/david`.

- [ ] **Step 2: Verify the unit file parses correctly**

Run:
```bash
systemd-analyze --user verify deploy/print-agent.service
```

Expected: no errors (warnings about the unit not being loaded yet are OK).

- [ ] **Step 3: Commit**

```bash
git add deploy/print-agent.service
git commit -m "feat: add systemd user-level service unit for print-agent"
```

---

### Task 2: Create the `.env.example` template

**Files:**
- Create: `.env.example`

- [ ] **Step 1: Create the env template**

The file documents every `PRINT_AGENT_*` environment variable supported by `cmd/print-agent/main.go:259-270` (the `cmdRun` flag defaults).

```bash
# print-agent environment configuration
# Copy this file to ~/.config/print-agent/env and fill in your values.
#
# Format rules (systemd EnvironmentFile):
#   - No 'export' prefix
#   - Use double quotes if value contains spaces: KEY="value with spaces"
#   - Lines starting with # are comments
#   - Empty lines are ignored

# Required — API connection
PRINT_AGENT_API_URL=https://your-api.example.com
PRINT_AGENT_API_KEY=your-api-key
PRINT_AGENT_API_SECRET="your-api-secret"

# Optional — intervals (Go duration format: 2s, 30s, 1m, etc.)
# PRINT_AGENT_POLL_INTERVAL=2s
# PRINT_AGENT_PING_INTERVAL=30s
# PRINT_AGENT_SYNC_INTERVAL=10s

# Optional — health check server (leave empty to disable)
# PRINT_AGENT_HEALTH_ADDR=:8080
```

- [ ] **Step 2: Commit**

```bash
git add .env.example
git commit -m "feat: add .env.example template for systemd EnvironmentFile"
```

---

### Task 3: Create the install script

**Files:**
- Create: `deploy/install.sh`

- [ ] **Step 1: Write the install script**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
SERVICE_NAME="print-agent"
CONFIG_DIR="${HOME}/.config/${SERVICE_NAME}"
SYSTEMD_DIR="${HOME}/.config/systemd/user"

echo "=== print-agent installer ==="
echo

# 1. Build the binary
echo "[1/5] Building print-agent..."
cd "$REPO_DIR"
go build -o print-agent ./cmd/print-agent
echo "  Built: ${REPO_DIR}/print-agent"

# 2. Create config directory and env file if missing
echo "[2/5] Setting up config directory..."
mkdir -p "$CONFIG_DIR"
if [ ! -f "${CONFIG_DIR}/env" ]; then
    cp "${REPO_DIR}/.env.example" "${CONFIG_DIR}/env"
    echo "  Created: ${CONFIG_DIR}/env (from .env.example)"
    echo "  >>> EDIT THIS FILE with your API credentials before starting the service <<<"
else
    echo "  Config already exists: ${CONFIG_DIR}/env (not overwritten)"
fi

# 3. Install systemd unit
echo "[3/5] Installing systemd service..."
mkdir -p "$SYSTEMD_DIR"
cp "${SCRIPT_DIR}/${SERVICE_NAME}.service" "${SYSTEMD_DIR}/${SERVICE_NAME}.service"
systemctl --user daemon-reload
echo "  Installed: ${SYSTEMD_DIR}/${SERVICE_NAME}.service"

# 4. Enable lingering (start service at boot, even without login)
echo "[4/5] Enabling lingering for user $(whoami)..."
loginctl enable-linger "$(whoami)"
echo "  Linger enabled"

# 5. Enable (but don't start yet — user needs to configure env first)
echo "[5/5] Enabling service..."
systemctl --user enable "$SERVICE_NAME"
echo "  Service enabled (will start at next boot)"

echo
echo "=== Installation complete ==="
echo
echo "Next steps:"
echo "  1. Edit your config:    nano ${CONFIG_DIR}/env"
echo "  2. Start the service:   systemctl --user start ${SERVICE_NAME}"
echo "  3. Check status:        systemctl --user status ${SERVICE_NAME}"
echo "  4. View logs:           journalctl --user -u ${SERVICE_NAME} -f"
```

- [ ] **Step 2: Make it executable**

Run:
```bash
chmod +x deploy/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add deploy/install.sh
git commit -m "feat: add install script for systemd service setup"
```

---

### Task 4: Create the deploy README

**Files:**
- Create: `deploy/README.md`

- [ ] **Step 1: Write the README**

```markdown
# Deploying print-agent as a systemd service

## Quick install

```bash
./deploy/install.sh
```

This script:
1. Builds the `print-agent` binary
2. Creates `~/.config/print-agent/env` from `.env.example` (if it doesn't exist)
3. Installs the systemd user service
4. Enables lingering (service starts at boot without login)
5. Enables the service

## Manual install

### 1. Build

```bash
go build -o print-agent ./cmd/print-agent
```

### 2. Configure environment

```bash
mkdir -p ~/.config/print-agent
cp .env.example ~/.config/print-agent/env
nano ~/.config/print-agent/env    # fill in API credentials
```

The env file uses systemd `EnvironmentFile=` format:
- No `export` prefix
- Double-quote values with spaces: `KEY="value with spaces"`
- `#` for comments

### 3. Install the service

```bash
mkdir -p ~/.config/systemd/user
cp deploy/print-agent.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable print-agent
```

### 4. Enable start at boot (without login)

```bash
loginctl enable-linger $(whoami)
```

### 5. Start

```bash
systemctl --user start print-agent
```

## Managing the service

| Action | Command |
|--------|---------|
| Start | `systemctl --user start print-agent` |
| Stop | `systemctl --user stop print-agent` |
| Restart | `systemctl --user restart print-agent` |
| Status | `systemctl --user status print-agent` |
| Enable at boot | `systemctl --user enable print-agent` |
| Disable at boot | `systemctl --user disable print-agent` |

## Viewing logs

Logs go to journald (stdout/stderr are captured automatically).

```bash
# Follow logs in real time
journalctl --user -u print-agent -f

# Today's logs
journalctl --user -u print-agent --since today

# Errors only
journalctl --user -u print-agent -p err

# Last 100 lines
journalctl --user -u print-agent -n 100
```

Log rotation is handled by journald (configured in `/etc/systemd/journald.conf`).

## Uninstall

```bash
systemctl --user stop print-agent
systemctl --user disable print-agent
rm ~/.config/systemd/user/print-agent.service
systemctl --user daemon-reload
```
```

- [ ] **Step 2: Commit**

```bash
git add deploy/README.md
git commit -m "docs: add deployment instructions for systemd service"
```

---

### Task 5: End-to-end verification

- [ ] **Step 1: Run the install script**

```bash
./deploy/install.sh
```

Expected: all 5 steps complete without error.

- [ ] **Step 2: Edit the env file with real credentials**

```bash
nano ~/.config/print-agent/env
```

Fill in `PRINT_AGENT_API_URL`, `PRINT_AGENT_API_KEY`, `PRINT_AGENT_API_SECRET`.

- [ ] **Step 3: Start the service and verify**

```bash
systemctl --user start print-agent
systemctl --user status print-agent
```

Expected: `Active: active (running)`.

- [ ] **Step 4: Check logs**

```bash
journalctl --user -u print-agent -n 20
```

Expected: startup lines (`Starting print-agent...`, `API URL: ...`, etc.)

- [ ] **Step 5: Test graceful stop**

```bash
systemctl --user stop print-agent
systemctl --user status print-agent
```

Expected: `Active: inactive (dead)`, exit code 0.

- [ ] **Step 6: Test auto-restart on failure**

```bash
systemctl --user start print-agent
# kill the process abruptly
kill -9 $(systemctl --user show print-agent --property=MainPID --value)
# wait 6 seconds (RestartSec=5)
sleep 6
systemctl --user status print-agent
```

Expected: `Active: active (running)` — systemd restarted it.
