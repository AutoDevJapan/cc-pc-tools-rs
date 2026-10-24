package main

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"

	contract "cc/civ6-ci-contract"
)

// No SHA, manifest, parent list or authorization is accepted from the caller.
func inspectMain(ctx context.Context, request api) error {
	var r struct {
		ID          int64
		FullName    string `json:"full_name"`
		Fork        bool
		Owner       struct{ Login string }
		Permissions struct{ Admin bool }
	}
	if err := request(ctx, "GET", "repositories/1392152644", nil, &r); err != nil {
		return err
	}
	if r.ID != contract.RepoID || r.FullName != contract.Repository || r.Fork || r.Owner.Login != "AutoDevJapan" || !r.Permissions.Admin {
		return errors.New("fixed merged source owner differs")
	}
	type repo struct {
		ID       int64
		FullName string `json:"full_name"`
		Fork     bool
	}
	var p struct {
		Number int
		State  string
		Merged bool
		Head   struct {
			SHA, Ref string
			Repo     repo
		}
		Base struct {
			SHA, Ref string
			Repo     repo
		}
	}
	if err := request(ctx, "GET", apiPrefix+"/pulls/149", nil, &p); err != nil {
		return err
	}
	if p.Number != 149 || p.State != "open" || p.Merged || p.Head.SHA != contract.OldRoutedHead || p.Head.Ref != contract.OriginalBranch ||
		(p.Base.SHA != contract.OriginalBase && p.Base.SHA != contract.MergeMain) || p.Base.Ref != "main" ||
		p.Head.Repo.ID != contract.RepoID || p.Base.Repo.ID != contract.RepoID || p.Head.Repo.FullName != contract.Repository || p.Base.Repo.FullName != contract.Repository || p.Head.Repo.Fork || p.Base.Repo.Fork {
		return errors.New("fixed old PR tip/base/repository changed")
	}
	for branch, sha := range map[string]string{contract.OriginalBranch: contract.OldRoutedHead, "main": contract.MergeMain} {
		var ref struct{ Object struct{ SHA string } }
		if err := request(ctx, "GET", apiPrefix+"/git/ref/heads/"+branch, nil, &ref); err != nil {
			return err
		}
		if ref.Object.SHA != sha {
			return errors.New("fixed main or old tip advanced; retain evidence without following")
		}
	}
	read := func(c context.Context, path string, out any) error { return request(c, "GET", path, nil, out) }
	if err := contract.VerifyOldRoutedSource(ctx, read); err != nil {
		return err
	}
	var main commit
	if err := request(ctx, "GET", apiPrefix+"/git/commits/"+contract.MergeMain, nil, &main); err != nil {
		return err
	}
	if main.SHA != contract.MergeMain || main.Tree.SHA != contract.MergeMainTree {
		return errors.New("fixed main commit/tree changed")
	}
	return nil
}

func proposalMain(ctx context.Context, request api) (manifest, map[string][]byte, error) {
	m := manifest{Generation: contract.MergeGeneration, RepoID: contract.RepoID, Head: contract.OldRoutedHead, Tree: contract.OldRoutedTree,
		Base: contract.MergeMain, BaselineTree: contract.MergeMainTree, Parents: []string{contract.OldRoutedHead, contract.MergeMain}}
	if err := inspectMain(ctx, request); err != nil {
		return m, nil, err
	}
	files := map[string][]byte{}
	for _, w := range contract.MainWorkflows() {
		var v struct{ Type, Encoding, Content, SHA string }
		if err := request(ctx, "GET", apiPrefix+"/contents/"+w.Path+"?ref="+contract.MergeMain, nil, &v); err != nil {
			return m, nil, err
		}
		if v.Type != "file" || v.Encoding != "base64" || len(v.Content) > 2<<20 {
			return m, nil, errors.New("main workflow response differs")
		}
		b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
		if err != nil || len(b) > 1<<20 || blobID(b) != v.SHA {
			return m, nil, errors.New("main workflow Git blob differs")
		}
		next, err := contract.ProposeMain(b, w)
		if err != nil {
			return m, nil, err
		}
		files[w.Path] = next
		m.Files = append(m.Files, filePin{w.Path, contract.Digest(b), contract.Digest(next), blobID(next)})
	}
	return m, files, nil
}

func prepareMain(ctx context.Context, dir string, request api) error {
	m, files, err := proposalMain(ctx, request)
	if err != nil {
		return err
	}
	for _, pin := range m.Files {
		if err = saveBytes(filepath.Join(dir, filepath.Base(pin.Path)+".candidate"), files[pin.Path]); err != nil {
			return err
		}
	}
	return save(filepath.Join(dir, "proposal.private.json"), m)
}
