package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func official3894RegistryJSON(t *testing.T, edit func(int, string) string) string {
	t.Helper()
	reg := official3894LineageFixture()
	rows := []string{}
	for _, id := range []int{3387, 3888, 3894} {
		r := reg.Tasks[id]
		jobs, e := json.Marshal(r.Jobs)
		if e != nil {
			t.Fatal(e)
		}
		row := fmt.Sprintf(`{"id":%d,"project":%q,"status":%q,"jobs":%s,"scheduler":{"subwork_parent_job":""},"subwork":null}`, id, r.Project, r.Status, jobs)
		if edit != nil {
			row = edit(id, row)
		}
		rows = append(rows, row)
	}
	return fmt.Sprintf(`{"projects":{"cc-pc-tools-rs":%q},"items":[%s]}`, roots[3], strings.Join(rows, ","))
}

func TestOfficialCI3894CanonicalTaskMetadata(t *testing.T) {
	r, ok := official3894RegistryProjection(strings.NewReader(official3894RegistryJSON(t, nil)))
	if !ok || !official3894TaskLineage(r) {
		t.Fatal("normal independent registration JSON rejected")
	}
	for _, id := range []int{3888, 3894} {
		for name, replace := range map[string]func(string) string{
			"typed-subwork-erased": func(row string) string {
				return strings.Replace(row, `"subwork":null`, `"subwork":{"parent_todo_id":3387},"Subwork":{"parent_todo_id":0}`, 1)
			},
			"typed-kind-erased": func(row string) string {
				return strings.Replace(row, `"subwork":null`, `"subwork":{"kind":"check","KIND":""}`, 1)
			},
			"scheduler-parent-erased": func(row string) string {
				return strings.Replace(row, `"scheduler":{"subwork_parent_job":""}`, `"scheduler":{"subwork_parent_job":"typed-parent"},"Scheduler":{"subwork_parent_job":""}`, 1)
			},
			"admission-erased": func(row string) string {
				return strings.Replace(row, `"scheduler":{"subwork_parent_job":""}`, `"scheduler":{"subwork_admission":{"key":"typed"},"SUBWORK_ADMISSION":null}`, 1)
			},
			"owner-stop-erased": func(row string) string { return row[:len(row)-1] + `,"owner_stopped":true,"OWNER_STOPPED":false}` },
			"id-alias":          func(row string) string { return row[:len(row)-1] + fmt.Sprintf(`,"ID":%d}`, id) },
			"status-alias-only": func(row string) string { return strings.Replace(row, `"status":`, `"STATUS":`, 1) },
			"unicode-key-alias": func(row string) string {
				return strings.Replace(row, `"scheduler":{"subwork_parent_job":""}`, `"scheduler":{"key":"typed","Key":""}`, 1)
			},
		} {
			t.Run(fmt.Sprintf("%d/%s", id, name), func(t *testing.T) {
				raw := official3894RegistryJSON(t, func(which int, row string) string {
					if which == id {
						return replace(row)
					}
					return row
				})
				if r, ok := official3894RegistryProjection(strings.NewReader(raw)); ok {
					t.Fatalf("ambiguous JSON reached task authority (lineage=%v)", official3894TaskLineage(r))
				}
			})
		}
	}
}

func TestOfficialCI3894TaskWaitPresenceOnly(t *testing.T) {
	raw := official3894RegistryJSON(t, func(id int, row string) string {
		if id != 3894 {
			return row
		}
		return row[:len(row)-1] + `,"note":{"synthetic":"ignored original text"},"owner_question_wait":{"synthetic":"ignored question text"}}`
	})
	r, ok := official3894RegistryProjection(strings.NewReader(raw))
	if !ok || r.Tasks[3894].OwnerQuestion != true || !skillsHeld(r.Tasks[3894]) {
		t.Fatal("wait body must become presence metadata without losing its stop")
	}
	b, e := json.Marshal(r)
	if e != nil || strings.Contains(string(b), "ignored") || strings.Contains(string(b), "synthetic") {
		t.Fatal("original text retained in metadata projection")
	}
}

func TestOfficialCI3894TaskIdentityBeforeSelection(t *testing.T) {
	base := official3894RegistryJSON(t, nil)
	for _, extra := range []string{
		`{"id":3894,"ID":9999,"owner_stopped":true}`,
		`{"id":9999,"ID":3894}`,
		`{"id":3894,"id":9999}`,
		`{"id":null}`,
	} {
		raw := base[:len(base)-2] + "," + extra + "]}"
		if _, ok := official3894RegistryProjection(strings.NewReader(raw)); ok {
			t.Fatal("case-insensitive id selection hid an ambiguous or invalid task")
		}
	}
	for _, raw := range []string{
		strings.Replace(base, `"items":`, `"Items":[],"items":`, 1),
		strings.Replace(base, `"projects":`, `"Projects":{},"projects":`, 1),
	} {
		if _, ok := official3894RegistryProjection(strings.NewReader(raw)); ok {
			t.Fatal("ambiguous registry envelope accepted")
		}
	}
}
