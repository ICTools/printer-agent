package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"print-agent/internal/agent"
	"print-agent/internal/api"
	"print-agent/internal/label"
	"print-agent/internal/receipt"
	"print-agent/internal/registry"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "detect":
		cmdDetect()
	case "receipt-test":
		cmdReceiptTest(os.Args[2:])
	case "open-drawer":
		cmdOpenDrawer(os.Args[2:])
	case "label":
		cmdLabel(os.Args[2:])
	case "sticker-address":
		cmdStickerAddress(os.Args[2:])
	case "sticker-image":
		cmdStickerImage(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: print-agent <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run               Start the agent (poll API for print jobs)")
	fmt.Println("  detect            List detected printer devices")
	fmt.Println("  receipt-test      Print a sample receipt")
	fmt.Println("  open-drawer       Open cash drawer")
	fmt.Println("  label             Print a price label")
	fmt.Println("  sticker-address   Print an address sticker")
	fmt.Println("  sticker-image     Print a sticker image")
	fmt.Println()
	fmt.Println("Run 'print-agent run --help' for agent options.")
}

func cmdDetect() {
	devices, err := receipt.DetectPrinters()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Detect error: %v\n", err)
		os.Exit(1)
	}
	if len(devices) == 0 {
		fmt.Println("No devices found")
		return
	}
	for _, d := range devices {
		status := "no"
		if d.IsAvailable {
			status = "yes"
		}
		fmt.Printf("%s (writable: %s)\n", d.Path, status)
	}
}

func cmdReceiptTest(args []string) {
	fs := flag.NewFlagSet("receipt-test", flag.ExitOnError)
	device := fs.String("device", "", "Printer device path")
	logo := fs.String("logo", "", "Logo image path")
	barcode := fs.String("barcode", "TEST123", "Barcode text")
	delay := fs.Int("delay-ms", 1, "Delay in ms between writes")
	_ = fs.Parse(args)

	printer := receipt.NewPrinter(*device)
	if *logo != "" {
		printer.LogoPath = *logo
	}
	printer.Delay = time.Duration(*delay) * time.Millisecond

	r := receipt.Receipt{
		StoreAddress1: "21 Avenue des Combattants",
		StoreAddress2: "1370 Jodoigne",
		StorePhone:    "0471 70 69 01",
		StoreVAT:      "BE 1025.024.536",
		StoreSocial:   "@chapitreneuf",
		StoreWebsite:  "https://chapitreneuf.be",
		Barcode:       *barcode,
		CreatedAt:     time.Now(),
		Items: []receipt.ReceiptItem{
			{Name: "Livre neuf", Quantity: 1, UnitPrice: "12.50"},
			{Name: "Carnet", Quantity: 2, UnitPrice: "4.90"},
		},
		Payments: []string{"CARTE"},
	}

	if err := printer.PrintReceipt(r); err != nil {
		fmt.Fprintf(os.Stderr, "Print error: %v\n", err)
		os.Exit(1)
	}
}

func cmdOpenDrawer(args []string) {
	fs := flag.NewFlagSet("open-drawer", flag.ExitOnError)
	device := fs.String("device", "", "Printer device path")
	_ = fs.Parse(args)

	printer := receipt.NewPrinter(*device)
	if err := printer.OpenDrawer(); err != nil {
		fmt.Fprintf(os.Stderr, "Drawer error: %v\n", err)
		os.Exit(1)
	}
}

func cmdLabel(args []string) {
	fs := flag.NewFlagSet("label", flag.ExitOnError)
	python := fs.String("python", "", "Python executable")
	script := fs.String("script", "", "Path to print.py")
	footer := fs.String("footer", "", "Footer text")
	_ = fs.Parse(args)

	posArgs := fs.Args()
	if len(posArgs) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: print-agent label [options] <name> <price> <barcode>")
		os.Exit(1)
	}

	opts := label.LabelOptions{
		PythonPath: *python,
		ScriptPath: *script,
		Name:       posArgs[0],
		PriceText:  posArgs[1],
		Barcode:    posArgs[2],
		Footer:     *footer,
	}

	if err := label.PrintLabel(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Label error: %v\n", err)
		os.Exit(1)
	}
}

