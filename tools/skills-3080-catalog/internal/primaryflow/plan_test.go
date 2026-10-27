package primaryflow

import (
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
