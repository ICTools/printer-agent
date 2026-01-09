package registry

import (
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	info := PrinterInfo{
		ID:         "test-printer",
		Type:       PrinterTypeReceipt,
		DevicePath: "/dev/test",
		Available:  true,
	}

	reg.Register(info)

	got, err := reg.Get("test-printer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != info.ID {
		t.Errorf("expected ID %s, got %s", info.ID, got.ID)
	}
	if got.Type != info.Type {
		t.Errorf("expected Type %s, got %s", info.Type, got.Type)
	}
	if got.DevicePath != info.DevicePath {
		t.Errorf("expected DevicePath %s, got %s", info.DevicePath, got.DevicePath)
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent printer")
	}
}

func TestRegistry_GetByType(t *testing.T) {
	reg := NewRegistry()

	reg.Register(PrinterInfo{
		ID:         "receipt-1",
		Type:       PrinterTypeReceipt,
		DevicePath: "/dev/receipt1",
		Available:  false,
	})
	reg.Register(PrinterInfo{
		ID:         "receipt-2",
		Type:       PrinterTypeReceipt,
		DevicePath: "/dev/receipt2",
		Available:  true,
	})
	reg.Register(PrinterInfo{
		ID:         "label-1",
		Type:       PrinterTypeLabel,
		DevicePath: "/dev/label1",
		Available:  true,
	})

	// Should get the available receipt printer
	got, err := reg.GetByType(PrinterTypeReceipt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "receipt-2" {
		t.Errorf("expected receipt-2, got %s", got.ID)
	}

	// Should get the label printer
	got, err = reg.GetByType(PrinterTypeLabel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "label-1" {
		t.Errorf("expected label-1, got %s", got.ID)
	}
}

func TestRegistry_GetByType_NoneAvailable(t *testing.T) {
	reg := NewRegistry()

	reg.Register(PrinterInfo{
		ID:         "receipt-1",
		Type:       PrinterTypeReceipt,
		DevicePath: "/dev/receipt1",
		Available:  false,
	})

	_, err := reg.GetByType(PrinterTypeReceipt)
	if err == nil {
		t.Fatal("expected error when no printer available")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()

	reg.Register(PrinterInfo{ID: "p1", Type: PrinterTypeReceipt})
	reg.Register(PrinterInfo{ID: "p2", Type: PrinterTypeLabel})

	list := reg.List()
	if len(list) != 2 {
		t.Errorf("expected 2 printers, got %d", len(list))
	}
}

func TestRegistry_Clear(t *testing.T) {
	reg := NewRegistry()

	reg.Register(PrinterInfo{ID: "p1", Type: PrinterTypeReceipt})
	reg.Clear()

	list := reg.List()
	if len(list) != 0 {
		t.Errorf("expected 0 printers after clear, got %d", len(list))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()

	// Run concurrent registrations and reads
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			reg.Register(PrinterInfo{
				ID:   "printer-" + string(rune('0'+id)),
				Type: PrinterTypeReceipt,
			})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		go func() {
			reg.List()
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}
