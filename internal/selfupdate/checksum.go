package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseChecksums reads a sha256sum file ("<hex>  <name>" per line, the name
// optionally prefixed by "*" for binary mode) into a name -> hex digest map.
func ParseChecksums(data []byte) (map[string]string, error) {
	sums := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("checksums line %d: malformed %q", n, line)
		}
		digest := strings.ToLower(fields[0])
		if b, err := hex.DecodeString(digest); err != nil || len(b) != sha256.Size {
			return nil, fmt.Errorf("checksums line %d: invalid sha256 %q", n, fields[0])
		}
		sums[strings.TrimPrefix(fields[1], "*")] = digest
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("checksums file is empty")
	}
	return sums, nil
}

// ExpectedChecksum returns the digest published for the asset.
func ExpectedChecksum(checksums []byte, asset string) (string, error) {
	sums, err := ParseChecksums(checksums)
	if err != nil {
		return "", err
	}
	digest, ok := sums[asset]
	if !ok {
		return "", fmt.Errorf("no checksum published for %s", asset)
	}
	return digest, nil
}

// VerifyFile checks that the file at path has the expected sha256 digest.
func VerifyFile(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got %s, expected %s", got, expected)
	}
	return nil
}
