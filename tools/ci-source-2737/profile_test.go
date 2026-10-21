package sourcepacket

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPressPhysicalSourceUsesExistingVerifier(t *testing.T) {
	for _, workflow := range []string{"local-checks.yml", "rust-v2-https.yml", "windows-hidden-fixtures.yml"} {
		t.Run(workflow, func(t *testing.T) {
			p, root := fixtureForWorkflow(t, ".github/workflows/"+workflow)
			p.Pin.Product, p.Pin.Workflow = PressProduct, workflow
			encoded, err := Encode(p)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(encoded, p.Pin)
			if err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(root, "press")
			proof, err := Materialize(context.Background(), decoded, p.Pin, dst)
			if err != nil || proof.RepoID != 1386702183 || proof.PR != 175 || proof.HeadSHA != p.Pin.HeadSHA || proof.TreeSHA != p.Pin.TreeSHA || !proof.PhysicalBlobsVerified || proof.BlobCount != 4 {
				t.Fatalf("physical Press proof rejected: %v", err)
			}
			if err = os.WriteFile(filepath.Join(dst, "tracked.dat"), []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dst); err == nil {
				t.Fatal("tampered Press blob accepted")
			}
			other := p.Pin
			other.Workflow = "rust-v2-https.yml"
			if workflow == other.Workflow {
				other.Workflow = "local-checks.yml"
			}
			if _, err = Decode(encoded, other); err == nil {
				t.Fatal("another workflow expectation accepted")
			}
		})
	}
}

func TestPressSourceIdentityAndEarlyRejection(t *testing.T) {
	pin := Pin{Product: PressProduct, Workflow: "local-checks.yml", HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64)}
	profile, ok := pin.Profile()
	if !ok || !validPin(pin) {
		t.Fatal("Press profile unavailable")
	}
	r := repo{ID: profile.RepoID, FullName: profile.Repository, Private: true}
	r.Owner.Login, r.Permissions.Admin = "AutoDevJapan", true
	p := pull{State: "open", Draft: true}
	p.Head.SHA, p.Head.Ref, p.Head.Repo, p.Base.Repo, p.Base.SHA = pin.HeadSHA, profile.HeadRef, r, r, PressBase
	ref := identityRef{}
	ref.Object.SHA = pin.HeadSHA
	for _, tc := range []struct {
		name      string
		wantCalls int
		change    func(*Pin, *repo, *pull, *identityRef)
	}{
		{"accepted", 3, func(*Pin, *repo, *pull, *identityRef) {}},
		{"unknown-product", 0, func(p *Pin, _ *repo, _ *pull, _ *identityRef) { p.Product = "arbitrary" }},
		{"unknown-workflow", 0, func(p *Pin, _ *repo, _ *pull, _ *identityRef) { p.Workflow = "codeql.yml" }},
		{"path-traversal", 0, func(p *Pin, _ *repo, _ *pull, _ *identityRef) { p.Workflow = "../local-checks.yml" }},
		{"workflow-without-product", 0, func(p *Pin, _ *repo, _ *pull, _ *identityRef) { p.Product = "" }},
		{"other-repo", 1, func(_ *Pin, r *repo, _ *pull, _ *identityRef) { r.ID = RepoID }},
		{"other-name", 1, func(_ *Pin, r *repo, _ *pull, _ *identityRef) { r.FullName = "AutoDevJapan/GoDD-MCP-Alpha" }},
		{"other-owner", 1, func(_ *Pin, r *repo, _ *pull, _ *identityRef) { r.Owner.Login = "other" }},
		{"public", 1, func(_ *Pin, r *repo, _ *pull, _ *identityRef) { r.Private = false }},
		{"fork", 1, func(_ *Pin, r *repo, _ *pull, _ *identityRef) { r.Fork = true }},
		{"not-admin", 1, func(_ *Pin, r *repo, _ *pull, _ *identityRef) { r.Permissions.Admin = false }},
		{"not-draft", 2, func(_ *Pin, _ *repo, p *pull, _ *identityRef) { p.Draft = false }},
		{"base-advanced", 2, func(_ *Pin, _ *repo, p *pull, _ *identityRef) { p.Base.SHA = strings.Repeat("d", 40) }},
		{"closed", 2, func(_ *Pin, _ *repo, p *pull, _ *identityRef) { p.State = "closed" }},
		{"head-advanced", 2, func(_ *Pin, _ *repo, p *pull, _ *identityRef) { p.Head.SHA = strings.Repeat("d", 40) }},
		{"head-fork", 2, func(_ *Pin, _ *repo, p *pull, _ *identityRef) { p.Head.Repo.Fork = true }},
		{"base-repo", 2, func(_ *Pin, _ *repo, p *pull, _ *identityRef) { p.Base.Repo.ID++ }},
		{"ref-advanced", 3, func(_ *Pin, _ *repo, _ *pull, r *identityRef) { r.Object.SHA = strings.Repeat("d", 40) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, rr, pp, rf := pin, r, p, ref
			tc.change(&q, &rr, &pp, &rf)
			calls := 0
			endpoints := []string{fmt.Sprintf("repositories/%d", profile.RepoID), "repos/" + profile.Repository + "/pulls/175", "repos/" + profile.Repository + "/git/ref/heads/" + url.PathEscape(profile.HeadRef)}
			_, detail, err := verifySourceIdentity(context.Background(), q, func(_ context.Context, ep string, dst any) error {
				if calls >= 3 || ep != endpoints[calls] {
					t.Fatal("unexpected identity read")
				}
				calls++
				switch dst := dst.(type) {
				case *repo:
					*dst = rr
				case *pull:
					*dst = pp
				case *identityRef:
					*dst = rf
				default:
					t.Fatal("unexpected decode target")
				}
				return nil
			})
			if calls != tc.wantCalls || (err == nil) != (tc.name == "accepted") || (err != nil && detail == "") {
				t.Fatal("Press identity rejection boundary changed")
			}
		})
	}
}
