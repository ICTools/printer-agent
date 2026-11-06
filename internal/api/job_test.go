package api

import (
	"encoding/json"
	"testing"
)

// chapitreneuf #874: contract fields of the receipt payload.
func TestParseReceiptPayloadReadsBarcodeContract(t *testing.T) {
	job := Job{Payload: json.RawMessage(`{
		"barcode": "POS-20261106-ABCDEFGHJK",
		"barcode_symbology": "CODE128",
		"is_gift_receipt": true,
		"items": [{"name": "Roman fictif", "quantity": 1}]
	}`)}

	p, err := job.ParseReceiptPayload()
	if err != nil {
		t.Fatal(err)
	}
	if p.Barcode != "POS-20261106-ABCDEFGHJK" || p.BarcodeSymbology != "CODE128" {
		t.Fatalf("barcode contract not parsed: %+v", p)
	}
	if !p.IsGiftReceipt || p.IsReturnReceipt {
		t.Fatalf("flags not parsed: %+v", p)
	}
}

// A server that predates #874 sends neither barcode_symbology nor the flags.
func TestParseReceiptPayloadFromOlderServer(t *testing.T) {
	job := Job{Payload: json.RawMessage(`{"barcode": "POS-20260310-ABCD1234", "items": [], "total": "0.00"}`)}

	p, err := job.ParseReceiptPayload()
	if err != nil {
		t.Fatal(err)
	}
	if p.BarcodeSymbology != "" || p.IsGiftReceipt || p.IsReturnReceipt {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}

func TestParseReceiptPayloadReturnFlag(t *testing.T) {
	job := Job{Payload: json.RawMessage(`{"barcode": "POS-20261106-ABCDEFGHJK", "is_return_receipt": true, "items": []}`)}

	p, err := job.ParseReceiptPayload()
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsReturnReceipt {
		t.Fatal("is_return_receipt not parsed")
	}
}
