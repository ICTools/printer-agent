package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"print-agent/internal/label"
	"print-agent/internal/receipt"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
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
	fmt.Println("  detect            List detected printer devices")
	fmt.Println("  receipt-test      Print a sample receipt")
	fmt.Println("  open-drawer       Open cash drawer")
	fmt.Println("  label             Print a price label")
	fmt.Println("  sticker-address   Print an address sticker")
	fmt.Println("  sticker-image     Print a sticker image")
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
