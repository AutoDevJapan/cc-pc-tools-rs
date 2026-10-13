package main

import (
	"errors"
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
func TestUnknownAndSiblingProductPathsStayProtected(t *testing.T) {
	for _, path := range []string{gameTree + "-other", filepath.Join(filepath.Dir(videoTree), "2026-10-15"), filepath.Join(t.TempDir(), "unknown")} {
		if productPathAllowed(path) {
			t.Fatal("unrequested product path accepted")
		}
	}
}
