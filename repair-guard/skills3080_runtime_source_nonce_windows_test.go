package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080RuntimeSourceNonceIsPrivateTypedCorrelation(t *testing.T) {
	for _, consumer := range []bool{false, true} {
		for _, kind := range []string{"legacy", "issued", "null", "empty", "short", "uppercase", "zero", "digest", "number", "alias", "duplicate"} {
			t.Run(kind+"/"+map[bool]string{false: "review", true: "consumer"}[consumer], func(t *testing.T) {
				var base []byte
				if consumer {
					c, _, _, _ := skillsConsumerFixture()
					base, _ = json.Marshal(c)
				} else {
					base, _ = json.Marshal(skillsPublicScopeFixture())
				}
				if bytes.Contains(base, []byte("runtime_source_nonce")) {
					t.Fatal("legacy serialization acquired a new field")
				}
				value := any(strings.Repeat("a1", 16))
				switch kind {
				case "null":
					value = nil
				case "empty":
					value = ""
				case "short":
					value = "a1"
				case "uppercase":
					value = strings.Repeat("A1", 16)
				case "zero":
					value = strings.Repeat("0", 32)
				case "digest":
					value = strings.Repeat("a1", 32)
				case "number":
					value = 1
				}
				var m map[string]any
				json.Unmarshal(base, &m)
				if kind != "legacy" {
					key := "runtime_source_nonce"
					if kind == "alias" {
						key = "Runtime_Source_Nonce"
					}
					m[key] = value
				}
				raw, _ := json.Marshal(m)
				if kind == "duplicate" {
					raw = append(raw[:len(raw)-1], []byte(`,"runtime_source_nonce":"`+strings.Repeat("b2", 16)+`"}`)...)
				}
				var ok bool
				if consumer {
					_, ok = skillsParseConsumer(raw)
				} else {
					_, ok = skillsParsePublicReview(raw)
				}
				if ok != (kind == "legacy" || kind == "issued") {
					t.Fatalf("typed correlation %s accepted=%v", kind, ok)
				}
			})
		}
	}
}

func TestSkills3080RuntimeSourceNonceCannotExtendRepairScope(t *testing.T) {
	c := skillsPublicScopeFixture()
	c.Domain, c.Instructions = "skills-3080-public-repair-v1", skillsPublicRepairInstructions
	raw, _ := json.Marshal(c)
	if _, ok := skillsParsePublicRepair(raw); !ok {
		t.Fatal("original repair contract changed")
	}
	c.RuntimeSourceNonce = strings.Repeat("a1", 16)
	raw, _ = json.Marshal(c)
	if _, ok := skillsParsePublicRepair(raw); ok {
		t.Fatal("data-source correlation expanded the repair contract")
	}
}