func cmdStickerAddress(args []string) {
	fs := flag.NewFlagSet("sticker-address", flag.ExitOnError)
	python := fs.String("python", "", "Python executable")
	script := fs.String("script", "", "Path to print.py")
	_ = fs.Parse(args)

	posArgs := fs.Args()
	if len(posArgs) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: print-agent sticker-address [options] <line1> <line2> [line3]")
		os.Exit(1)
	}

	line1 := posArgs[0]
	line2 := posArgs[1]
	line3 := ""
	if len(posArgs) > 2 {
		line3 = strings.Join(posArgs[2:], " ")
	}

	opts := label.LabelOptions{
		PythonPath: *python,
		ScriptPath: *script,
		Name:       line1,
		PriceText:  line2,
		Barcode:    "",
		Footer:     line3,
	}

	if err := label.PrintLabel(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Sticker address error: %v\n", err)
		os.Exit(1)
	}
}

func cmdStickerImage(args []string) {
	fs := flag.NewFlagSet("sticker-image", flag.ExitOnError)
	python := fs.String("python", "", "Python executable")
	script := fs.String("script", "", "Path to print_sticker.py")
	device := fs.String("device", "", "Sticker printer device")
	_ = fs.Parse(args)

	posArgs := fs.Args()
	if len(posArgs) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: print-agent sticker-image [options] <image_path>")
		os.Exit(1)
	}

	opts := label.StickerImageOptions{
		PythonPath: *python,
		ScriptPath: *script,
		ImagePath:  posArgs[0],
		DevicePath: *device,
	}

	if err := label.PrintStickerImage(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Sticker image error: %v\n", err)
		os.Exit(1)
	}
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: print-agent run [options]")
		fmt.Println()
		fmt.Println("Start the print agent to poll for jobs from a remote API.")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -api-url string")
		fmt.Println("        API base URL (required)")
		fmt.Println("  -api-key string")
		fmt.Println("        API key for JWT authentication (required)")
		fmt.Println("  -api-secret string")
		fmt.Println("        API secret for JWT authentication (required)")
		fmt.Println("  -poll-interval duration")
		fmt.Println("        Poll interval (default 2s)")
		fmt.Println("  -ping-interval duration")
		fmt.Println("        Ping/heartbeat interval (default 30s)")
		fmt.Println("  -sync-interval duration")
		fmt.Println("        Printer sync check interval (default 10s)")
		fmt.Println("  -timeout duration")
		fmt.Println("        HTTP request timeout (default 30s)")
		fmt.Println("  -health-addr string")
		fmt.Println("        Health check server address (e.g., :8080)")
		fmt.Println("  -verbose")
		fmt.Println("        Enable verbose logging")
		fmt.Println("  -dry-run")
		fmt.Println("        Log jobs without executing (for testing communication)")
		fmt.Println("  -insecure")
		fmt.Println("        Skip TLS certificate verification (for local testing)")
		fmt.Println()
		fmt.Println("Environment variables:")
		fmt.Println("  PRINT_AGENT_API_URL       API base URL (same as -api-url)")
		fmt.Println("  PRINT_AGENT_API_KEY       API key (same as -api-key)")
		fmt.Println("  PRINT_AGENT_API_SECRET    API secret (same as -api-secret)")
		fmt.Println("  PRINT_AGENT_POLL_INTERVAL Poll interval (same as -poll-interval)")
		fmt.Println("  PRINT_AGENT_PING_INTERVAL Ping interval (same as -ping-interval)")
		fmt.Println("  PRINT_AGENT_SYNC_INTERVAL Sync interval (same as -sync-interval)")
		fmt.Println("  PRINT_AGENT_HEALTH_ADDR   Health server address (same as -health-addr)")
	}

	apiURL := fs.String("api-url", getEnvOrDefault("PRINT_AGENT_API_URL", ""), "API base URL (required)")
	apiKey := fs.String("api-key", getEnvOrDefault("PRINT_AGENT_API_KEY", ""), "API key for authentication")
	apiSecret := fs.String("api-secret", getEnvOrDefault("PRINT_AGENT_API_SECRET", ""), "API secret for authentication")
	pollInterval := fs.Duration("poll-interval", getEnvDurationOrDefault("PRINT_AGENT_POLL_INTERVAL", 2*time.Second), "Poll interval")
	pingInterval := fs.Duration("ping-interval", getEnvDurationOrDefault("PRINT_AGENT_PING_INTERVAL", 30*time.Second), "Ping interval")
	syncInterval := fs.Duration("sync-interval", getEnvDurationOrDefault("PRINT_AGENT_SYNC_INTERVAL", 10*time.Second), "Sync interval")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP request timeout")
	healthAddr := fs.String("health-addr", getEnvOrDefault("PRINT_AGENT_HEALTH_ADDR", ""), "Health check server address")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	dryRun := fs.Bool("dry-run", false, "Log jobs without executing (for testing)")
	insecure := fs.Bool("insecure", false, "Skip TLS certificate verification")

	_ = fs.Parse(args)

	// Validate required flags
	if *apiURL == "" {
		fmt.Fprintln(os.Stderr, "Error: -api-url is required (or set PRINT_AGENT_API_URL)")
		fmt.Fprintln(os.Stderr)
		fs.Usage()
		os.Exit(1)
	}
	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: -api-key is required (or set PRINT_AGENT_API_KEY)")
		fmt.Fprintln(os.Stderr)
		fs.Usage()
		os.Exit(1)
	}
	if *apiSecret == "" {
		fmt.Fprintln(os.Stderr, "Error: -api-secret is required (or set PRINT_AGENT_API_SECRET)")
		fmt.Fprintln(os.Stderr)
		fs.Usage()
		os.Exit(1)
	}

	// Create authenticator
	auth := api.NewAuthenticator(api.AuthConfig{
		BaseURL:   *apiURL,
		APIKey:    *apiKey,
		APISecret: *apiSecret,
		Insecure:  *insecure,
	})

	// Create API client with authenticator
	client := api.NewClientWithAuth(api.ClientConfig{
		BaseURL:  *apiURL,
		Timeout:  *timeout,
		Insecure: *insecure,
	}, auth)

	// Create registry
	reg := registry.NewRegistry()

	// Create agent config
	config := agent.DefaultConfig()
	config.PollInterval = *pollInterval
	config.PingInterval = *pingInterval
	config.SyncInterval = *syncInterval
	config.Verbose = *verbose
	config.DryRun = *dryRun

	// Create and start agent with auth
	a := agent.NewWithAuth(client, auth, reg, config)

	fmt.Println("Starting print-agent...")
	fmt.Printf("  API URL: %s\n", *apiURL)
	fmt.Printf("  Poll interval: %s\n", *pollInterval)
	fmt.Printf("  Ping interval: %s\n", *pingInterval)
	fmt.Printf("  Sync interval: %s\n", *syncInterval)
	if *healthAddr != "" {
		fmt.Printf("  Health server: %s\n", *healthAddr)
	}
	fmt.Printf("  Verbose: %v\n", *verbose)
	if *insecure {
		fmt.Println("  INSECURE MODE: TLS certificate verification disabled")
	}
	if *dryRun {
		fmt.Println("  DRY-RUN MODE: Jobs will be logged but NOT executed")
	}
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	// Use signal-aware context
	ctx, cancel := agent.SignalContext()
	defer cancel()

	// Start health server if configured
	var healthServer *agent.HealthServer
	if *healthAddr != "" {
		healthServer = agent.NewHealthServer(a, *healthAddr)
		if err := healthServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start health server: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			healthServer.Stop(shutdownCtx)
		}()
	}

	if err := a.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Agent error: %v\n", err)
		os.Exit(1)
	}
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDurationOrDefault(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
