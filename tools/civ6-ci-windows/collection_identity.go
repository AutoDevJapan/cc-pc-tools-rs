package main

import (
	contract "cc/civ6-ci-contract"
	"errors"
	"fmt"
)

// Collection follows the protected local launcher/SCM drain proof. It must
// never certify CI success, or retain a stopped owned service just because the
// immutable run is terminal/failed or its PR/branch has subsequently moved.
// Only the immutable run identity and the absence of this plan's own unique
// registration are required; other repository runners are not examined.
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
	if run.ID != p.RunID || run.Attempt != p.Attempt || run.HeadSHA != p.Head || run.Event != p.Event || !contract.MatchRunPath(p.Plan, run.Path) || run.HeadBranch != p.Branch || run.Repository.ID != repositoryID || run.Repository.FullName != repository || run.HeadRepository.ID != repositoryID || run.HeadRepository.FullName != repository || run.HeadRepository.Fork {
		return errors.New("collection run identity rejected")
	}
	own, e := ownRunner(p, read)
	if e != nil {
		return e
	}
	if own != nil {
		return errors.New("unique remote registration remains; retain credential runtime")
	}
	return nil
}
