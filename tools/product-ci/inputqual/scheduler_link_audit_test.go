package inputqual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hard link is an alternate mutable name even when every expected byte hash
// matches. Exercise an otherwise eligible policy so a scope rejection cannot
// make the link assertion pass without reaching the physical-file boundary.
func TestPurposeRejectsHardLinkedOriginals(t *testing.T) {
	for _, role := range []string{"pin", "review", "acceptance", "scope", "original"} {
		t.Run(role, func(t *testing.T) {
			f := newFixture(t)
			if _, err := qualify(f.p.pin, f.p); err != nil {
				t.Fatal("ordinary source-bound fixture must qualify first")
			}
			path := map[string]string{"pin": f.p.pin, "review": f.p.review, "acceptance": f.p.acceptance, "scope": f.p.scope.path, "original": f.p.original.path}[role]
			alias := filepath.Join(filepath.Dir(path), "unregistered-hardlink")
			if err := os.Link(path, alias); err != nil {
				t.Fatal(err)
			}
			if _, err := qualify(f.p.pin, f.p); err == nil {
				t.Fatal("purpose accepted an original with an unregistered hard-link alias")
			}
		})
	}
}

func TestPurposeRejectsReviewOfAnotherHead(t *testing.T) {
	f := newFixture(t)
	if _, err := qualify(f.p.pin, f.p); err != nil {
		t.Fatal("ordinary source-bound fixture must qualify first")
	}
	data, err := os.ReadFile(f.p.original.path)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	original["final_review"].(map[string]any)["candidate"] = strings.Repeat("f", 40)
	f.p.original = f.put(f.p.original.path, original)
	f.prior["original_receipt_sha256"] = f.p.original.sha
	f.updatePrior()
	if _, err := qualify(f.p.pin, f.p); err == nil {
		t.Fatal("a review of another head qualified the fixed source")
	}
}
