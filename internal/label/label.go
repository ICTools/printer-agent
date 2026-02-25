package label

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
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
	return scriptName
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
