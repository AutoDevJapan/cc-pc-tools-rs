package primaryflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompiledProtectionClosureCannotBeReboundByCaller(t *testing.T) {
	c, e := parseProtectionClosure(fixedProtectionJSON)
	if e != nil {
		t.Fatal(e)
	}
	if c.InputCount != 184 || c.Count != 6222 || c.Plans[0] == c.Plans[1] {
		t.Fatal("fixed closure changed")
	}
	tampered := append([]byte(nil), fixedProtectionJSON...)
	tampered[len(tampered)-2] ^= 1
	if _, e = parseProtectionClosure(tampered); e == nil {
		t.Fatal("changed runtime-selected protection closure accepted")
	}
	if _, e := Load(Parent+"/primaryflow-cohort0-v2-214717.json", strings.Repeat("a", 64)); e == nil {
		t.Fatal("caller-selected plan digest accepted")
	}
}

func TestProtectionRejectsSameCountReplacedInputAndOmittedPin(t *testing.T) {
	one, two := strings.Repeat("a", 64), strings.Repeat("b", 64)
	original := []byte(`{"sha256":"` + one + `"}`)
	changed := []byte(`{"sha256":"` + two + `"}`)
	set, _ := json.Marshal([]string{one})
	c := ProtectionClosure{Domain: "skills3080-fixed-protection-closure-v1", InputCount: 1, Pins: []Pin{{Path: "research3719-chunk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.json", SHA: Hash(original), Bytes: int64(len(original))}}, Count: 1, SetSHA: Hash(set)}
	for _, kind := range []string{"changed bytes same hash count", "omitted pin"} {
		t.Run(kind, func(t *testing.T) {
			x := c
			if kind == "omitted pin" {
				x.Pins = nil
			}
			if _, _, e := checkedProtection(x, func(string) ([]byte, error) { return changed, nil }); e == nil {
				t.Fatal("untrusted protection input was accepted")
			}
		})
	}
	if _, _, e := checkedProtection(c, func(string) ([]byte, error) { return original, nil }); e != nil {
		t.Fatal(e)
	}
}
