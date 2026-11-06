package receipt

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
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
	StoreAddress1  string
	StoreAddress2  string
	StorePhone     string
	StoreVAT       string
	StoreSocial    string
	StoreWebsite   string
	Items          []ReceiptItem
	Payments       []string
	Barcode        string
	CreatedAt      time.Time
	Subtotal       string
	Total          string
	DiscountLabel  string
	DiscountAmount string
	// BarcodeSymbology is the payload "barcode_symbology" ("CODE128" or empty: CODE128).
	BarcodeSymbology string
	// IsGiftReceipt / IsReturnReceipt hide every price and the total (chapitreneuf #874).
	IsGiftReceipt   bool
	IsReturnReceipt bool
}

type ReceiptItem struct {
	Name              string
	Quantity          int
	UnitPrice         string
	DiscountPercent   string
	OriginalUnitPrice string
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

	printer := &Printer{Device: device, Delay: 0}
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
	f, err := os.OpenFile(p.Device, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Sync()
		_ = f.Close()
	}()

	return p.writeReceipt(f, r)
}

// sleep is replaced in tests so rendering a receipt does not wait for the printer.
var sleep = time.Sleep

// writeReceipt renders the receipt as ESC/POS bytes.
//
// Gift and return receipts (is_gift_receipt / is_return_receipt, chapitreneuf #874) carry a
// title and never show a price, a discount, a subtotal, a total or the payments. Every
// receipt ends with the sale barcode (CODE128, see writeSaleBarcode).
func (p *Printer) writeReceipt(w io.Writer, r Receipt) error {
	sendLine := func(data string) error {
		data = removeAccents(data)
		_, err := w.Write([]byte(data))
		return err
	}
	sendRaw := func(data []byte) error {
		_, err := w.Write(data)
		return err
	}
	nl := "\n"
	priceless := r.IsGiftReceipt || r.IsReturnReceipt

	if err := sendRaw([]byte{0x1b, '@'}); err != nil {
		return err
	}
	sleep(200 * time.Millisecond)

	if p.LogoPath != "" {
		_ = printImageGraphicsMode(w, p.LogoPath)
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
		if err := sendLine(storeAddress1 + nl); err != nil {
			return err
		}
	}
	if storeAddress2 != "" {
		if err := sendLine(storeAddress2 + nl); err != nil {
			return err
		}
	}
	if storePhone != "" {
		if err := sendLine("Tel: " + storePhone + nl); err != nil {
			return err
		}
	}
	if storeVAT != "" {
		if err := sendLine("TVA: " + storeVAT + nl); err != nil {
			return err
		}
	}
	if err := sendLine(nl); err != nil {
		return err
	}

	if title := r.title(); title != "" {
		if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
			return err
		}
		if err := sendLine(title + nl + nl); err != nil {
			return err
		}
		if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
			return err
		}
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
	if priceless {
		header += spaces(lineWidth-len(header)-len("QTE")) + "QTE"
	} else {
		header += spaces(col2-len(header)) + "QTE"
		header += spaces(col3-(col2+len("QTE"))) + "PRIX"
	}

	if err := sendLine(header + nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'E', 0}); err != nil {
		return err
	}
	if err := sendLine(strings.Repeat("-", lineWidth) + nl); err != nil {
		return err
	}

	for _, it := range r.Items {
		if priceless {
			if err := sendLine(pricelessItemLine(it) + nl); err != nil {
				return err
			}
			continue
		}
		name := it.Name
		if len(name) > 22 {
			name = name[:22]
		}
		line := name
		line += spaces(col2 - len(line))
		line += fmt.Sprintf("%3d", it.Quantity)
		line += spaces(col3 - len(line))
		line += fmt.Sprintf("%6s", it.UnitPrice)
		if err := sendLine(line + nl); err != nil {
			return err
		}
		// Display discount detail line if present
		if it.DiscountPercent != "" && it.DiscountPercent != "0" {
			origPrice := it.OriginalUnitPrice
			if origPrice == "" {
				origPrice = it.UnitPrice
			}
			detail := fmt.Sprintf("  %d x %s  -%s%%", it.Quantity, origPrice, it.DiscountPercent)
			detail += spaces(lineWidth - len(detail) - len(it.UnitPrice))
			detail += it.UnitPrice
			if err := sendLine(detail + nl); err != nil {
				return err
			}
		}
	}

	if err := sendLine(strings.Repeat("-", lineWidth) + nl); err != nil {
		return err
	}

	if !priceless {
		if err := p.writeTotals(sendLine, sendRaw, r); err != nil {
			return err
		}
	}
	if err := sendLine(nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1b, 'a', 1}); err != nil {
		return err
	}
	if len(r.Payments) > 0 && !priceless {
		payLine := "PAIEMENT: " + strings.Join(r.Payments, ", ")
		if err := sendLine(payLine + nl + nl); err != nil {
			return err
		}
	}
	if err := sendLine("Merci de votre visite!" + nl); err != nil {
		return err
	}

	storeSocial := getEnvOrDefault("STORE_SOCIAL_HANDLE", r.StoreSocial)
	storeWebsite := getEnvOrDefault("STORE_WEBSITE", r.StoreWebsite)
	if storeSocial != "" {
		if err := sendLine(storeSocial + nl); err != nil {
			return err
		}
	}
	if storeWebsite != "" {
		if err := sendLine(storeWebsite + nl + nl); err != nil {
			return err
		}
	}

	// Sale barcode: scanned at the till to open the sale in the return flow (#874).
	if r.Barcode != "" {
		sleep(100 * time.Millisecond) // Extra delay before barcode
		if err := writeSaleBarcode(w, r.Barcode, r.BarcodeSymbology, printableDots()); err != nil {
			return err
		}
		sleep(200 * time.Millisecond) // Wait for barcode to print
	}
	if err := sendLine(nl + nl + nl); err != nil {
		return err
	}

	if err := sendRaw([]byte{0x1d, 'V', 1}); err != nil {
		_ = sendRaw([]byte{0x1d, 'V', 0x41})
	}

	return nil
}

