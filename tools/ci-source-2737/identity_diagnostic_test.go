package sourcepacket

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSourceIdentityDiagnosticsPreserveAllRejections(t *testing.T) {
	pin := Pin{HeadSHA: strings.Repeat("a", 40)}
	r := repo{ID: RepoID, FullName: "AutoDevJapan/GoDD-MCP-Alpha", Private: true}
	r.Owner.Login, r.Permissions.Admin = "AutoDevJapan", true
	p := pull{State: "open"}
	p.Head.SHA, p.Head.Ref, p.Head.Repo, p.Base.Repo = pin.HeadSHA, HeadRef, r, r
	ref := identityRef{}
	ref.Object.SHA = pin.HeadSHA
	tests := []struct {
		name, want string
		stop       int
		change     func(*repo, *pull, *identityRef)
	}{
		{"accept", "", 3, func(*repo, *pull, *identityRef) {}},
		{"id", "repository_identity", 1, func(r *repo, _ *pull, _ *identityRef) { r.ID++ }},
		{"owner", "repository_identity", 1, func(r *repo, _ *pull, _ *identityRef) { r.Owner.Login = "other" }},
		{"name", "repository_identity", 1, func(r *repo, _ *pull, _ *identityRef) { r.FullName += "other" }},
		{"public", "repository_identity", 1, func(r *repo, _ *pull, _ *identityRef) { r.Private = false }},
		{"fork", "repository_identity", 1, func(r *repo, _ *pull, _ *identityRef) { r.Fork = true }},
		{"admin", "repository_identity", 1, func(r *repo, _ *pull, _ *identityRef) { r.Permissions.Admin = false }},
		{"state", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.State = "closed" }},
		{"head", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Head.SHA = strings.Repeat("b", 40) }},
		{"ref", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Head.Ref += "other" }},
		{"head_repo", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Head.Repo.ID++ }},
		{"base_repo", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Base.Repo.ID++ }},
		{"head_name", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Head.Repo.FullName += "other" }},
		{"base_name", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Base.Repo.FullName += "other" }},
		{"head_fork", "pull_identity", 2, func(_ *repo, p *pull, _ *identityRef) { p.Head.Repo.Fork = true }},
		{"ref_sha", "ref_identity", 3, func(_ *repo, _ *pull, r *identityRef) { r.Object.SHA = strings.Repeat("b", 40) }},
	}
	endpoints := []string{fmt.Sprintf("repositories/%d", RepoID), "repos/" + r.FullName + "/pulls/3613", "repos/" + r.FullName + "/git/ref/heads/" + url.PathEscape(HeadRef)}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, p, ref := r, p, ref
			tc.change(&r, &p, &ref)
			calls := 0
			got, detail, err := verifySourceIdentity(context.Background(), pin, func(_ context.Context, ep string, dst any) error {
				if calls >= 3 || ep != endpoints[calls] {
					t.Fatal("unexpected API read")
				}
				calls++
				switch dst := dst.(type) {
				case *repo:
					*dst = r
				case *pull:
					*dst = p
				case *identityRef:
					*dst = ref
				default:
					t.Fatal("unexpected decode target")
				}
				return nil
			})
			if calls != tc.stop || detail != tc.want || (err == nil) != (tc.want == "") {
				t.Fatal("rejection or read boundary changed")
			}
			if err == nil && got.ID != RepoID || err != nil && got.ID != 0 {
				t.Fatal("identity result not cleared on rejection")
			}
		})
	}
	for step, name := range []string{"repository", "pull", "ref"} {
		for _, code := range []string{"start", "context", "drain", "exit", "json", "capture", "denied", "transport", "operation"} {
			t.Run(name+"_"+code, func(t *testing.T) {
				calls := 0
				_, detail, err := verifySourceIdentity(context.Background(), pin, func(_ context.Context, _ string, dst any) error {
					calls++
					if calls == step+1 {
						if code == "operation" {
							return errors.New("PRIVATE endpoint response must never escape")
						}
						return fmt.Errorf("PRIVATE: %w", &sourceAPIError{code})
					}
					switch dst := dst.(type) {
					case *repo:
						*dst = r
					case *pull:
						*dst = p
					}
					return nil
				})
				if calls != step+1 || detail != name+"_api_"+code || err == nil || err.Error() != "source packet rejected" {
					t.Fatal("API failure leaked, mislabeled, or continued")
				}
			})
		}
	}
	for _, err := range []error{nil, (*sourceAPIError)(nil), &sourceAPIError{"PRIVATE"}, errors.New("PRIVATE")} {
		if sourceAPICode(err) != "operation" {
			t.Fatal("non-vocabulary detail escaped")
		}
	}
}

func TestSourceAPIClosedExitCategories(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"PRIVATE TLS handshake timeout", "transport"},
		{"PRIVATE HTTP 503", "transport"},
		{"PRIVATE rejected; unexpected EOF", "denied"},
		{"PRIVATE HTTP 401; HTTP 503", "denied"},
		{"PRIVATE gh auth login", "denied"},
		{"PRIVATE arbitrary body and path", "exit"},
	} {
		if sourceExitCode(tc.text) != tc.want {
			t.Fatal("exit category mismatch")
		}
	}
	b := bounded{limit: 4}
	if _, err := b.Write([]byte("1234")); err != nil || b.overflow {
		t.Fatal("complete bound rejected")
	}
	if _, err := b.Write([]byte("PRIVATE")); err == nil || !b.overflow || string(b.Bytes()) != "1234" {
		t.Fatal("incomplete capture was not recorded")
	}
}
