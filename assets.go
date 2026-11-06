// Package printagent embeds the files the single release binary needs at
// install time and at run time: the systemd units, the config template and
// the Python label scripts.
package printagent

import _ "embed"

// EnvTemplate is the config file written by `printer setup` when none exists.
//
//go:embed .env.example
var EnvTemplate []byte

// ServiceUnit is the systemd user unit of the agent (print-agent.service).
//
//go:embed deploy/print-agent.service
var ServiceUnit []byte

// UpdateServiceUnit runs `printer update` once (printer-update.service).
//
//go:embed deploy/printer-update.service
var UpdateServiceUnit []byte

// UpdateTimerUnit schedules UpdateServiceUnit daily (printer-update.timer).
//
//go:embed deploy/printer-update.timer
var UpdateTimerUnit []byte

// LabelScript is scripts/print.py (price labels and address stickers).
//
//go:embed scripts/print.py
var LabelScript []byte

// StickerScript is scripts/print_sticker.py (image stickers).
//
//go:embed scripts/print_sticker.py
var StickerScript []byte
