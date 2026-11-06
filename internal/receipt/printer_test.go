package receipt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func render(t *testing.T, r Receipt) string {
	t.Helper()
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = time.Sleep })
	for _, key := range []string{"STORE_ADDRESS_LINE1", "STORE_ADDRESS_LINE2", "STORE_PHONE", "STORE_VAT_NUMBER", "STORE_SOCIAL_HANDLE", "STORE_WEBSITE", "RECEIPT_PRINTABLE_DOTS"} {
		t.Setenv(key, "")
	}

	var buf bytes.Buffer
	if err := (&Printer{}).writeReceipt(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func sampleReceipt() Receipt {
	return Receipt{
		StoreAddress1: "Librairie Test",
		Barcode:       "POS-20261106-ABCDEFGHJK",
		CreatedAt:     time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC),
		Items: []ReceiptItem{
			{Name: "Roman fictif", Quantity: 2, UnitPrice: "12.50"},
			{Name: "Album fictif", Quantity: 1, UnitPrice: "9.90", DiscountPercent: "10", OriginalUnitPrice: "11.00"},
		},
		Payments: []string{"Carte bancaire"},
		Total:    "34.90",
	}
}

func TestReceiptPrintsTicketTextThenCode128Barcode(t *testing.T) {
	out := render(t, sampleReceipt())

	text := strings.Index(out, "Ticket: POS-20261106-ABCDEFGHJK")
	code := strings.Index(out, "\x1dk\x49")
	if text < 0 || code < 0 || code < text {
		t.Fatalf("expected the ticket number then GS k 73, got text=%d code=%d", text, code)
	}
	if !strings.Contains(out, "\x1dw\x02") {
		t.Fatal("expected module width 2")
	}
	if strings.Contains(out, "\x1dk\x04") {
		t.Fatal("CODE39 must not be used for the sale barcode")
	}
}

func TestPricedReceiptKeepsPricesAndTotal(t *testing.T) {
	out := render(t, sampleReceipt())

	for _, want := range []string{"PRIX", "12.50", "-10%", "TOTAL:      34.90 EUR", "PAIEMENT: Carte bancaire"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "TICKET CADEAU") || strings.Contains(out, "TICKET DE RETOUR") {
		t.Error("a sale receipt has no gift/return title")
	}
}

func assertPriceless(t *testing.T, out, title string) {
	t.Helper()
	if !strings.Contains(out, title) {
		t.Errorf("missing title %q", title)
	}
	for _, forbidden := range []string{"PRIX", "12.50", "9.90", "11.00", "TOTAL", "EUR", "Sous-total", "PAIEMENT", "%"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("priceless receipt must not contain %q", forbidden)
		}
	}
	if !strings.Contains(out, "Roman fictif") || !strings.Contains(out, "QTE") {
		t.Error("items and quantities must still be printed")
	}
	if !strings.Contains(out, "\x1dk\x49") {
		t.Error("the sale barcode must still be printed")
	}
}

func TestGiftReceiptHidesPricesAndTotal(t *testing.T) {
	r := sampleReceipt()
	r.IsGiftReceipt = true
	// Gift payloads carry no unit_price: the old agent recomputed a "0.00" total.
	for i := range r.Items {
		r.Items[i].UnitPrice = ""
	}
	r.Total = ""
	r.Payments = nil

	assertPriceless(t, render(t, r), "TICKET CADEAU")
}

func TestReturnReceiptHidesPricesAndTotalEvenIfTheServerSendsThem(t *testing.T) {
	r := sampleReceipt()
	r.IsReturnReceipt = true
	r.DiscountLabel = "Remise adherent (5%)"
	r.DiscountAmount = "1.75"

	assertPriceless(t, render(t, r), "TICKET DE RETOUR")
}

func TestPricelessItemLineAlignsQuantityRight(t *testing.T) {
	line := pricelessItemLine(ReceiptItem{Name: strings.Repeat("x", 60), Quantity: 3})
	if len(line) != lineWidth || !strings.HasSuffix(line, "  3") {
		t.Fatalf("line = %q (%d)", line, len(line))
	}
}

func TestPrintReceiptWritesToTheDevice(t *testing.T) {
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = time.Sleep })
	device := filepath.Join(t.TempDir(), "printer")
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (&Printer{Device: device}).PrintReceipt(sampleReceipt()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(device)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte{0x1d, 'k', 73}) {
		t.Fatal("device did not receive the CODE128 barcode")
	}
}
