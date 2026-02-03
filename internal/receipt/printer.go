package receipt

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Printer struct {
	Device   string
	Delay    time.Duration
	LogoPath string
}

type PrinterDevice struct {
	Path        string
	IsAvailable bool
}

type Receipt struct {
	StoreAddress1 string
	StoreAddress2 string
	StorePhone    string
	StoreVAT      string
	StoreSocial   string
	StoreWebsite  string
	Items         []ReceiptItem
	Payments      []string
	Barcode       string
	CreatedAt     time.Time
}

type ReceiptItem struct {
	Name      string
	Quantity  int
	UnitPrice string
}

// PutAside represents a "mise de cote" (put aside) ticket for reserved products.
type PutAside struct {
	CustomerName   string
	CustomerPhone  string // optional
	ProductName    string
	ProductBarcode string // optional
	Quantity       int
	OrderBarcode   string // e.g. CMD-2024-001234
	OrderDate      string // e.g. 15/01/2024
}

func NewPrinter(device string) *Printer {
	if device == "" {
		if envDevice := os.Getenv("RECEIPT_PRINTER_DEVICE"); envDevice != "" {
			device = envDevice
		} else if detected, err := AutoDetectPrinter(); err == nil {
			device = detected.Path
		} else {
			device = defaultDevice()
		}
	}

	printer := &Printer{Device: device, Delay: 50 * time.Millisecond}
	if logoPath := os.Getenv("RECEIPT_LOGO_PATH"); logoPath != "" {
		printer.LogoPath = logoPath
	}
	return printer
}

func defaultDevice() string {
	switch runtime.GOOS {
	case "windows":
		return "\\\\.\\COM1"
	case "darwin":
		return "/dev/tty.usbserial"
	default:
		return "/dev/usb/epson_tmt20iii"
	}
}

func DetectPrinters() ([]PrinterDevice, error) {
	candidates := []string{
		"/dev/usb/epson_tmt20iii",
		"/dev/usb/brother_ql800",
	}
	candidates = append(candidates, globDevices("/dev/usb/lp*")...)
	candidates = append(candidates, globDevices("/dev/lp*")...)
	candidates = append(candidates, globDevices("/dev/ttyUSB*")...)
	candidates = append(candidates, globDevices("/dev/ttyACM*")...)

	seen := map[string]bool{}
	var devices []PrinterDevice
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		devices = append(devices, PrinterDevice{
			Path:        path,
			IsAvailable: isDeviceWritable(path),
		})
	}
	return devices, nil
}

func AutoDetectPrinter() (*PrinterDevice, error) {
	devices, _ := DetectPrinters()
	for _, dev := range devices {
		if dev.IsAvailable {
			return &dev, nil
		}
	}
	return nil, fmt.Errorf("aucune imprimante disponible detectee")
}

func (p *Printer) OpenDrawer() error {
	f, err := os.OpenFile(p.Device, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("impossible d'ouvrir %s: %w", p.Device, err)
	}
	defer f.Close()

	cmd := []byte{0x1b, 0x70, 0x00, 0x19, 0xfa}
	if _, err := f.Write(cmd); err != nil {
		return fmt.Errorf("impossible d'ecrire sur %s: %w", p.Device, err)
	}
	return nil
}

