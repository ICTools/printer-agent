package label

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	envKey := ""
	switch scriptName {
	case "print.py":
		envKey = "LABEL_SCRIPT_PATH"
	case "print_sticker.py":
		envKey = "STICKER_SCRIPT_PATH"
	}
	if env := os.Getenv(envKey); envKey != "" && env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		// Typically a path into a repository clone deleted after the migration
		// to release binaries: fall back to the copy built into the binary.
		log.Printf("[WARN] %s=%s not found, using the script built into the binary", envKey, env)
	}

	if execPath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(execPath), "scripts", scriptName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if dir, err := extractEmbeddedScripts(); err == nil {
		return filepath.Join(dir, scriptName)
	}
	return scriptName
}

// embeddedFiles are written together in one directory: print.py looks for
// ./sticker.png in its working directory (the script directory).
func embeddedFiles() map[string][]byte {
	return map[string][]byte{
		"print.py":         printagent.LabelScript,
		"print_sticker.py": printagent.StickerScript,
		"sticker.png":      printagent.StickerImage,
	}
}

// extractEmbeddedScripts writes the files built into the binary to the user
// cache (one directory per content hash, so an updated binary never runs a
// stale script) and returns the directory. It lets a release binary print
// labels without a checkout of the repository.
func extractEmbeddedScripts() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return extractTo(filepath.Join(cacheDir, "print-agent", "scripts"), embeddedFiles())
}

func extractTo(root string, files map[string][]byte) (string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write(files[name])
	}
	dir := filepath.Join(root, hex.EncodeToString(h.Sum(nil))[:12])
	for _, name := range names {
		if _, err := writeScriptOnce(dir, name, files[name]); err != nil {
			return "", err
		}
	}
	return dir, nil
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
