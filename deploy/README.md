# Deploying print-agent as a systemd service

## Quick install

```bash
./deploy/install.sh
```

This script:
1. Builds the `print-agent` binary
2. Installs the binary to `~/.local/bin/print-agent`
3. Creates `~/.config/print-agent/env` from `.env.example` (if it doesn't exist)
4. Installs the systemd user service
5. Enables lingering (service starts at boot without login)
6. Enables the service

## Manual install

### 1. Build and install the binary

```bash
go build -o print-agent ./cmd/print-agent
mkdir -p ~/.local/bin
install -m 0755 print-agent ~/.local/bin/print-agent
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