func (p *Printer) PrintReceipt(r Receipt) error {
	f, err := os.OpenFile(p.Device, os.O_CREATE|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Sync()
		_ = f.Close()
	}()

	sendLine := func(data string) error {
		data = removeAccents(data)
		if _, err := f.Write([]byte(data)); err != nil {
			return err
		}
		_ = f.Sync()
		time.Sleep(p.Delay + 10*time.Millisecond)
		return nil
	}
	sendRaw := func(data []byte) error {
		if _, err := f.Write(data); err != nil {
			return err
		}
		_ = f.Sync()
		time.Sleep(p.Delay + 10*time.Millisecond)
		return nil
	}
	nl := "\r\n"

	if err := sendRaw([]byte{0x1b, '@'}); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	if p.LogoPath != "" {
		_ = printImageGraphicsMode(f, p.LogoPath)
		_ = sendRaw([]byte{0x0A})
	}

	if err := sendRaw([]byte{0x1b, 't', 0x13}); err != nil {
		return err
	}
	if err := sendRaw([]byte{0x1b, 'a', 1}); err != nil {
		return err
	}

	storeAddress1 := getEnvOrDefault("STORE_ADDRESS_LINE1", r.StoreAddress1)
	storeAddress2 := getEnvOrDefault("STORE_ADDRESS_LINE2", r.StoreAddress2)
	storePhone := getEnvOrDefault("STORE_PHONE", r.StorePhone)
	storeVAT := getEnvOrDefault("STORE_VAT_NUMBER", r.StoreVAT)

	if storeAddress1 != "" {
		if err := sendLine(storeAddress1+nl); err != nil {
			return err
		}
	}
	if storeAddress2 != "" {
		if err := sendLine(storeAddress2+nl); err != nil {
			return err
		}
	}
	if storePhone != "" {
		if err := sendLine("Tel: "+storePhone+nl); err != nil {
			return err
		}
	}
	if storeVAT != "" {
		if err := sendLine("TVA: "+storeVAT+nl); err != nil {
			return err
		}
	}
	if err := sendLine(nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'a', 0}); err != nil {
		return err
	}

	date := r.CreatedAt.Format("02/01/2006 15:04")
	if err := sendLine(fmt.Sprintf("Date: %s%s", date, nl)); err != nil {
		return err
	}
	if err := sendLine(fmt.Sprintf("Ticket: %s%s%s", r.Barcode, nl, nl)); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
		return err
	}

	col2 := 23
	col3 := 35
	header := "ARTICLE"
	header += spaces(col2-len(header)) + "QTE"
	header += spaces(col3-(col2+len("QTE"))) + "PRIX"

	if err := sendLine(header+nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
		return err
	}
	if err := sendLine(strings.Repeat("-", 48)+nl); err != nil {
		return err
	}

	for _, it := range r.Items {
		name := it.Name
		if len(name) > 22 {
			name = name[:22]
		}
		line := name
		line += spaces(col2 - len(line))
		line += fmt.Sprintf("%3d", it.Quantity)
		line += spaces(col3 - len(line))
		line += fmt.Sprintf("%6s", it.UnitPrice)
		if err := sendLine(line+nl); err != nil {
			return err
		}
	}

	if err := sendLine(strings.Repeat("-", 48)+nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'a', 2}); err != nil {
		return err
	}
	if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
		return err
	}
	if err := sendLine(fmt.Sprintf("TOTAL:      %s EUR%s", computeTotal(r.Items), nl)); err != nil {
		return err
	}
	if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
		return err
	}
	if err := sendLine(nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'a', 1}); err != nil {
		return err
	}
	if len(r.Payments) > 0 {
		payLine := "PAIEMENT: " + strings.Join(r.Payments, ", ")
		if err := sendLine(payLine+nl+nl); err != nil {
			return err
		}
	}
	if err := sendLine("Merci de votre visite!"+nl); err != nil {
		return err
	}

	storeSocial := getEnvOrDefault("STORE_SOCIAL_HANDLE", r.StoreSocial)
	storeWebsite := getEnvOrDefault("STORE_WEBSITE", r.StoreWebsite)
	if storeSocial != "" {
		if err := sendLine(storeSocial+nl); err != nil {
			return err
		}
	}
	if storeWebsite != "" {
		if err := sendLine(storeWebsite+nl+nl); err != nil {
			return err
		}
	}

	// Print barcode if present
	if r.Barcode != "" {
		time.Sleep(100 * time.Millisecond) // Extra delay before barcode
		if err := sendRaw([]byte{0x1d, 'h', 100}); err != nil { // height
			return err
		}
		if err := sendRaw([]byte{0x1d, 'w', 3}); err != nil { // width
			return err
		}
		if err := sendRaw([]byte{0x1d, 'H', 2}); err != nil { // HRI below
			return err
		}
		if err := sendRaw([]byte{0x1d, 'k', 4}); err != nil { // CODE128 type A
			return err
		}
		if err := sendRaw([]byte(r.Barcode)); err != nil {
			return err
		}
		if err := sendRaw([]byte{0x00}); err != nil { // null terminator
			return err
		}
		time.Sleep(200 * time.Millisecond) // Wait for barcode to print
	}
	if err := sendLine(nl+nl+nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1d, 'V', 1}); err != nil {
		_ = sendRaw([]byte{0x1d, 'V', 0x41})
	}

	return nil
}

