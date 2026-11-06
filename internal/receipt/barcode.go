package receipt

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Sale barcode printed at the bottom of every customer receipt (chapitreneuf #874).
//
// Contract with the server (payload field "barcode_symbology"):
//   - "" (servers that predate the field) or "CODE128": CODE128 with ESC/POS function B,
//     GS k 73 n {B..., code set C for runs of digits, module width 2, HRI below.
//   - "QR" / "QRCODE": QR code (model 2) with the code printed as text below.
//   - any other value: CODE128 (safe default, the server only sends CODE128 today).
//
// If the CODE128 symbol is wider than the printable area (576 dots on an 80 mm TM-T20III,
// RECEIPT_PRINTABLE_DOTS to override, e.g. 384 on 58 mm paper) or cannot encode the data,
// the agent falls back to a QR code so the ticket always carries a scannable code.
const (
	SymbologyCode128 = "CODE128"
	SymbologyQR      = "QR"

	// DefaultPrintableDots is the printable width of an 80 mm Epson TM-T20III (72 mm at 203 dpi).
	DefaultPrintableDots = 576

	barcodeModuleWidth = 2   // GS w 2: 0.25 mm per module, reliably read by handheld scanners
	barcodeHeight      = 100 // GS h 100: 12.5 mm
	qrModuleSize       = 6
)

// code128Symbol is an ESC/POS CODE128 payload ready for GS k 73 n.
type code128Symbol struct {
	data    []byte // bytes sent after n, code set selectors included
	symbols int    // data symbol characters, code set switches included (start, check and stop excluded)
}

// widthDots returns the printed width of the symbol: start + data + check (11 modules each) + stop (13 modules).
func (s code128Symbol) widthDots(moduleWidth int) int {
	return ((s.symbols+2)*11 + 13) * moduleWidth
}

// encodeCode128 builds the GS k 73 data for value: code set B for text, code set C for
// runs of digits long enough to make the symbol narrower (4+ digits at the start or the end,
// 6+ in the middle). "POS-20261106-ABCDE12345" becomes "{BPOS-{C<20><26><11><06>{B-ABCDE12345".
func encodeCode128(value string) (code128Symbol, error) {
	var sym code128Symbol
	if value == "" {
		return sym, fmt.Errorf("code-barres vide")
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 32 || value[i] > 126 {
			return sym, fmt.Errorf("caractere non encodable en CODE128 B: %q", value[i])
		}
	}

	var set byte
	switchTo := func(s byte) {
		if set == s {
			return
		}
		if set != 0 {
			sym.symbols++ // CODE B / CODE C switch character
		}
		sym.data = append(sym.data, '{', s)
		set = s
	}

	for i := 0; i < len(value); {
		run := digitRun(value, i)
		atStart := i == 0
		reachesEnd := i+run == len(value)
		useC := run >= 6 || (run >= 4 && (atStart || reachesEnd)) || (run >= 2 && run%2 == 0 && atStart && reachesEnd)

		if !useC {
			switchTo('B')
			if value[i] == '{' {
				sym.data = append(sym.data, '{', '{')
			} else {
				sym.data = append(sym.data, value[i])
			}
			sym.symbols++
			i++
			continue
		}

		if run%2 == 1 { // odd run: first digit in code set B, the even remainder in C
			switchTo('B')
			sym.data = append(sym.data, value[i])
			sym.symbols++
			i++
			run--
		}
		switchTo('C')
		for end := i + run; i < end; i += 2 {
			sym.data = append(sym.data, (value[i]-'0')*10+(value[i+1]-'0'))
			sym.symbols++
		}
	}

	if len(sym.data) > 255 {
		return sym, fmt.Errorf("code-barres trop long (%d octets)", len(sym.data))
	}
	return sym, nil
}

func digitRun(s string, from int) int {
	n := 0
	for i := from; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n++
	}
	return n
}

// normalizeSymbology maps the payload value to the symbology the agent prints.
func normalizeSymbology(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "QR", "QRCODE", "QR_CODE":
		return SymbologyQR
	default:
		return SymbologyCode128
	}
}

func printableDots() int {
	if v, err := strconv.Atoi(os.Getenv("RECEIPT_PRINTABLE_DOTS")); err == nil && v > 0 {
		return v
	}
	return DefaultPrintableDots
}

// writeSaleBarcode writes the ESC/POS commands that print value as a scannable barcode
// (centered alignment is expected to be set by the caller).
func writeSaleBarcode(w io.Writer, value, symbology string, maxDots int) error {
	if normalizeSymbology(symbology) == SymbologyCode128 {
		sym, err := encodeCode128(value)
		if err == nil && sym.widthDots(barcodeModuleWidth) <= maxDots {
			return writeCode128(w, sym)
		}
	}
	return writeQRCode(w, value)
}

func writeCode128(w io.Writer, sym code128Symbol) error {
	cmd := []byte{
		0x1d, 'h', barcodeHeight, // GS h: height
		0x1d, 'w', barcodeModuleWidth, // GS w: module width
		0x1d, 'H', 2, // GS H 2: HRI below
		0x1d, 'f', 0, // GS f 0: HRI font A
		0x1d, 'k', 73, byte(len(sym.data)), // GS k 73 n: CODE128 (function B)
	}
	cmd = append(cmd, sym.data...)
	cmd = append(cmd, '\n')
	_, err := w.Write(cmd)
	return err
}

func writeQRCode(w io.Writer, value string) error {
	data := []byte(value)
	store := len(data) + 3
	cmd := []byte{
		0x1d, '(', 'k', 4, 0, 49, 65, 50, 0, // model 2
		0x1d, '(', 'k', 3, 0, 49, 67, qrModuleSize, // module size
		0x1d, '(', 'k', 3, 0, 49, 69, 49, // error correction M
		0x1d, '(', 'k', byte(store & 0xff), byte(store >> 8), 49, 80, 48, // store data
	}
	cmd = append(cmd, data...)
	cmd = append(cmd, 0x1d, '(', 'k', 3, 0, 49, 81, 48) // print
	cmd = append(cmd, '\n')
	cmd = append(cmd, []byte(removeAccents(value)+"\n")...) // human readable code under the QR
	_, err := w.Write(cmd)
	return err
}
