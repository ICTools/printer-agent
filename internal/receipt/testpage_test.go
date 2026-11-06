package receipt

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTestPage(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTestPage(&buf, []string{"Version: v1.0.0", "Poste: caisse-é"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"TEST IMPRESSION", "Version: v1.0.0", "Poste: caisse-e", "\x1dV\x01"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "\x1bp") {
		t.Error("the test page must not kick the cash drawer")
	}
}