// PrintPutAsideTicket prints a "mise de cote" ticket for reserved products.
func (p *Printer) PrintPutAsideTicket(pa PutAside) error {
	f, err := os.OpenFile(p.Device, os.O_CREATE|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Sync()
		_ = f.Close()
	}()

	sendLine := func(data string) error {
		data = removeAccents(data)
		if _, err := f.Write([]byte(data)); err != nil {
			return err
		}
		_ = f.Sync()
		time.Sleep(p.Delay + 10*time.Millisecond)
		return nil
	}
	sendRaw := func(data []byte) error {
		if _, err := f.Write(data); err != nil {
			return err
		}
		_ = f.Sync()
		time.Sleep(p.Delay + 10*time.Millisecond)
		return nil
	}
	nl := "\r\n"
	separator := strings.Repeat("=", 32)
	thinSeparator := strings.Repeat("-", 32)

	// Initialize printer
	if err := sendRaw([]byte{0x1b, '@'}); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	// Set character table
	if err := sendRaw([]byte{0x1b, 't', 0x13}); err != nil {
		return err
	}

	// === HEADER ===
	// Center align
	if err := sendRaw([]byte{0x1b, 'a', 1}); err != nil {
		return err
	}
	// Bold on
	if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
		return err
	}
	if err := sendLine(separator + nl); err != nil {
		return err
	}
	if err := sendLine("** MISE DE COTE **" + nl); err != nil {
		return err
	}
	if err := sendLine(separator + nl + nl); err != nil {
		return err
	}
	// Bold off
	if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
		return err
	}

	// === CUSTOMER INFO ===
	// Left align
	if err := sendRaw([]byte{0x1b, 'a', 0}); err != nil {
		return err
	}
	// Bold on for customer name
	if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
		return err
	}
	if err := sendLine("Client: " + pa.CustomerName + nl); err != nil {
		return err
	}
	// Bold off
	if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
		return err
	}
	// Phone (optional)
	if pa.CustomerPhone != "" {
		if err := sendLine("Tel: " + pa.CustomerPhone + nl); err != nil {
			return err
		}
	}
	if err := sendLine(nl); err != nil {
		return err
	}

	// === PRODUCT INFO ===
	if err := sendLine(thinSeparator + nl); err != nil {
		return err
	}
	if err := sendLine("Produit:" + nl); err != nil {
		return err
	}
	// Word wrap product name at ~30 chars
	productName := pa.ProductName
	for len(productName) > 0 {
		lineLen := 30
		if len(productName) <= lineLen {
			if err := sendLine(productName + nl); err != nil {
				return err
			}
			break
		}
		// Find last space before lineLen
		cutPoint := lineLen
		for i := lineLen; i > 0; i-- {
			if productName[i] == ' ' {
				cutPoint = i
				break
			}
		}
		if err := sendLine(productName[:cutPoint] + nl); err != nil {
			return err
		}
		productName = strings.TrimLeft(productName[cutPoint:], " ")
	}
	if err := sendLine(nl); err != nil {
		return err
	}
	if err := sendLine(fmt.Sprintf("Quantite: %d", pa.Quantity) + nl); err != nil {
		return err
	}
	if err := sendLine(thinSeparator + nl + nl); err != nil {
		return err
	}

	// === ORDER INFO ===
	if err := sendLine("Commande du: " + pa.OrderDate + nl); err != nil {
		return err
	}
	if err := sendLine("Ref: " + pa.OrderBarcode + nl + nl); err != nil {
		return err
	}

	// === PRODUCT BARCODE (if provided) ===
	if pa.ProductBarcode != "" {
		// Center align for barcode
		if err := sendRaw([]byte{0x1b, 'a', 1}); err != nil {
			return err
		}
		// Set barcode height
		if err := sendRaw([]byte{0x1d, 'h', 50}); err != nil {
			return err
		}
		// Set barcode width
		if err := sendRaw([]byte{0x1d, 'w', 2}); err != nil {
			return err
		}
		// Print HRI below barcode
		if err := sendRaw([]byte{0x1d, 'H', 2}); err != nil {
			return err
		}
		// Print CODE128 barcode
		if err := sendRaw([]byte{0x1d, 'k', 4}); err != nil {
			return err
		}
		if err := sendRaw([]byte(pa.ProductBarcode)); err != nil {
			return err
		}
		if err := sendRaw([]byte{0x00}); err != nil {
			return err
		}
		if err := sendLine(nl + nl); err != nil {
			return err
		}
	}

	// === FOOTER ===
	// Center align
	if err := sendRaw([]byte{0x1b, 'a', 1}); err != nil {
		return err
	}
	// Bold on
	if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
		return err
	}
	if err := sendLine(separator + nl); err != nil {
		return err
	}
	if err := sendLine("RESERVE - NE PAS VENDRE" + nl); err != nil {
		return err
	}
	if err := sendLine(separator + nl); err != nil {
		return err
	}
	// Bold off
	if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
		return err
	}
	if err := sendLine(nl + nl + nl); err != nil {
		return err
	}

	// Cut paper
	if err := sendRaw([]byte{0x1d, 'V', 1}); err != nil {
		_ = sendRaw([]byte{0x1d, 'V', 0x41})
	}

	return nil
}

