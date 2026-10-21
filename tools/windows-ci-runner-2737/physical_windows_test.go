//go:build windows && amd64

package windowsrunner

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsSyntheticPhysicalProofAndCRLFDrift(t *testing.T) {
	p := fixturePacket()
	root := filepath.Join(t.TempDir(), "checkout")
	proof, e := sourcepacket.Materialize(context.Background(), p, p.Pin, root)
	if e != nil || !proof.PhysicalBlobsVerified {
		t.Fatalf("synthetic physical proof: %v", e)
	}
	file := filepath.Join(root, "sentinel.txt")
	body, e := os.ReadFile(file)
	if e != nil || !bytes.Contains(body, []byte("\r\n")) {
		t.Fatal("original CRLF changed")
	}
	if e = os.WriteFile(file, bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = sourcepacket.VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, root); e == nil {
		t.Fatal("physical bytes drift accepted")
	}
}
