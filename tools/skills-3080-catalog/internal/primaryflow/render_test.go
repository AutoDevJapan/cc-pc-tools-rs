package primaryflow

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func maskedGroup(raw []byte) Group {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d%c", len(raw), 0)
	h.Write(raw)
	return Group{Ordinal: 1, SHA: Hash(raw), Bytes: int64(len(raw)), Members: []Member{{Blob: hex.EncodeToString(h.Sum(nil))}}}
}

func TestSectionHashesPreserveWholeViewTerminalNewlineAndNormalizeDisplayedRange(t *testing.T) {
	for _, ending := range []string{"", "\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending_%x", ending), func(t *testing.T) {
			raw := []byte("public example" + ending)
			view, err := CheckAndMask(raw, maskedGroup(raw))
			if err != nil {
				t.Fatal(err)
			}
			section, err := Section(view, 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			rangeView := section.(map[string]any)
			expectedWhole := "public example"
			if ending != "" {
				expectedWhole += "\n"
			}
			if view.MaskedSHA != Hash([]byte(expectedWhole)) || rangeView["masked_range"] != "public example\n" || rangeView["range_sha256"] != Hash([]byte("public example\n")) {
				t.Fatal("whole view or displayed range byte representation changed")
			}
			if (rangeView["range_sha256"] == view.MaskedSHA) != (ending != "") {
				t.Fatal("a full physical range need not share the whole-view digest when the source lacks terminal LF")
			}
			if view.NewRawReadCredit != 0 || !view.UnseenMeaningsUnknown {
				t.Fatal("digest representation check changed read authority")
			}
		})
	}
}
func TestViewMasksBeforeOutputAndKeepsPhysicalLineIdentity(t *testing.T) {
	raw := []byte("---\r\nname: x\r\n---\r\n# compare visible conditions\r\nauthorization: synthetic-test-value\r\n-----BEGIN TEST PRIVATE KEY-----\r\nshort dummy material\r\n-----END TEST PRIVATE KEY-----\r\ncard identifier fragment 1234\r\nnormal final text\r\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if v.Lines != 10 || len(v.Withheld) != 8 || strings.Contains(v.Visible, "synthetic-test-value") || strings.Contains(v.Visible, "short dummy material") || strings.Contains(v.Visible, "1234") || !strings.Contains(v.Visible, "# compare visible conditions\n[WITHHELD physical line 5]") || !strings.HasSuffix(v.Visible, "normal final text\n") {
		t.Fatalf("unexpected masked view profile: lines%d masks%d", v.Lines, len(v.Withheld))
	}
	if v.NewRawReadCredit != 0 || !v.UnseenMeaningsUnknown {
		t.Fatal("mask became raw or unseen claim")
	}
}
func TestViewRejectsModifiedOrInvalidOriginalBeforeAnyView(t *testing.T) {
	raw := []byte("visible\n")
	g := maskedGroup(raw)
	tests := [][]byte{[]byte("changed\n"), []byte{'a', 0, 'b'}, []byte{0xff}}
	for _, input := range tests {
		if _, e := CheckAndMask(input, g); e == nil {
			t.Fatal("changed input accepted")
		}
	}
	g.Members[0].Blob = strings.Repeat("0", 40)
	if _, e := CheckAndMask(raw, g); e == nil {
		t.Fatal("changed Git object accepted")
	}
}
func TestFullyWithheldBodyIsNotReportedAsRawOrMeaningComplete(t *testing.T) {
	raw := []byte("password: dummy\napi_key: example\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Withheld) != v.Lines || v.NewRawReadCredit != 0 || !v.UnseenMeaningsUnknown {
		t.Fatal("allmasked promoted")
	}
}
func TestDisjointPagesRetainEmptyPhysicalLineAndDoNotExposeOtherRanges(t *testing.T) {
	raw := []byte("one\n\nthree\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Section(v, 0, 2); e == nil {
		t.Fatal("zero-based range accepted")
	}
	s, e := Section(v, 2, 2)
	if e != nil {
		t.Fatal(e)
	}
	m := s.(map[string]any)
	if m["masked_range"] != "\n" || m["first"] != 2 || m["through"] != 2 {
		t.Fatal("empty line skipped or range expanded")
	}
	if _, e = Section(v, 4, 5); e == nil {
		t.Fatal("outside body accepted")
	}
	if _, e = Section(v, 1, 181); e == nil {
		t.Fatal("display window silently expanded")
	}
}

func TestMaskCoversSpaceSeparatedCredentialLabelsAndBareUUID(t *testing.T) {
	raw := []byte("API key: synthetic-example\nAccess token: [REDACTED]\n11111111-2222-4333-8444-555555555555\nvisible explanatory sentence\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Withheld) != 3 || strings.Contains(v.Visible, "synthetic-example") || strings.Contains(v.Visible, "11111111") || !strings.Contains(v.Visible, "visible explanatory sentence") {
		t.Fatal("common credential label or unlabelled opaque ID was displayed")
	}
}
func TestBOMDoesNotExposeUnlabelledFrontmatter(t *testing.T) {
	raw := []byte("\ufeff---\nname: ordinary\ncustom_field: synthetic-frontmatter\n---\nvisible body\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Withheld) != 4 || strings.Contains(v.Visible, "synthetic-frontmatter") || !strings.Contains(v.Visible, "visible body") {
		t.Fatal("BOM bypassed whole-frontmatter mask")
	}
}

func TestPGPPrivateBlockUNCAndCompactOpaqueValuesAreWithheld(t *testing.T) {
	raw := []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\nshort private fixture material\n-----END PGP PRIVATE KEY BLOCK-----\n\\\\fixture-host\\share\\user\\file\n0123456789abcdef0123456789abcdef\nvisible final paragraph\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Withheld) != 5 || strings.Contains(v.Visible, "short private fixture") || strings.Contains(v.Visible, "fixture-host") || strings.Contains(v.Visible, "0123456789abcdef") || !strings.Contains(v.Visible, "visible final paragraph") {
		t.Fatal("PGP/UNC/compact opaque material escaped its whole-line mask")
	}
}

func TestOpaqueValueLexingIncludesPunctuationSeparators(t *testing.T) {
	raw := []byte("abcdefghijklmnop.qrstuvwx\nabcdefghijklmnop:qrstuvwx\nabcdefgh!ijklmnop?qrstuvwx\nshort ordinary words remain visible\n")
	v, e := CheckAndMask(raw, maskedGroup(raw))
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Withheld) != 3 || strings.Contains(v.Visible, "abcdefghijklmnop") || !strings.Contains(v.Visible, "short ordinary words remain visible") {
		t.Fatal("punctuation split an opaque value into falsely visible fragments")
	}
}

func TestOpaqueValueExact23And24CharacterBoundary(t *testing.T) {
	short := "abcdefghijk.lmnopqrstuv"
	long := short + "x"
	if len(short) != 23 || len(long) != 24 {
		t.Fatal("boundary fixture lengths differ")
	}
	v, e := CheckAndMask([]byte(short+"\n"+long+"\n"), maskedGroup([]byte(short+"\n"+long+"\n")))
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Withheld) != 1 || v.Withheld[0] != 2 || v.Visible != short+"\n[WITHHELD physical line 2]\n" {
		t.Fatal("opaque boundary hid the short value or exposed the 24-character value")
	}
}
