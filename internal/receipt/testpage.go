package receipt

import (
	"io"
	"os"
	"strings"
)

// PrintTestPage prints a short ticket with the given lines (no price, no
// barcode, no drawer kick): `printer test` uses it to check the printer path.
func (p *Printer) PrintTestPage(lines []string) error {
	f, err := os.OpenFile(p.Device, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Sync()
		_ = f.Close()
	}()
	return writeTestPage(f, lines)
}

func writeTestPage(w io.Writer, lines []string) error {
	var b strings.Builder
	b.Write([]byte{0x1b, '@'})       // initialize
	b.Write([]byte{0x1b, 't', 0x13}) // character table
	b.Write([]byte{0x1b, 'a', 1})    // center
	b.Write([]byte{0x1b, 'E', 1})    // bold
	b.WriteString("TEST IMPRESSION\n")
	b.Write([]byte{0x1b, 'E', 0})
	b.WriteString(strings.Repeat("-", 32) + "\n")
	b.Write([]byte{0x1b, 'a', 0}) // left
	for _, l := range lines {
		b.WriteString(removeAccents(l) + "\n")
	}
	b.WriteString("\n\n\n\n")
	b.Write([]byte{0x1d, 'V', 1}) // partial cut
	_, err := io.WriteString(w, b.String())
	return err
}
