package primaryflow

import (
	"fmt"
	"strings"
	"testing"
)

func memberForTest(id, path, sha, blob string) Member {
	return Member{ID: id, Repo: "6mile/skills", Commit: Commit, Path: path, SHA: sha, Blob: blob, Bytes: 17, Content: "github/6mile__skills/objects/" + sha + ".gz"}
}
func TestPlanConservesAllOccurrencesAndDefersProtectedBodies(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	blob := strings.Repeat("1", 40)
	rows := []Member{memberForTest("github:a", "skills/x/a/SKILL.md", a, blob), memberForTest("github:b", "skills/y/a/SKILL.md", a, blob), memberForTest("github:c", "skills/z/b/SKILL.md", b, blob)}
	plans, deferred, e := Select(rows, nil, map[string]bool{b: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(deferred) != 1 || deferred[0].ID != "github:c" {
		t.Fatalf("protected source reread risk: %+v", deferred)
	}
	groups, n := 0, 0
	seen := map[string]bool{}
	for _, p := range plans {
		for _, g := range p.Groups {
			groups++
			for _, m := range g.Members {
				if seen[m.ID] {
					t.Fatal("duplicate occurrence")
				}
				seen[m.ID] = true
				n++
			}
		}
	}
	if groups != 1 || n != 2 || !seen["github:a"] || !seen["github:b"] {
		t.Fatalf("lost independent occurrences groups%d n%d", groups, n)
	}
}
func TestPlanRejectsConflictingIdentityAndSourceTraversal(t *testing.T) {
	sha, blob := strings.Repeat("c", 64), strings.Repeat("1", 40)
	normal := memberForTest("github:a", "skills/user/name/SKILL.md", sha, blob)
	cases := []struct {
		name string
		rows []Member
	}{{"traversal", []Member{memberForTest("github:a", "skills/user/../private/SKILL.md", sha, blob)}}, {"conflicting blob", []Member{normal, memberForTest("github:b", "skills/other/name/SKILL.md", sha, strings.Repeat("2", 40))}}, {"duplicate identity", []Member{normal, normal}}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, e := Select(c.rows, nil, nil); e == nil {
				t.Fatal("accepted invalid source identity")
			}
		})
	}
}
func TestPlansHaveDeterministicMembershipRegardlessOfInputOrder(t *testing.T) {
	sha, blob := strings.Repeat("d", 64), strings.Repeat("1", 40)
	a, b := memberForTest("github:a", "skills/u/a/SKILL.md", sha, blob), memberForTest("github:b", "skills/v/b/SKILL.md", sha, blob)
	x, _, e := Select([]Member{b, a}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	y, _, e := Select([]Member{a, b}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	for i := range x {
		if len(x[i].Groups) != len(y[i].Groups) {
			t.Fatal("non-deterministic grouping")
		}
		for j := range x[i].Groups {
			if x[i].Groups[j].Members[0].ID != y[i].Groups[j].Members[0].ID {
				t.Fatal("unstable representative")
			}
		}
	}
}

func TestNestedPackagePrimariesRemainInTheFinitePlan(t *testing.T) {
	sha, blob := strings.Repeat("e", 64), strings.Repeat("1", 40)
	r := memberForTest("github:nested", "skills/u/package/sub/nested/SKILL.md", sha, blob)
	plans, deferred, e := Select([]Member{r}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(deferred) != 0 || plans[0].Header.TotalOccurrences+plans[1].Header.TotalOccurrences != 1 {
		t.Fatal("nested actual primary was dropped")
	}
}

func TestCaseVariantSkillLeafRemainsASeparateSourceOccurrence(t *testing.T) {
	sha, blob := strings.Repeat("f", 64), strings.Repeat("1", 40)
	r := memberForTest("github:lower", "skills/u/long/sub/skill.md", sha, blob)
	plans, _, e := Select([]Member{r}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if plans[0].Header.TotalOccurrences+plans[1].Header.TotalOccurrences != 1 {
		t.Fatal("case variant omitted")
	}
}

func TestProtectedAndReservedRowsStillRequireValidConsistentDescriptors(t *testing.T) {
	sha, blob := strings.Repeat("a", 64), strings.Repeat("1", 40)
	a := memberForTest("github:a", "skills/u/name/SKILL.md", sha, blob)
	b := a
	b.ID = "github:b"
	b.Path = "skills/v/name/SKILL.md"
	for _, kind := range []string{"protected traversal", "protected blob conflict", "protected length conflict", "reserved namespace"} {
		t.Run(kind, func(t *testing.T) {
			x, y := a, b
			rows := []Member{x, y}
			reserved := map[string]bool{}
			protected := map[string]bool{sha: true}
			switch kind {
			case "protected traversal":
				rows[0].Path = "skills/u/../x/SKILL.md"
			case "protected blob conflict":
				rows[1].Blob = strings.Repeat("2", 40)
			case "protected length conflict":
				rows[1].Bytes++
			case "reserved namespace":
				rows[0].Repo = "other/source"
				reserved[x.ID] = true
			}
			if _, _, e := Select(rows, reserved, protected); e == nil {
				t.Fatal("defer hid an invalid source identity")
			}
		})
	}
}

func TestFixedPlanTotalsRejectMissingGroupOrOccurrenceBeforeWrite(t *testing.T) {
	sha, blob := strings.Repeat("a", 64), strings.Repeat("1", 40)
	a := memberForTest("github:a", "skills/u/name/SKILL.md", sha, blob)
	plans, _, e := Select([]Member{a}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	groups, counts := [2]int{1, 0}, [2]int{1, 0}
	if e = validateTotals(plans, groups, counts, 1, 1, 1, 1, 3); e != nil {
		t.Fatal(e)
	}
	plans[0].Groups = nil
	if e = validateTotals(plans, groups, counts, 1, 1, 1, 1, 3); e == nil {
		t.Fatal("missing group accepted")
	}
}
func TestMemberMetadataPagesPreserveEveryAliasTuple(t *testing.T) {
	g := Group{Ordinal: 1, SHA: strings.Repeat("a", 64)}
	for i := 0; i < 35; i++ {
		g.Members = append(g.Members, Member{ID: fmt.Sprintf("github:%d", i), Path: fmt.Sprintf("skills/u/%d/SKILL.md", i), Commit: Commit, Blob: strings.Repeat("1", 40)})
	}
	one, e := memberPage(g, 1, 32)
	if e != nil {
		t.Fatal(e)
	}
	two, e := memberPage(g, 33, 32)
	if e != nil {
		t.Fatal(e)
	}
	a, b := one.(map[string]any), two.(map[string]any)
	if len(a["members"].([]Member)) != 32 || len(b["members"].([]Member)) != 3 || b["members"].([]Member)[2].ID != "github:34" || a["whole_member_metadata_sha256"] != b["whole_member_metadata_sha256"] {
		t.Fatal("alias page lost or rebound original source tuples")
	}
}
