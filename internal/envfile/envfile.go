// Package envfile reads, checks and displays the agent config file, a systemd
// EnvironmentFile (~/.config/print-agent/env).
package envfile

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Entry is one KEY=VALUE assignment.
type Entry struct {
	Line  int
	Key   string
	Value string
}

// Problem is an error or a warning found in the file.
type Problem struct {
	Line    int // 0 when not tied to a line
	Message string
	Fatal   bool
}

func (p Problem) String() string {
	level := "warning"
	if p.Fatal {
		level = "error"
	}
	if p.Line > 0 {
		return fmt.Sprintf("%s: line %d: %s", level, p.Line, p.Message)
	}
	return fmt.Sprintf("%s: %s", level, p.Message)
}

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Parse reads the file the way systemd does for the common cases: blank
// lines and lines starting with # or ; are ignored, values may be wrapped in
// single or double quotes. Syntax errors are returned as fatal problems.
func Parse(data []byte) ([]Entry, []Problem) {
	var entries []Entry
	var problems []Problem
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			problems = append(problems, Problem{n, "remove the 'export' prefix (systemd EnvironmentFile format)", true})
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !keyRe.MatchString(key) {
			// Never echo the line: it may hold a secret (a pasted token, a key with a typo).
			problems = append(problems, Problem{n, "expected KEY=VALUE (line not shown, it may contain a secret)", true})
			continue
		}
		v, err := unquote(strings.TrimSpace(value))
		if err != nil {
			problems = append(problems, Problem{n, fmt.Sprintf("%s: %v", key, err), true})
			continue
		}
		entries = append(entries, Entry{Line: n, Key: key, Value: v})
	}
	return entries, problems
}

func unquote(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	q := v[0]
	if q != '"' && q != '\'' {
		return v, nil
	}
	if len(v) < 2 || v[len(v)-1] != q {
		return "", fmt.Errorf("unterminated quote")
	}
	inner := v[1 : len(v)-1]
	if q == '"' {
		inner = strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(inner)
	}
	return inner, nil
}

// Map returns the last value of each key.
func Map(entries []Entry) map[string]string {
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Key] = e.Value
	}
	return m
}

// Known lists the variables the agent reads (README "Variables d'environnement").
var Known = map[string]bool{
	"PRINT_AGENT_API_URL": true, "PRINT_AGENT_API_KEY": true, "PRINT_AGENT_API_SECRET": true,
	"PRINT_AGENT_POLL_INTERVAL": true, "PRINT_AGENT_PING_INTERVAL": true, "PRINT_AGENT_SYNC_INTERVAL": true,
	"PRINT_AGENT_HEALTH_ADDR": true, "RECEIPT_PRINTER_DEVICE": true, "RECEIPT_LOGO_PATH": true,
	"RECEIPT_PRINTABLE_DOTS": true, "STORE_ADDRESS_LINE1": true, "STORE_ADDRESS_LINE2": true,
	"STORE_PHONE": true, "STORE_VAT_NUMBER": true, "STORE_SOCIAL_HANDLE": true, "STORE_WEBSITE": true,
	"PYTHON_PATH": true, "LABEL_SCRIPT_PATH": true, "STICKER_SCRIPT_PATH": true,
	"PRINTER_GITHUB_TOKEN": true, "GITHUB_TOKEN": true, "GH_TOKEN": true,
}

// placeholders are the values of .env.example that must be replaced.
var placeholders = map[string]bool{
	"https://your-api.example.com": true, "your-api-key": true, "your-api-secret": true,
}

// Validate parses the file and checks what the agent needs to start.
func Validate(data []byte) ([]Entry, []Problem) {
	entries, problems := Parse(data)
	m := Map(entries)
	for _, key := range []string{"PRINT_AGENT_API_URL", "PRINT_AGENT_API_KEY", "PRINT_AGENT_API_SECRET"} {
		v, ok := m[key]
		switch {
		case !ok || v == "":
			problems = append(problems, Problem{0, key + " is required", true})
		case placeholders[v]:
			problems = append(problems, Problem{lineOf(entries, key), key + " still has the example value", true})
		}
	}
	if raw := m["PRINT_AGENT_API_URL"]; raw != "" && !placeholders[raw] {
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			problems = append(problems, Problem{lineOf(entries, "PRINT_AGENT_API_URL"),
				"PRINT_AGENT_API_URL must be an http(s) URL, e.g. https://app.example.com", true})
		}
	}
	for _, key := range []string{"PRINT_AGENT_POLL_INTERVAL", "PRINT_AGENT_PING_INTERVAL", "PRINT_AGENT_SYNC_INTERVAL"} {
		if v := m[key]; v != "" {
			if d, err := time.ParseDuration(v); err != nil || d <= 0 {
				problems = append(problems, Problem{lineOf(entries, key),
					fmt.Sprintf("%s=%q is not a positive duration (e.g. 2s, 1m)", key, v), true})
			}
		}
	}
	for _, key := range []string{"LABEL_SCRIPT_PATH", "STICKER_SCRIPT_PATH", "RECEIPT_LOGO_PATH"} {
		if v := m[key]; v != "" {
			if _, err := os.Stat(v); err != nil {
				msg := key + " points to a missing file"
				if key != "RECEIPT_LOGO_PATH" {
					msg += ": remove the line to use the script built into the binary"
				}
				problems = append(problems, Problem{lineOf(entries, key), msg, false})
			}
		}
	}
	for _, e := range entries {
		if !Known[e.Key] {
			problems = append(problems, Problem{e.Line, "unknown variable " + e.Key + " (typo?)", false})
		}
	}
	return entries, problems
}

// HasFatal reports whether one of the problems prevents starting the agent.
func HasFatal(problems []Problem) bool {
	for _, p := range problems {
		if p.Fatal {
			return true
		}
	}
	return false
}

func lineOf(entries []Entry, key string) int {
	line := 0
	for _, e := range entries {
		if e.Key == key {
			line = e.Line
		}
	}
	return line
}

// IsSecret reports whether a variable holds a credential that must never be
// printed in full.
func IsSecret(key string) bool {
	k := strings.ToUpper(key)
	for _, s := range []string{"SECRET", "PASSWORD", "PASSWD", "TOKEN", "KEY"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Mask returns the value to display: secrets are hidden, API keys keep their
// first 4 characters so two configs can be told apart.
func Mask(key, value string) string {
	if strings.HasSuffix(strings.ToUpper(key), "_URL") {
		return MaskURL(value)
	}
	if !IsSecret(key) || value == "" {
		return value
	}
	k := strings.ToUpper(key)
	if strings.HasSuffix(k, "_KEY") && !strings.Contains(k, "SECRET") && len(value) > 8 {
		return value[:4] + "********"
	}
	return "********"
}

// Load reads the file at path into the process environment without
// overriding variables already set, so the CLI sees what the service sees.
func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	entries, _ := Parse(data)
	for _, e := range entries {
		if _, set := os.LookupEnv(e.Key); !set {
			if err := os.Setenv(e.Key, e.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

// MaskURL hides the password of credentials embedded in a URL (user:pass@host).
func MaskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPass := u.User.Password(); hasPass {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
		return strings.Replace(u.String(), "xxxxx", "********", 1)
	}
	return raw
}