func computeTotal(items []ReceiptItem) string {
	var totalCents int
	for _, it := range items {
		var euros int
		var cents int
		_, _ = fmt.Sscanf(it.UnitPrice, "%d.%d", &euros, &cents)
		totalCents += (euros*100 + cents) * it.Quantity
	}
	return fmt.Sprintf("%0.2f", float64(totalCents)/100.0)
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func globDevices(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func isDeviceWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%*s", n, "")
}

func removeAccents(s string) string {
	replacer := strings.NewReplacer(
		"à", "a", "â", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i",
		"ô", "o", "ö", "o",
		"ù", "u", "û", "u", "ü", "u",
		"ç", "c",
		"À", "A", "Â", "A", "Ä", "A",
		"É", "E", "È", "E", "Ê", "E", "Ë", "E",
		"Î", "I", "Ï", "I",
		"Ô", "O", "Ö", "O",
		"Ù", "U", "Û", "U", "Ü", "U",
		"Ç", "C",
	)
	return replacer.Replace(s)
}

func printImageGraphicsMode(f *os.File, path string) error {
	imgFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer imgFile.Close()

	img, _, err := image.Decode(imgFile)
	if err != nil {
		return err
	}

	bounds := img.Bounds()
	originalWidth := bounds.Dx()
	originalHeight := bounds.Dy()

	maxWidth := 512
	maxHeight := 512
	width := originalWidth
	height := originalHeight

	if width > maxWidth {
		height = (height * maxWidth) / width
		width = maxWidth
	}
	if height > maxHeight {
		width = (width * maxHeight) / height
		height = maxHeight
	}

	width = (width / 8) * 8
	if width == 0 {
		width = 8
	}

	f.Write([]byte{0x1b, 'a', 1})
	widthBytes := width / 8
	imageData := make([]byte, widthBytes*height)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			origX := (x * originalWidth) / width
			origY := (y * originalHeight) / height

			if origX < originalWidth && origY < originalHeight {
				r, g, b, a := img.At(origX, origY).RGBA()
				if a >= 0x8000 {
					luminance := (r*2126 + g*7152 + b*722) / 10000
					if luminance < 0x8000 {
						byteIndex := y*widthBytes + x/8
						bitIndex := 7 - (x % 8)
						if byteIndex < len(imageData) {
							imageData[byteIndex] |= 1 << uint(bitIndex)
						}
					}
				}
			}
		}
	}

	cmd := []byte{
		0x1d, 'v', '0',
		0x00,
		byte(widthBytes & 0xFF),
		byte((widthBytes >> 8) & 0xFF),
		byte(height & 0xFF),
		byte((height >> 8) & 0xFF),
	}

	_, _ = f.Write(cmd)
	_, _ = f.Write(imageData)
	time.Sleep(300 * time.Millisecond)
	_, _ = f.Write([]byte{0x1b, 'a', 0})
	return nil
}

func DumpReceiptPreview(r Receipt) string {
	var b bytes.Buffer
	b.WriteString("=== RECEIPT PREVIEW ===\n")
	b.WriteString(r.StoreAddress1 + "\n")
	b.WriteString(r.StoreAddress2 + "\n")
	b.WriteString("Tel: " + r.StorePhone + "\n")
	b.WriteString("TVA: " + r.StoreVAT + "\n")
	b.WriteString("Ticket: " + r.Barcode + "\n")
	return b.String()
}
