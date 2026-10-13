package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFiniteProductScopeExpiry(t *testing.T) {
	for _, c := range []struct {
		date    string
		allowed bool
	}{{"2026-10-12", false}, {"2026-10-13", true}, {"2026-10-20", true}, {"2026-10-21", false}} {
		at, _ := time.Parse("2006-01-02", c.date)
		if finiteProductScope(at) != c.allowed {
			t.Fatal("scope boundary differs")
		}
	}
}

func videoFixture() (object, object, productIO) {
	request := "D:/CC/Tools/todo/specs/2475-9c08c1c10273475f87e003ca75cb4629.md"
	item := object{"id": float64(2475), "project": "youtube-daily", "status": "doing", "spec": request, "jobs": []any{videoJob}}
	db := object{"projects": object{"youtube-daily": "D:/CC/Tools/youtube-daily"}, "items": []any{item}}
	job := object{"id": videoJob, "status": "succeeded", "thread_id": videoSession, "cwd": videoTree, "prompt_source": filepath.Join(videoTree, "prompt.md")}
	access := productIO{
		readObject: func(path string, limit int64) (object, error) {
			if !same(path, filepath.Join("D:/CC/Tools/codex-jobs/jobs", videoJob, "job.json")) {
				return nil, errors.New("unexpected access")
			}
			return job, nil
		},
		read: func(path string, limit int64) ([]byte, error) {
			if !same(path, request) {
				return nil, errors.New("unexpected access")
			}
			return []byte(videoTree), nil
		},
		physical: func(string) bool { return true }, origin: func(string, string) bool { return false },
		glob: func(string) ([]string, error) { return []string{"original-session-file"}, nil },
		at:   time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC),
	}
	return db, job, access
}
func TestOriginalReceiptQualificationAndMismatchDoNotGrantOtherScopes(t *testing.T) {
	db, _, access := videoFixture()
	if r := verifyProductWith(db, 2475, access); r["verified"] != true || r["session"] != videoSession {
		t.Fatal("original receipt rejected")
	}
	for _, field := range []string{"id", "status", "thread_id", "cwd", "prompt_source"} {
		db, job, access := videoFixture()
		job[field] = "different"
		if verifyProductWith(db, 2475, access)["verified"] == true {
			t.Fatal("worker mismatch accepted")
		}
	}
	for _, condition := range []string{"path", "spec", "session", "expired", "item_link", "project_registration"} {
		db, _, access := videoFixture()
		switch condition {
		case "path":
			access.physical = func(string) bool { return false }
		case "spec":
			access.read = func(string, int64) ([]byte, error) { return []byte("unrelated specification"), nil }
		case "session":
			access.glob = func(string) ([]string, error) { return nil, nil }
		case "expired":
			access.at = access.at.AddDate(0, 0, 9)
		case "item_link":
			itemAt(db, 2475)["jobs"] = []any{"unrelated"}
		case "project_registration":
			obj(db["projects"])["youtube-daily"] = "unrelated"
		}
		if verifyProductWith(db, 2475, access)["verified"] == true {
			t.Fatal("unverified original scope accepted")
		}
	}
}
func TestDuplicateOrFractionalOriginalIdsNeverSelectARecord(t *testing.T) {
	for _, rows := range [][]any{{object{"id": float64(1626)}, object{"id": float64(1626)}}, {object{"id": 1626.1}}} {
		if itemAt(object{"items": rows}, 1626) != nil {
			t.Fatal("ambiguous original selected")
		}
	}
}

func TestOnlyNamedOriginalSavedSpecNamesCanReachVerification(t *testing.T) {
	for _, c := range []struct {
		name  string
		valid bool
	}{
		{"1626-f72998fdd0e74aaa90e628e72f37a12b.md", true},
		{"2407-00000000000000000000000000000000.md", true},
		{"2475-9c08c1c10273475f87e003ca75cb4629.md", true},
		{"17-f72998fdd0e74aaa90e628e72f37a12b.md", false},
		{"1626-unknown.md", false},
	} {
		if savedSpecName.MatchString(c.name) != c.valid {
			t.Fatal("unrequested spec filename reached verification")
		}
	}
}

func TestOriginalReceiptAuthorizationSurvivesNormalSpecEditOnlyWithHistory(t *testing.T) {
	original := `D:\CC\Tools\todo\specs\2530-original.md`
	if !specRetained(object{"spec": original}, original) {
		t.Fatal("original spec rejected")
	}
	if !specRetained(object{"spec": "new", "spec_history": []any{object{"spec": original}}}, original) {
		t.Fatal("retained original spec rejected")
	}
	if specRetained(object{"spec": "new", "spec_history": []any{object{"spec": "unrelated"}}}, original) {
		t.Fatal("unrelated history authorized outputs")
	}
}
func TestUnknownAndSiblingProductPathsStayProtected(t *testing.T) {
	for _, path := range []string{gameTree + "-other", filepath.Join(filepath.Dir(videoTree), "2026-10-15"), filepath.Join(t.TempDir(), "unknown")} {
		if productPathAllowed(path) {
			t.Fatal("unrequested product path accepted")
		}
	}
}

func TestPlannedFilesRejectAnExistingEscapingOrBrokenLink(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !physicalOrPlannedWithin(root, filepath.Join(root, "new", "receipt.json")) {
		t.Fatal("bounded new file rejected")
	}
	if physicalOrPlannedWithin(root, root+"-sibling") {
		t.Fatal("sibling accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	if physicalOrPlannedWithin(root, filepath.Join(link, "new-file")) {
		t.Fatal("escaping link accepted")
	}
	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(outside, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	if physicalOrPlannedWithin(root, filepath.Join(broken, "new-file")) {
		t.Fatal("broken link accepted")
	}
}
