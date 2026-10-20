package inputqual

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskScopeIgnoresUnrelatedLegacyFieldTypes(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(f.p.db)
	if err != nil {
		t.Fatal(err)
	}
	// Selected tasks use the current schema; unrelated historical rows need not.
	data = bytes.Replace(data, []byte(`"items":[`), []byte(`"items":[{"id":1,"spec":{"old":"schema"},"wait_on":"legacy"},`), 1)
	if err := os.WriteFile(f.p.db, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := qualify(f.p.pin, f.p); err != nil {
		t.Fatal("unrelated legacy task blocked the finite original purpose")
	}
	data = bytes.Replace(data, []byte(`"id":1,`), []byte(`"id":3059,`), 1)
	if err := os.WriteFile(f.p.db, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := qualify(f.p.pin, f.p); err == nil {
		t.Fatal("malformed or duplicate selected task accepted")
	}
}

// Rebind only the synthetic prior observations, retaining independent original
// receipt/review identity. A self-consistent replacement must still be refused.
func (f *fixture) rebind() {
	f.t.Helper()
	var pin map[string]any
	data, err := os.ReadFile(f.p.pin)
	if err != nil {
		f.t.Fatal(err)
	}
	if json.Unmarshal(data, &pin) != nil {
		f.t.Fatal("synthetic plan")
	}
	for role, path := range map[string]string{"review": f.p.review, "acceptance": f.p.acceptance} {
		data, err := os.ReadFile(path)
		if err != nil {
			f.t.Fatal(err)
		}
		pin[role+"_sha256"] = hash(data)
		f.prior[role+"_sha256"] = hash(data)
	}
	a := f.put(f.p.pin, pin)
	f.prior["current_secret_pin"].(map[string]any)["secret_pin_sha256"] = a.sha
	f.updatePrior()
	data, err = os.ReadFile(f.p.final.path)
	if err != nil {
		f.t.Fatal(err)
	}
	var final map[string]any
	if json.Unmarshal(data, &final) != nil {
		f.t.Fatal("synthetic final")
	}
	final["current_product_candidate"].(map[string]any)["current_secret_pin_sha256"] = a.sha
	f.p.final = f.put(f.p.final.path, final)
	data, err = os.ReadFile(f.p.outcome.path)
	if err != nil {
		f.t.Fatal(err)
	}
	var outcome map[string]any
	if json.Unmarshal(data, &outcome) != nil {
		f.t.Fatal("synthetic outcome")
	}
	outcome["final_candidate_receipt_sha256"] = f.p.final.sha
	f.p.outcome = f.put(f.p.outcome.path, outcome)
}

func TestSelfConsistentPayloadRequiresIndependentOriginal(t *testing.T) {
	for _, role := range []string{"review", "acceptance"} {
		t.Run(role, func(t *testing.T) {
			f := newFixture(t)
			if _, err := qualify(f.p.pin, f.p); err != nil {
				t.Fatal(err)
			}
			path := map[string]string{"review": f.p.review, "acceptance": f.p.acceptance}[role]
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]any
			if json.Unmarshal(data, &envelope) != nil {
				t.Fatal("synthetic envelope")
			}
			changed := []byte("self-consistent unrelated private payload")
			envelope["original"] = changed
			envelope["original_sha256"] = hash(changed)
			f.put(path, envelope)
			f.rebind()
			_, err = qualify(f.p.pin, f.p)
			if err == nil || !strings.HasSuffix(err.Error(), role+"_independent_payload") {
				t.Fatal("independent payload mismatch was not reached and rejected")
			}
		})
	}
}

func TestOriginalStopCannotAuthorizeAnotherInput(t *testing.T) {
	f := newFixture(t)
	f.p.stop = f.put(f.p.stop.path, map[string]any{"origin_todo_id": 3059, "input_owned_id": f.prior["owned_id"], "guard_client_resource": true, "guard_classification_unverified": true, "input_files": []string{filepath.Base(f.p.pin), filepath.Base(f.p.review), "unknown"}})
	_, err := qualify(f.p.pin, f.p)
	if err == nil || !strings.HasSuffix(err.Error(), "original_guard_inputs") {
		t.Fatal("stop input identity did not fail")
	}
}

func TestLocatorCannotSupplyAuthorityOrRelocateOriginals(t *testing.T) {
	for _, name := range []string{"unknown-role", "self-approved", "relocated", "duplicate-key"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			paths := map[string]string{}
			for k := range originalLocations {
				paths[k] = f.p.pin
			}
			switch name {
			case "unknown-role":
				paths["another-task"] = f.p.pin
			case "self-approved":
				paths["approved"] = "true"
			case "relocated":
				paths["pin"] = filepath.Join(filepath.Dir(f.p.pin), "copied-plan")
			}
			data, err := json.Marshal(paths)
			if err != nil {
				t.Fatal(err)
			}
			if name == "duplicate-key" {
				data = append([]byte(`{"pin":"self-asserted",`), data[1:]...)
			}
			path := filepath.Join(filepath.Dir(f.p.pin), "locators.json")
			if os.WriteFile(path, data, 0600) != nil {
				t.Fatal("synthetic locator")
			}
			if _, err := loadPolicy(path); err == nil {
				t.Fatal("caller locator minted original authority")
			}
		})
	}
}
