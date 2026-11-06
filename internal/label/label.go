package label

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	printagent "print-agent"
)

type LabelOptions struct {
	PythonPath string
	ScriptPath string
	Name       string
	PriceText  string
	Barcode    string
	Footer     string
}

type StickerImageOptions struct {
	PythonPath string
	ScriptPath string
	ImagePath  string
	DevicePath string
}

func PrintLabel(ctx context.Context, opts LabelOptions) error {
	pythonPath := opts.PythonPath
	if pythonPath == "" {
		pythonPath = getEnvOrDefault("PYTHON_PATH", "python3")
	}

	scriptPath := opts.ScriptPath
	if scriptPath == "" {
		scriptPath = defaultScriptPath("print.py")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		cmdCtx,
		pythonPath,
		scriptPath,
		opts.Name,
		opts.PriceText,
		opts.Barcode,
		opts.Footer,
	)
	cmd.Dir = filepath.Dir(scriptPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("label print failed: %w - %s", err, string(output))
	}
	return nil
}

func PrintStickerImage(ctx context.Context, opts StickerImageOptions) error {
	pythonPath := opts.PythonPath
	if pythonPath == "" {
		pythonPath = getEnvOrDefault("PYTHON_PATH", "python3")
	}

	scriptPath := opts.ScriptPath
	if scriptPath == "" {
		scriptPath = defaultScriptPath("print_sticker.py")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	args := []string{scriptPath, opts.ImagePath}
	if opts.DevicePath != "" {
		args = append(args, opts.DevicePath)
	}

	cmd := exec.CommandContext(cmdCtx, pythonPath, args...)
	cmd.Dir = filepath.Dir(scriptPath)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sticker print failed: %w - %s", err, string(output))
	}
	return nil
}

func defaultScriptPath(scriptName string) string {
	if env := os.Getenv("LABEL_SCRIPT_PATH"); env != "" && scriptName == "print.py" {
		return env
	}
	if env := os.Getenv("STICKER_SCRIPT_PATH"); env != "" && scriptName == "print_sticker.py" {
		return env
	}

	execPath, err := os.Executable()
	if err != nil {
		return scriptName
	}
	base := filepath.Dir(execPath)
	candidate := filepath.Join(base, "scripts", scriptName)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	if extracted, err := extractEmbeddedScript(scriptName); err == nil {
		return extracted
	}
	return scriptName
}

// extractEmbeddedScript writes the copy of the script built into the binary to
// the user cache (one directory per content hash, so an updated binary never
// runs a stale script) and returns its path. It lets a release binary print
// labels without a checkout of the repository.
func extractEmbeddedScript(scriptName string) (string, error) {
	var content []byte
	switch scriptName {
	case "print.py":
		content = printagent.LabelScript
	case "print_sticker.py":
		content = printagent.StickerScript
	default:
		return "", fmt.Errorf("no embedded script %q", scriptName)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	dir := filepath.Join(cacheDir, "print-agent", "scripts", hex.EncodeToString(sum[:])[:12])
	return writeScriptOnce(dir, scriptName, content)
}

func writeScriptOnce(dir, scriptName string, content []byte) (string, error) {
	target := filepath.Join(dir, scriptName)
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
		return target, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "."+scriptName+"-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", err
	}
	return target, nil
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