// lineWidth is the number of font A characters per line on 80 mm paper.
const lineWidth = 48

// title is printed in bold at the top of gift and return receipts.
func (r Receipt) title() string {
	switch {
	case r.IsGiftReceipt:
		return "TICKET CADEAU"
	case r.IsReturnReceipt:
		return "TICKET DE RETOUR"
	default:
		return ""
	}
}

// pricelessItemLine prints the item name (up to 43 characters) and the quantity, never a price.
func pricelessItemLine(it ReceiptItem) string {
	name := it.Name
	if len(name) > lineWidth-5 {
		name = name[:lineWidth-5]
	}
	qty := fmt.Sprintf("%3d", it.Quantity)
	return name + spaces(lineWidth-len(name)-len(qty)) + qty
}

// writeTotals prints the optional subtotal and discount, then the total (priced receipts only).
func (p *Printer) writeTotals(sendLine func(string) error, sendRaw func([]byte) error, r Receipt) error {
	nl := "\n"
	if err := sendRaw([]byte{0x1b, 'a', 2}); err != nil {
		return err
	}

	// If discount info is present, show subtotal + discount + total
	if r.DiscountLabel != "" && r.DiscountAmount != "" {
		subtotal := r.Subtotal
		if subtotal == "" {
			subtotal = computeTotal(r.Items)
		}
		subLine := "Sous-total"
		subLine += spaces(lineWidth - len(subLine) - len(subtotal))
		subLine += subtotal
		if err := sendLine(subLine + nl); err != nil {
			return err
		}
		discLine := r.DiscountLabel
		discLine += spaces(lineWidth - len(discLine) - len(r.DiscountAmount))
		discLine += r.DiscountAmount
		if err := sendLine(discLine + nl); err != nil {
			return err
		}
	}

	if err := sendRaw([]byte{0x1b, 'E', 1}); err != nil {
		return err
	}
	total := r.Total
	if total == "" {
		total = computeTotal(r.Items)
	}
	if err := sendLine(fmt.Sprintf("TOTAL:      %s EUR%s", total, nl)); err != nil {
		return err
	}
	return sendRaw([]byte{0x1b, 'E', 0})
}

// PrintPutAsideTicket prints a "mise de cote" ticket for reserved products.
func (p *Printer) PrintPutAsideTicket(pa PutAside) error {
	f, err := os.OpenFile(p.Device, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Sync()
		_ = f.Close()
	}()

	sendLine := func(data string) error {
		data = removeAccents(data)
		_, err := f.Write([]byte(data))
		return err
	}
	sendRaw := func(data []byte) error {
		_, err := f.Write(data)
		return err
	}
	nl := "\n"
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

func printImageGraphicsMode(f io.Writer, path string) error {
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
