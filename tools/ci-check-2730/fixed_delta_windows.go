package main

import (
	"context"
	"errors"
)

const fixedSHA = "75a4051305b6f47c70ddb59a8d2bf9015666f12c"
const fixedTree = "cecb9cd535d135a1eb4407882db532c325f42860"
const parentSHA = "74f023c0ee4b460f05f87dc3db5ceaf75e999a8a"
const changedPath = "tools/civ6-autotest-rs/src/runner.rs"

// Keep the single-file proposal binding as well as the original identity checks.
func verifyDelta(ctx context.Context, repo string, read api) error {
	var commit struct {
		SHA     string
		Tree    struct{ SHA string }
		Parents []struct{ SHA string }
	}
	if e := read(ctx, "repos/"+repo+"/git/commits/"+fixedSHA, &commit); e != nil {
		return e
	}
	if commit.SHA != fixedSHA || commit.Tree.SHA != fixedTree || len(commit.Parents) != 1 || commit.Parents[0].SHA != parentSHA {
		return errors.New("fixed commit tree/parent rejected")
	}
	var comparison struct {
		Status   string
		AheadBy  int `json:"ahead_by"`
		BehindBy int `json:"behind_by"`
		Files    []struct{ Filename, Status, SHA string }
	}
	if e := read(ctx, "repos/"+repo+"/compare/"+parentSHA+"..."+fixedSHA, &comparison); e != nil {
		return e
	}
	if comparison.Status != "ahead" || comparison.AheadBy != 1 || comparison.BehindBy != 0 || len(comparison.Files) != 1 {
		return errors.New("single-file fixed delta rejected")
	}
	f := comparison.Files[0]
	if f.Filename != changedPath || f.Status != "modified" || f.SHA != "cf3398189ba7fb6cf96ce152903d4cdaa6387c50" {
		return errors.New("proposal blob rejected")
	}
	return nil
}
