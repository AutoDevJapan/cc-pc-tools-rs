package inputqual

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConstructFromIndependentOriginalsWithoutRejectedFiles(t *testing.T) {
	f := newFixture(t)
	first, err := qualifyPurpose(f.p.pin, f.p, true)
	if err != nil {
		t.Fatal(err)
	}
	// No copy, reserialization or read of any of the three protected inputs is
	// necessary. Their absence/changed payload cannot supply the new evidence.
	for _, path := range []string{f.p.pin, f.p.review, f.p.acceptance} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	second, err := qualifyPurpose(f.p.pin, f.p, true)
	if err != nil {
		t.Fatal("constructor depended on rejected original file bytes")
	}
	if first.Report() != second.Report() || second.Report().OriginalInputFilesRead || second.Report().InputConstruction != "official-helper-from-independent-original-receipts" || second.Report().OfficialCISuccess {
		t.Fatal("construction lost original provenance or claimed CI success")
	}
	r := second.Report()
	called := false
	err = second.Consume(func(pin, review, acceptance []byte) error {
		called = true
		if hash(pin) != r.PinSHA || hash(review) != r.ReviewSHA || hash(acceptance) != r.AcceptanceSHA {
			t.Fatal("consumer got other bytes")
		}
		var v struct {
			Original []byte `json:"original"`
		}
		if decode(review, &v, true) == nil {
			t.Fatal("strict partial decode unexpectedly accepted envelope")
		}
		if json.Unmarshal(review, &v) != nil || !sameOriginalReviewRecord(v.Original, mustReadFixture(t, f.p.original.path)) {
			t.Fatal("constructed review is not the independent original record")
		}
		if json.Unmarshal(acceptance, &v) != nil || hash(v.Original) != f.p.original.sha {
			t.Fatal("constructed acceptance is not the independent original receipt")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatal("constructed snapshot not consumed")
	}
	if _, err := qualify(f.p.pin, f.p); err == nil {
		t.Fatal("old input was silently reclassified by construction")
	}
}

func mustReadFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConstructRequiresOriginalHeadAndAuthority(t *testing.T) {
	for _, label := range []string{"missing-original", "changed-head", "changed-source", "stopped"} {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			switch label {
			case "missing-original":
				if os.Remove(f.p.original.path) != nil {
					t.Fatal("remove fixture")
				}
			case "changed-head":
				f.prior["head"] = strings.Repeat("f", 40)
				f.updatePrior()
			case "changed-source":
				f.p.origin = func(string) bool { return false }
			case "stopped":
				data := mustReadFixture(t, f.p.db)
				if os.WriteFile(f.p.db, []byte(strings.Replace(string(data), `"status":"doing"`, `"status":"block"`, 1)), 0600) != nil {
					t.Fatal("change fixture")
				}
			}
			if _, err := qualifyPurpose(f.p.pin, f.p, true); err == nil {
				t.Fatal("constructor minted authority without original scope")
			}
		})
	}
}
