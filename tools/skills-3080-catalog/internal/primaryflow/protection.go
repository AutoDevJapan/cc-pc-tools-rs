package primaryflow

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed protection_manifest.json
var fixedProtectionJSON []byte

const fixedProtectionSHA = "67044de760acbff946aa240a2397cc8941fe5f0fb30323b684debc458133b548"

type ProtectionClosure struct {
	Domain     string    `json:"domain"`
	InputCount int       `json:"input_count"`
	Pins       []Pin     `json:"pins"`
	Count      int       `json:"protected_count"`
	SetSHA     string    `json:"protected_sorted_JSON_sha256"`
	Plans      [2]string `json:"admitted_cohort_SHA256"`
}

func fixedProtectionClosure() (ProtectionClosure, error) {
	return parseProtectionClosure(fixedProtectionJSON)
}
func parseProtectionClosure(raw []byte) (ProtectionClosure, error) {
	var c ProtectionClosure
	if Hash(raw) != fixedProtectionSHA || Decode(raw, &c) != nil || c.Domain != "skills3080-fixed-protection-closure-v1" || len(c.Pins) != 184 || c.Count != 6222 || c.SetSHA != "bf933a84a19c4f8a3ff06c1b8ba0cb3f10ba21120133a60bd8ad5f848556f828" || c.Plans[0] != "edb8069e0564eb3b95fd0863865b66c4e0b1d3fb994990c6701bcc624678cabf" || c.Plans[1] != "9ebe1d51eef54663c578687b103e69eeb6fe687709def40b790faa104184195a" {
		return c, errors.New("fixed compiled protection closure differs")
	}
	c.InputCount = 184
	return c, nil
}

func VerifyProtection() (any, error) {
	c, h, _, e := loadProtection()
	if e != nil {
		return nil, e
	}
	return map[string]any{"domain": "skills3080-fixed-protection-verified-v1", "compiled_manifest_sha256": fixedProtectionSHA, "fixed_input_pins": c.InputCount, "protected_count": len(h), "protected_sorted_JSON_sha256": c.SetSHA, "admitted_cohort_SHA256": c.Plans, "source_bodies_opened": 0, "new_semantic_or_rights_claims": 0}, nil
}
func protectionPath(name string) (string, error) {
	if strings.HasPrefix(name, "docs/skills-3080-") && filepath.Base(name) == strings.TrimPrefix(name, "docs/") {
		return filepath.Join(filepath.Dir(filepath.Dir(Parent)), filepath.FromSlash(name)), nil
	}
	if filepath.Base(name) == name && !strings.ContainsAny(name, `\/:`) && strings.HasSuffix(name, ".json") {
		return filepath.Join(Parent, name), nil
	}
	return "", errors.New("protection input outside fixed task metadata")
}

// Expected artifact bytes and complete protected membership are fixed by a
// compiled, reviewed closure; never choose a fresh glob or trust a new hash
// collected after opening an arbitrary current file.
func checkedProtection(c ProtectionClosure, read func(string) ([]byte, error)) (map[string]bool, map[string][]byte, error) {
	if c.InputCount < 1 || len(c.Pins) != c.InputCount || c.Count < 1 || !digest.MatchString(c.SetSHA) {
		return nil, nil, errors.New("protection input count/membership anchor differs")
	}
	hashes := map[string]bool{}
	raw := map[string][]byte{}
	for _, p := range c.Pins {
		abs, e := protectionPath(p.Path)
		if e != nil || raw[p.Path] != nil || !digest.MatchString(p.SHA) || p.Bytes < 1 || p.Bytes > 8<<20 {
			return nil, nil, errors.New("invalid or repeated fixed protection input")
		}
		b, e := read(abs)
		if e != nil {
			return nil, nil, errors.New("fixed protection input missing")
		}
		if int64(len(b)) != p.Bytes || Hash(b) != p.SHA {
			return nil, nil, errors.New("fixed protection input bytes differ")
		}
		var v any
		if e = Decode(b, &v); e != nil {
			return nil, nil, e
		}
		HashStrings(v, hashes)
		raw[p.Path] = b
	}
	sorted := []string{}
	for h := range hashes {
		sorted = append(sorted, h)
	}
	sort.Strings(sorted)
	set, e := json.Marshal(sorted)
	if e != nil || len(sorted) != c.Count || Hash(set) != c.SetSHA {
		return nil, nil, errors.New("fixed protected digest membership differs")
	}
	return hashes, raw, nil
}

func loadProtection() (ProtectionClosure, map[string]bool, map[string][]byte, error) {
	c, e := fixedProtectionClosure()
	if e != nil {
		return c, nil, nil, e
	}
	h, b, e := checkedProtection(c, func(path string) ([]byte, error) {
		i, e := os.Lstat(path)
		if e != nil {
			return nil, e
		}
		if !i.Mode().IsRegular() || i.Size() > 8<<20 {
			return nil, errors.New("bounded regular protection metadata required")
		}
		return os.ReadFile(path)
	})
	return c, h, b, e
}
