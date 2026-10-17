package main

import (
	"errors"
	"fmt"
)

// Collection follows the protected local launcher/SCM drain proof. It must
// never certify CI success, or retain a stopped owned service just because the
// immutable run is terminal/failed or its PR/branch has subsequently moved.
func checkCollectionRemote(p Plan, read readAPI) error {
	if p.validate() != nil || p.Fixture != "" {
		return errors.New("real collection identity required")
	}
	var repo struct {
		ID            int64
		FullName      string `json:"full_name"`
		Private, Fork bool
		Owner         struct{ Login string }
		Permissions   struct{ Admin bool }
	}
	if e := read("repos/"+repository, &repo); e != nil {
		return e
	}
	if repo.ID != repositoryID || repo.FullName != repository || repo.Owner.Login != "AutoDevJapan" || !repo.Private || repo.Fork || !repo.Permissions.Admin {
		return errors.New("collection repository identity rejected")
	}
	var run struct {
		ID          int64
		Attempt     int    `json:"run_attempt"`
		HeadSHA     string `json:"head_sha"`
		Event, Path string
		HeadBranch  string `json:"head_branch"`
		Repository  struct {
			ID       int64
			FullName string `json:"full_name"`
		}
		HeadRepository struct {
			ID       int64
			FullName string `json:"full_name"`
			Fork     bool
		} `json:"head_repository"`
	}
	if e := read(fmt.Sprintf("repos/%s/actions/runs/%d", repository, p.RunID), &run); e != nil {
		return e
	}
	if run.ID != p.RunID || run.Attempt != p.Attempt || run.HeadSHA != p.Head || run.Event != "workflow_dispatch" || run.Path != p.Workflow || run.HeadBranch != branch || run.Repository.ID != repositoryID || run.Repository.FullName != repository || run.HeadRepository.ID != repositoryID || run.HeadRepository.FullName != repository || run.HeadRepository.Fork {
		return errors.New("collection run identity rejected")
	}
	var runners struct {
		Total   int `json:"total_count"`
		Runners []struct {
			ID   int64
			Name string
		}
	}
	if e := read("repos/"+repository+"/actions/runners?per_page=100", &runners); e != nil {
		return e
	}
	if runners.Total != 0 || len(runners.Runners) != 0 {
		return errors.New("remote registration remains; retain credential runtime")
	}
	return nil
}
