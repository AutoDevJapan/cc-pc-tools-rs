package sourcepacket

import (
	"bytes"
	"strings"
	"testing"
)

func TestSignedAPIPayloadPreservesSignatureFinalContinuation(t *testing.T) {
	var c commitResponse
	c.Tree.SHA = strings.Repeat("a", 40)
	c.Author.Name, c.Author.Email, c.Author.Date = "Public", "fixture@example.invalid", "2026-10-17T00:00:00Z"
	c.Committer = c.Author
	c.Message = "public body"
	c.Verification.Payload = "tree " + c.Tree.SHA + "\nauthor Public <fixture@example.invalid> 1792195200 +0000\ncommitter Public <fixture@example.invalid> 1792195200 +0000\n\npublic body\n"
	c.Verification.Signature = "PUBLIC SIGNATURE\nPUBLIC END\n"
	header, body, _ := strings.Cut(c.Verification.Payload, "\n\n")
	want := []byte(header + "\ngpgsig " + strings.ReplaceAll(c.Verification.Signature, "\n", "\n ") + "\n\n" + body)
	got, e := originalPayload(c, gitID("commit", want))
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("signature final continuation was discarded")
	}
}

func TestSignedAPIPayloadPreservesOriginalTimezoneAndExactBody(t *testing.T) {
	for _, signature := range []string{"PUBLIC SIGNATURE\nPUBLIC END\n", "PUBLIC SIGNATURE\nPUBLIC END"} {
		var c commitResponse
		c.Verification.Payload = "tree " + strings.Repeat("a", 40) + "\nparent " + strings.Repeat("b", 40) + "\nauthor Public <fixture@example.invalid> 12345 -0730\ncommitter Public <fixture@example.invalid> 12345 -0730\n\npublic body\n"
		c.Verification.Signature = signature
		header, body, _ := strings.Cut(c.Verification.Payload, "\n\n")
		for _, embedded := range []string{signature, strings.TrimSuffix(signature, "\n")} {
			want := []byte(header + "\ngpgsig " + strings.ReplaceAll(embedded, "\n", "\n ") + "\n\n" + body)
			got, e := originalPayload(c, gitID("commit", want))
			if e != nil || !bytes.Equal(got, want) {
				t.Fatal("original signed bytes could not be reconstructed")
			}
		}
		if _, e := originalPayload(c, strings.Repeat("c", 40)); e == nil {
			t.Fatal("wrong signed commit hash accepted")
		}
	}
}
