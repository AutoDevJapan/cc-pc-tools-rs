//go:build windows

package main

import (
	"reflect"
	"testing"
)

func TestCI3574ChecksTheActualCompletedRunJobAndRemovedRegistration(t *testing.T) {
	p := ci3574Resources[0]
	run := ci3574Run{ID: p.run, Head: p.head, Status: "completed", Conclusion: "success"}
	run.Repository.ID = 1429803672
	job := ci3574Run{ID: p.job, RunID: p.run, Head: p.head, Name: "acceptance", Status: "completed", Conclusion: "success"}
	if !ci3574OfficialValid(p, run, job, []int64{999}) {
		t.Fatal("completed original resource rejected because an unrelated runner exists")
	}
	for _, kind := range []string{"run_id", "run_head", "run_status", "run_result", "repo", "job_id", "job_run", "job_head", "job_status", "job_result", "job_name", "runner40", "runner41", "runner42", "runner43"} {
		t.Run(kind, func(t *testing.T) {
			r, j := run, job
			ids := []int64{}
			switch kind {
			case "run_id":
				r.ID++
			case "run_head":
				r.Head = "other"
			case "run_status":
				r.Status = "in_progress"
			case "run_result":
				r.Conclusion = "failure"
			case "repo":
				r.Repository.ID++
			case "job_id":
				j.ID++
			case "job_run":
				j.RunID++
			case "job_head":
				j.Head = "other"
			case "job_status":
				j.Status = "queued"
			case "job_result":
				j.Conclusion = "cancelled"
			case "job_name":
				j.Name = "other"
			case "runner40":
				ids = []int64{40}
			case "runner41":
				ids = []int64{41}
			case "runner42":
				ids = []int64{42}
			case "runner43":
				ids = []int64{43}
			}
			if ci3574OfficialValid(p, r, j, ids) {
				t.Fatal("unfinished or different CI source accepted")
			}
		})
	}
}

func TestCI3574DoesNotAdmitOtherOwnedResourcesOrStores(t *testing.T) {
	for _, id := range []string{"", ci3174ID, console3451ID, geminiID, "00000000000000000000000000000000"} {
		if _, ok := ci3574ResourceByID(id); ok {
			t.Fatal("unrelated original resource admitted")
		}
	}
	for _, p := range ci3574Resources {
		c := config{Store: t.TempDir(), Quarantine: t.TempDir()}
		if scope, err := c.ci3574Recovery(p.id); err == nil || scope != nil {
			t.Fatal("an arbitrary local receipt store acquired the production scope")
		}
	}
}

func TestCI3574UsesExistingOAuthWithoutTokenOverrides(t *testing.T) {
	input := []string{"USERPROFILE=C:/fixture", "PATH=fixture", "GH_TOKEN=fixture", "github_token=fixture", "GH_ENTERPRISE_TOKEN=fixture", "GITHUB_ENTERPRISE_TOKEN=fixture", "GH_HOST=other"}
	if !reflect.DeepEqual(ci3574OAuthEnvironment(input), input[:2]) {
		t.Fatal("OAuth environment or token filtering changed")
	}
	gitEnv := []string{"USERPROFILE=C:/fixture", "PATH=fixture", "GIT_DIR=other", "git_work_tree=other", "GIT_CONFIG_COUNT=1"}
	if !reflect.DeepEqual(consoleCIGitEnvironment(gitEnv), []string{"USERPROFILE=C:/fixture", "PATH=fixture", "GIT_NO_LAZY_FETCH=1"}) {
		t.Fatal("Git inspection can be redirected to another repository")
	}
}
