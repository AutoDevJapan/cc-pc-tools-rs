package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The released b7800769 CreateNew consumer admits this exact annotation and
// binds renamed references through source.json. This tests the producer data;
// it neither changes that consumer nor manufactures a creation qualification.
const consolidationCreateModificationNoticePrefix = "Modification notice: The distributor changed this reference only by replacing withheld source lines with \"[WITHHELD physical line N]\" markers. All remaining source lines and their original positions are preserved below.\n\n"

func validateConsolidationSourceCreationContract(metadata, declaredSource map[string]any, writer string, reference []byte, originalMaskedSHA string) error {
	if writer == "" || metadata["reformulation_writer"] != writer || !reflect.DeepEqual(metadata["source"], declaredSource) {
		return fmt.Errorf("renamed source must bind its canonical source and actual reformulation writer")
	}
	count, countOK := metadata["source_bytes"].(float64)
	blob, blobOK := metadata["source_git_blob"].(string)
	if !countOK || count <= 0 || count > 2<<20 || count != float64(int(count)) || !blobOK || !validHex(blob, 40) || blob == strings.Repeat("0", 40) {
		return fmt.Errorf("source object identity required by the released consumer is absent")
	}
	if !strings.HasPrefix(string(reference), consolidationCreateModificationNoticePrefix) || travel3Digest([]byte(strings.TrimPrefix(string(reference), consolidationCreateModificationNoticePrefix))) != originalMaskedSHA {
		return fmt.Errorf("released fixed modification notice or unchanged masked body differs")
	}
	return nil
}

func consolidationCreationContractFixture(t *testing.T) (map[string]any, map[string]any, string, []byte, string) {
	t.Helper()
	b, o, _, _ := loadConsolidationFixture(t)
	files := next4Object(b["targets"].([]any)[0])["files"].([]any)
	var metadata map[string]any
	var reference []byte
	for _, item := range files {
		f := next4Object(item)
		switch f["path"] {
		case "references/source.json":
			if err := json.Unmarshal([]byte(f["content"].(string)), &metadata); err != nil {
				t.Fatal(err)
			}
		case "references/original-skill.masked.txt":
			reference = []byte(f["content"].(string))
		}
	}
	return metadata, next4Object(o["sources"].([]any)[0]), next4Object(o["writer"])["job"].(string), reference, "cbf88de4df17b4b505ade3ef3f0efa1280190c18f80f58cda9aa4bcbcd847fc5"
}

func TestConsolidationSourceBindingsMatchReleasedCreateContract(t *testing.T) {
	m, s, writer, reference, sha := consolidationCreationContractFixture(t)
	if err := validateConsolidationSourceCreationContract(m, s, writer, reference, sha); err != nil {
		t.Fatal(err)
	}
}

func TestConsolidationMaskedReferenceUsesReleasedModificationNotice(t *testing.T) {
	_, _, _, reference, sha := consolidationCreationContractFixture(t)
	if !strings.HasPrefix(string(reference), consolidationCreateModificationNoticePrefix) || travel3Digest([]byte(strings.TrimPrefix(string(reference), consolidationCreateModificationNoticePrefix))) != sha {
		t.Fatal("actual proposal reference does not use the released fixed notice with the same masked bytes")
	}
}

func TestConsolidationCreateContractRejectsChangedBindingsAndReferences(t *testing.T) {
	for _, change := range []string{"valid", "missing writer", "other writer", "missing source", "changed source", "zero bytes", "fractional bytes", "zero blob", "unknown notice", "duplicate notice", "changed masked body"} {
		t.Run(change, func(t *testing.T) {
			m, s, writer, reference, sha := consolidationCreationContractFixture(t)
			switch change {
			case "missing writer":
				delete(m, "reformulation_writer")
			case "other writer":
				m["reformulation_writer"] = "another"
			case "missing source":
				delete(m, "source")
			case "changed source":
				m["source"] = map[string]any{"path": "another/SKILL.md"}
			case "zero bytes":
				m["source_bytes"] = float64(0)
			case "fractional bytes":
				m["source_bytes"] = 4539.5
			case "zero blob":
				m["source_git_blob"] = strings.Repeat("0", 40)
			case "unknown notice":
				reference = append([]byte("# Other modification notice\n"), reference...)
			case "duplicate notice":
				reference = append([]byte(consolidationCreateModificationNoticePrefix), reference...)
			case "changed masked body":
				reference = append(reference, 'x')
			}
			err := validateConsolidationSourceCreationContract(m, s, writer, reference, sha)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
