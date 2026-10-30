package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall"
	"testing"
)

func TestCI3888RuntimeRechecksCurrentMetadataAfterGitHub(t *testing.T) {
	for _, change := range []string{"assignment", "owner-stop", "parent-stop", "creator-stop", "current-job", "original-job", "stdout", "issuer"} {
		t.Run(change, func(t *testing.T) {
			a, reg, jobs, _, caller := ci3888RuntimeFixture()
			afterGitHub := false
			read := a.read
			a.read = func(path string, limit int64) ([]byte, bool) {
				if afterGitHub && ((change == "stdout" && path == ci3888RuntimeCreationOutput) || (change == "issuer" && path == ci3888RuntimeIssuerRecord)) {
					return []byte("replaced while the external query ran"), true
				}
				return read(path, limit)
			}
			a.github = func(context.Context) error {
				afterGitHub = true
				switch change {
				case "assignment":
					row := reg.Tasks[3888]
					row.Scheduler.Job = "20261028-000002-todo-3888"
					reg.Tasks[3888] = row
				case "owner-stop", "parent-stop", "creator-stop":
					id := 3888
					if change == "parent-stop" {
						id = 3387
					}
					if change == "creator-stop" {
						id = 3894
					}
					row := reg.Tasks[id]
					row.OwnerStopped = true
					reg.Tasks[id] = row
				case "current-job":
					jobs[caller]["scope_source"] = "caller"
				case "original-job":
					jobs[official3894OriginalCreator]["scope_source"] = "caller"
				}
				return nil
			}
			if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err == nil {
				t.Fatal("changed current assignment/stop/origin/ownership accepted")
			}
		})
	}
}

func TestCI3888RuntimeEveryAncestorIdentityAndTempOwner(t *testing.T) {
	_, _, _, r, _ := ci3888RuntimeFixture()
	for i, ancestor := range r.Ancestors {
		for _, field := range []string{"volume", "id", "birth", "reparse", "not-directory", "io", "temp-owner"} {
			if field == "temp-owner" && i != 0 {
				continue
			}
			t.Run(fmt.Sprintf("ancestor%d/%s", i, field), func(t *testing.T) {
				a, _, _, _, caller := ci3888RuntimeFixture()
				original := a.identity
				a.identity = func(path string) (audio2906Metadata, error) {
					m, err := original(path)
					if same(path, ancestor.Path) {
						switch field {
						case "volume":
							m.Volume++
						case "id":
							m.ID++
						case "birth":
							m.Created++
						case "reparse":
							m.Attributes |= syscall.FILE_ATTRIBUTE_REPARSE_POINT
						case "not-directory":
							m.Attributes &^= syscall.FILE_ATTRIBUTE_DIRECTORY
						case "io":
							err = syscall.ERROR_ACCESS_DENIED
						case "temp-owner":
							m.Owner = "S-1-5-18"
						}
					}
					return m, err
				}
				if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err == nil {
					t.Fatal("changed ancestor identity or Temp owner accepted")
				}
			})
		}
	}
}

func TestCI3888RuntimeActorFieldsAndCompletedProducer(t *testing.T) {
	for _, subject := range []string{"actor", "producer"} {
		for _, field := range []string{"task_type", "project", "cwd"} {
			t.Run(subject+"/"+field, func(t *testing.T) {
				a, _, jobs, _, caller := ci3888RuntimeFixture()
				id := caller
				if subject == "producer" {
					id = ci3888RuntimeProducer
				}
				jobs[id][field] = "different"
				if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err == nil {
					t.Fatal("foreign job metadata accepted")
				}
			})
		}
	}
	a, _, jobs, _, caller := ci3888RuntimeFixture()
	jobs[ci3888RuntimeProducer]["status"] = "succeeded"
	if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err != nil {
		t.Fatal("normal completed producer rejected", err)
	}
}

func TestCI3888RuntimeForeignExistingActor(t *testing.T) {
	a, reg, jobs, _, caller := ci3888RuntimeFixture()
	foreign := "20261028-000001-todo-4143"
	job := jobs[caller]
	job["id"], job["todo_id"] = foreign, json.Number("4143")
	jobs[foreign] = job
	row := reg.Tasks[3888]
	row.Scheduler.Job = foreign
	row.Jobs = append(row.Jobs, foreign)
	reg.Tasks[3888] = row
	if ci3888RuntimeActorBound(reg, foreign, job, jobs[ci3888RuntimeProducer], "D:/synthetic/.git") {
		t.Fatal("existing assigned foreign actor admitted")
	}
	if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, foreign, a); err == nil {
		t.Fatal("foreign actor reached purpose acceptance")
	}
}
