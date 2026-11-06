package receipt

import (
	"bytes"
	"testing"
)

func TestEncodeCode128UsesCodeSetCForTheDate(t *testing.T) {
	sym, err := encodeCode128("POS-20261106-ABCDEFGHJK")
	if err != nil {
		t.Fatal(err)
	}

	want := append([]byte("{BPOS-{C"), 20, 26, 11, 6)
	want = append(want, []byte("{B-ABCDEFGHJK")...)
	if !bytes.Equal(sym.data, want) {
		t.Fatalf("data = %q, want %q", sym.data, want)
	}
	// 4 (POS-) + 1 switch + 4 pairs + 1 switch + 11 (-ABCDEFGHJK)
	if sym.symbols != 21 {
		t.Fatalf("symbols = %d, want 21", sym.symbols)
	}
	// (21 + start + check) * 11 + stop 13 = 266 modules, x2 = 532 dots < 576
	if got := sym.widthDots(2); got != 532 {
		t.Fatalf("width = %d dots, want 532", got)
	}
}

func TestEncodeCode128TrailingDigitsUseCodeSetC(t *testing.T) {
	sym, err := encodeCode128("POS-20261106-0123456789")
	if err != nil {
		t.Fatal(err)
	}

	want := append([]byte("{BPOS-{C"), 20, 26, 11, 6)
	want = append(want, []byte("{B-{C")...)
	want = append(want, 1, 23, 45, 67, 89)
	if !bytes.Equal(sym.data, want) {
		t.Fatalf("data = %q, want %q", sym.data, want)
	}
	if sym.symbols != 17 {
		t.Fatalf("symbols = %d, want 17", sym.symbols)
	}
}

func TestEncodeCode128LegacyEightCharacterCode(t *testing.T) {
	sym, err := encodeCode128("POS-20260310-ABCD1234")
	if err != nil {
		t.Fatal(err)
	}
	if sym.widthDots(barcodeModuleWidth) > DefaultPrintableDots {
		t.Fatalf("legacy code too wide: %d dots", sym.widthDots(barcodeModuleWidth))
	}
}

func TestEncodeCode128OddDigitRunKeepsFirstDigitInCodeSetB(t *testing.T) {
	sym, err := encodeCode128("A1234567")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{'{', 'B', 'A', '1', '{', 'C', 23, 45, 67}
	if !bytes.Equal(sym.data, want) {
		t.Fatalf("data = %q, want %q", sym.data, want)
	}
}

func TestEncodeCode128AllDigitsStartsInCodeSetC(t *testing.T) {
	sym, err := encodeCode128("9782070612758")
	if err != nil {
		t.Fatal(err)
	}
	// 13 digits: one in B, then 6 pairs in C.
	want := []byte{'{', 'B', '9', '{', 'C', 78, 20, 70, 61, 27, 58}
	if !bytes.Equal(sym.data, want) {
		t.Fatalf("data = %q, want %q", sym.data, want)
	}
}

func TestEncodeCode128EscapesBraceAndRejectsNonASCII(t *testing.T) {
	sym, err := encodeCode128("A{B")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sym.data, []byte("{BA{{B")) {
		t.Fatalf("data = %q", sym.data)
	}
	if _, err := encodeCode128("caf\xc3\xa9"); err == nil {
		t.Fatal("expected an error for non-ASCII data")
	}
	if _, err := encodeCode128(""); err == nil {
		t.Fatal("expected an error for empty data")
	}
}

func TestWriteSaleBarcodeEmitsCode128FunctionBWithModuleWidth2(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSaleBarcode(&buf, "POS-20261106-ABCDEFGHJK", "CODE128", DefaultPrintableDots); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()

	for name, cmd := range map[string][]byte{
		"GS w 2": {0x1d, 'w', 2},
		"GS H 2": {0x1d, 'H', 2},
		"GS h":   {0x1d, 'h', barcodeHeight},
	} {
		if !bytes.Contains(out, cmd) {
			t.Errorf("missing %s in %q", name, out)
		}
	}
	sym, _ := encodeCode128("POS-20261106-ABCDEFGHJK")
	want := append([]byte{0x1d, 'k', 73, byte(len(sym.data))}, sym.data...)
	if !bytes.Contains(out, want) {
		t.Fatalf("missing GS k 73 n {B... in %q", out)
	}
	if bytes.Contains(out, []byte{0x1d, 'k', 4}) {
		t.Fatal("CODE39 (GS k 4) must not be used any more")
	}
}

func TestWriteSaleBarcodeDefaultsToCode128WhenServerSendsNoSymbology(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSaleBarcode(&buf, "POS-20261106-ABCDEFGHJK", "", DefaultPrintableDots); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte{0x1d, 'k', 73}) {
		t.Fatalf("expected CODE128, got %q", buf.Bytes())
	}
}

func TestWriteSaleBarcodeFallsBackToQRWhenTooWide(t *testing.T) {
	var buf bytes.Buffer
	// 58 mm paper: 384 dots, the 532-dot CODE128 does not fit.
	if err := writeSaleBarcode(&buf, "POS-20261106-ABCDEFGHJK", "CODE128", 384); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()
	if bytes.Contains(out, []byte{0x1d, 'k', 73}) {
		t.Fatal("CODE128 printed although wider than the paper")
	}
	store := []byte{0x1d, '(', 'k', 26, 0, 49, 80, 48}
	if !bytes.Contains(out, append(store, []byte("POS-20261106-ABCDEFGHJK")...)) {
		t.Fatalf("missing QR store command in %q", out)
	}
	if !bytes.Contains(out, []byte{0x1d, '(', 'k', 3, 0, 49, 81, 48}) {
		t.Fatal("missing QR print command")
	}
}

func TestWriteSaleBarcodeHonoursQRSymbology(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSaleBarcode(&buf, "POS-20261106-ABCDEFGHJK", "qr", DefaultPrintableDots); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte{0x1d, '(', 'k'}) {
		t.Fatal("expected a QR code")
	}
}

func TestPrintableDotsFromEnvironment(t *testing.T) {
	t.Setenv("RECEIPT_PRINTABLE_DOTS", "384")
	if got := printableDots(); got != 384 {
		t.Fatalf("printableDots = %d", got)
	}
	t.Setenv("RECEIPT_PRINTABLE_DOTS", "nope")
	if got := printableDots(); got != DefaultPrintableDots {
		t.Fatalf("printableDots = %d", got)
	}
}
