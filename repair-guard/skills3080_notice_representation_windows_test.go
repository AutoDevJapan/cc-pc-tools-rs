package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skillsTestModificationPrefix = "Modification notice: The distributor changed this reference only by replacing withheld source lines with \"[WITHHELD physical line N]\" markers. All remaining source lines and their original positions are preserved below.\n\n"

func TestSkills3080IndividualNoticeRetainsOriginalAcquisitionRole(t *testing.T) {
	notice, err := os.ReadFile(filepath.Join("testdata", "skills3080-rlm-controller-LICENCE.txt"))
	if err != nil || len(notice) != 10752 || hashBytes(notice) != "120701352845f72eda11433beb40df387026e41c5a9c6b9c9cef0c6c624f6004" || skillsGitBlobID(notice) != "0093542cc130c66e50a69f86839a5989484a6db3" {
		t.Fatal("fixed public individual notice fixture differs")
	}
	for _, flat := range []bool{false, true} {
		original := []byte("synthetic primary source\n")
		primary := "skills/skywyze/rlm-controller/SKILL.md"
		license := "skills/skywyze/rlm-controller/LICENCE.md"
		f := skillsAcquiredFixtureLayout(t, original, notice, flat, primary, license, "support_text")
		m := skillsManifestWithOriginals(t, original, notice)
		m.Sources[0].Path, m.Sources[0].License.Path = primary, license
		before, err := os.ReadFile(f.index)
		if err != nil { t.Fatal(err) }
		fs := skillsFS{blocked: func(string) bool { return false }}
		accepted := skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors)
		fs.close()
		if !accepted { t.Fatalf("exact individual notice with original support_text role rejected; flat=%v stage=%s", flat, fs.failureStage) }
		after, err := os.ReadFile(f.index)
		if err != nil || string(after) != string(before) || !strings.Contains(string(after), `"role":"support_text"`) { t.Fatal("original acquisition index changed") }
		// A source still requires every byte of the notice, even if its SHA
		// declaration and historical role are unchanged.
		m.Files[2].body = append([]byte(nil), notice[:len(notice)-1]...)
		fs = skillsFS{blocked: func(string) bool { return false }}
		if skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors) { t.Fatal("truncated individual notice accepted") }
		fs.close()
	}
}

func TestSkills3080SupportTextDoesNotBecomeNoticeByName(t *testing.T) {
	for _, license := range []string{"LICENSE", "skills/sample/LICENCE.md", "skills/skywyze/rlm-controller/LICENCE.md"} {
		original, notice := []byte("synthetic primary\n"), []byte("synthetic same-name notice\n")
		f := skillsAcquiredFixtureLayout(t, original, notice, true, "skills/sample/SKILL.md", license, "support_text")
		m := skillsManifestWithOriginals(t, original, notice)
		m.Sources[0].License.Path = license
		fs := skillsFS{blocked: func(string) bool { return false }}
		if skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors) { t.Fatal("arbitrary support_text or filename-only notice accepted") }
		fs.close()
	}
}

func TestSkills3080FixedModificationNoticePreservesSamePayload(t *testing.T) {
	for _, suffix := range []string{"", "\n"} {
		for _, ending := range []string{"\n", "\r\n"} {
			raw := []byte("Visible source" + ending + "synthetic withheld" + suffix)
			payload := "Visible source\n[WITHHELD physical line 2]" + suffix
			if !skillsMaskedSourceBytes(raw, []byte(payload)) { t.Fatal("original prefix-free view rejected") }
			if !skillsMaskedSourceBytes(raw, []byte(skillsTestModificationPrefix+payload)) { t.Fatal("fixed modification notice rejected valid unchanged masked payload") }
		}
	}
	// The notice is outside the physical source line numbering.
	original, notice := []byte("Visible\nhidden\n"), []byte("synthetic exact notice\n")
	f := skillsAcquiredFixture(t, original, notice, true)
	m := skillsManifestWithOriginals(t, original, notice)
	m.Files[1] = skillsMaskedFixtureOutput([]byte(skillsTestModificationPrefix + "Visible\n[WITHHELD physical line 2]\n"))
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if !skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors) { t.Fatalf("annotated acquired view rejected: %s", fs.failureStage) }
}

func TestSkills3080ModificationNoticeCannotChangeSourceContract(t *testing.T) {
	const raw = "Visible\nhidden\n"
	const payload = "Visible\n[WITHHELD physical line 2]\n"
	for name, view := range map[string]string{
		"unknown_prefix": "Other modification notice\n\n" + payload,
		"altered_prefix": strings.Replace(skillsTestModificationPrefix, "distributor", "author", 1) + payload,
		"duplicate_prefix": skillsTestModificationPrefix + skillsTestModificationPrefix + payload,
		"missing_blank": strings.TrimSuffix(skillsTestModificationPrefix, "\n") + payload,
		"prefix_CRLF": strings.ReplaceAll(skillsTestModificationPrefix, "\n", "\r\n") + payload,
		"prefix_BOM": "\ufeff" + skillsTestModificationPrefix + payload,
		"prefix_NUL": strings.Replace(skillsTestModificationPrefix, "notice:", "notice:\x00", 1) + payload,
		"prefix_bad_UTF8": "\xff" + skillsTestModificationPrefix + payload,
		"new_text": skillsTestModificationPrefix + payload + "New text\n",
		"changed_visible": skillsTestModificationPrefix + "Changed\n[WITHHELD physical line 2]\n",
		"removed_source_line": skillsTestModificationPrefix + "Visible\n",
		"moved_marker": skillsTestModificationPrefix + "[WITHHELD physical line 1]\nVisible\n",
		"shifted_ordinal": skillsTestModificationPrefix + "Visible\n[WITHHELD physical line 4]\n",
		"missing_final_LF": skillsTestModificationPrefix + strings.TrimSuffix(payload, "\n"),
		"payload_CRLF": skillsTestModificationPrefix + strings.ReplaceAll(payload, "\n", "\r\n"),
		"payload_NUL": skillsTestModificationPrefix + payload + "\x00",
		"payload_bad_UTF8": skillsTestModificationPrefix + payload + "\xff",
		"no_redaction": skillsTestModificationPrefix + raw,
		"no_visible_source": skillsTestModificationPrefix + "[WITHHELD physical line 1]\n[WITHHELD physical line 2]\n",
	} {
		t.Run(name, func(t *testing.T) { if skillsMaskedSourceBytes([]byte(raw), []byte(view)) { t.Fatal("invalid annotated view accepted") } })
	}
	for _, original := range []string{"Visible\nhidden", "Visible\rhidden\n", "Visible\n\xff\n", "Visible\n\x00\n"} {
		if skillsMaskedSourceBytes([]byte(original), []byte(skillsTestModificationPrefix+payload)) { t.Fatal("original encoding or final LF mismatch accepted") }
	}
	// Original marker literals must not count as newly withheld or visible.
	for _, c := range [][2]string{
		{"Visible\n[WITHHELD physical line 2]\n", "Visible\n[WITHHELD physical line 2]\n"},
		{"hidden\n[WITHHELD physical line 2]\n", "[WITHHELD physical line 1]\n[WITHHELD physical line 2]\n"},
	} { if skillsMaskedSourceBytes([]byte(c[0]), []byte(skillsTestModificationPrefix+c[1])) { t.Fatal("original marker counted as new mask or visible source") } }
}

func TestSkills3080NoticeRepairPreservesObservedSoftwarePair(t *testing.T) {
	const sha = "9be9a066053183a099e78133571d394ecbb2845372e3cf603e292c74dac1a8d3"
	const head = "deb430b8b089fde2391dbd243c01db48de48d493"
	if !skillsAcceptedProducer(skillsProducer{sha, head}, skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}) || !official3894InstalledBaseline(sha, head) { t.Fatal("independently observed PR610 distribution rejected by existing updaters") }
	var evidence struct { SHA string `json:"sha256"`; Head string `json:"source_revision"`; Bytes int `json:"bytes"` }
	raw, err := os.ReadFile(filepath.Join("..", "docs", "evidence", "skills-3080-guard-before-notice-representation175645.json"))
	if err != nil || hashBytes(raw) != "5a5295d3ac186e2de15cc113ea4dd8f4fac02cf07a0835f371c0fb03612c6bfe" || json.Unmarshal(raw, &evidence) != nil || evidence.SHA != sha || evidence.Head != head || evidence.Bytes != 13315072 { t.Fatal("original independent observation changed") }
	for _, pair := range []skillsProducer{{sha, strings.Repeat("b", 40)}, {strings.Repeat("a", 64), head}, {strings.Repeat("a", 64), strings.Repeat("b", 40)}} {
		if skillsAcceptedProducer(pair, skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}) || official3894InstalledBaseline(pair.SHA, pair.Revision) { t.Fatal("unknown hash or different revision admitted") }
	}
}
