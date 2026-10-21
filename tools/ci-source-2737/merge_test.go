package sourcepacket

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPressMergePacketProvesActualTwoParentExecutionCommit(t *testing.T) {
	p, root := fixtureForWorkflow(t, ".github/workflows/windows-hidden-fixtures.yml")
	src := filepath.Join(root, "repo")
	base := strings.TrimSpace(string(run(t, src, []byte("public base\n"), "commit-tree", p.Pin.TreeSHA)))
	candidate := p.Pin.HeadSHA
	head := strings.TrimSpace(string(run(t, src, []byte("public merge\n"), "commit-tree", p.Pin.TreeSHA, "-p", base, "-p", candidate)))
	p.Pin.Product, p.Pin.Workflow, p.Pin.PullHeadSHA, p.Pin.MergeBaseSHA, p.Pin.HeadSHA = PressProduct, "windows-hidden-fixtures.yml", candidate, base, head
	p.CommitPayload = run(t, src, nil, "cat-file", "commit", head)
	encoded, e := Encode(p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := Decode(encoded, p.Pin)
	if e != nil {
		t.Fatal(e)
	}
	dst := filepath.Join(root, "merge")
	proof, e := Materialize(context.Background(), got, p.Pin, dst)
	if e != nil || proof.HeadSHA != head || proof.HeadSHA == candidate || proof.RepoID != 1386702183 || !proof.PhysicalBlobsVerified {
		t.Fatalf("merge physical proof rejected: %v", e)
	}
	wrongParent := p.Pin
	wrongParent.MergeBaseSHA = strings.Repeat("f", 40)
	if _, e := VerifyCheckout(context.Background(), wrongParent, p.TreeEntries, dst); e == nil {
		t.Fatal("wrong physical merge-parent pin accepted")
	}
	for _, change := range []func(*Pin){
		func(p *Pin) { p.PullHeadSHA = "" }, func(p *Pin) { p.MergeBaseSHA = "" },
		func(p *Pin) { p.MergeBaseSHA, p.PullHeadSHA = p.PullHeadSHA, p.MergeBaseSHA },
		func(p *Pin) { p.Product = "" }, func(p *Pin) { p.MergeBaseSHA = strings.Repeat("e", 40) },
	} {
		q := p
		change(&q.Pin)
		if _, e := Encode(q); e == nil {
			t.Fatal("unbound/swapped merge parents accepted")
		}
	}
}

func TestPhysicalCommitObjectCannotMasqueradeUnderOriginalObjectName(t *testing.T) {
	p, root := fixture(t)
	dst := filepath.Join(root, "proof")
	if _, e := Materialize(context.Background(), p, p.Pin, dst); e != nil {
		t.Fatal(e)
	}
	forged := bytes.Replace(p.CommitPayload, []byte("synthetic fixture"), []byte("synthetic altered"), 1)
	if bytes.Equal(forged, p.CommitPayload) {
		t.Fatal("mutation did not change public fixture")
	}
	var object bytes.Buffer
	z := zlib.NewWriter(&object)
	fmt.Fprintf(z, "commit %d%c", len(forged), 0)
	_, _ = z.Write(forged)
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	objectPath := filepath.Join(dst, ".git", "objects", p.Pin.HeadSHA[:2], p.Pin.HeadSHA[2:])
	if e := os.Chmod(objectPath, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(objectPath, object.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyCheckout(context.Background(), p.Pin, p.TreeEntries, dst); e == nil {
		t.Fatal("changed physical commit bytes accepted under original SHA name")
	}
}

func TestPressMergeIdentityPinsBothLiveRefsAndRetainsOriginalCandidate(t *testing.T) {
	pin := Pin{Product: PressProduct, Workflow: "rust-v2-https.yml", HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64), PullHeadSHA: strings.Repeat("d", 40), MergeBaseSHA: strings.Repeat("e", 40)}
	for _, bad := range []string{"accept", "pr-merge", "candidate", "candidate-ref", "main-ref", "merge-ref"} {
		t.Run(bad, func(t *testing.T) {
			calls := 0
			_, _, e := verifySourceIdentity(context.Background(), pin, func(_ context.Context, ep string, out any) error {
				calls++
				switch dst := out.(type) {
				case *repo:
					*dst = repo{ID: 1386702183, FullName: "AutoDevJapan/GoDD-Press", Private: true}
					dst.Owner.Login = "AutoDevJapan"
					dst.Permissions.Admin = true
				case *pull:
					dst.State, dst.Draft, dst.MergeCommitSHA = "open", true, pin.HeadSHA
					r := repo{ID: 1386702183, FullName: "AutoDevJapan/GoDD-Press"}
					dst.Head.SHA, dst.Head.Ref, dst.Head.Repo, dst.Base.Repo, dst.Base.SHA = pin.PullHeadSHA, "fix/todo2642-schedule-predecessor-overflow", r, r, PressBase
					if bad == "pr-merge" {
						dst.MergeCommitSHA = pin.PullHeadSHA
					}
					if bad == "candidate" {
						dst.Head.SHA = pin.HeadSHA
					}
				case *identityRef:
					want := pin.PullHeadSHA
					if strings.HasSuffix(ep, "heads/main") {
						want = pin.MergeBaseSHA
						if bad == "main-ref" {
							want = pin.PullHeadSHA
						}
					} else if strings.HasSuffix(ep, "pull/175/merge") {
						want = pin.HeadSHA
						if bad == "merge-ref" {
							want = pin.PullHeadSHA
						}
					} else if bad == "candidate-ref" {
						want = pin.HeadSHA
					}
					dst.Object.SHA = want
				default:
					t.Fatal("unexpected identity target")
				}
				return nil
			})
			if (e == nil) != (bad == "accept") {
				t.Fatal("merge identity mismatch accepted")
			}
			if bad == "accept" && calls != 5 {
				t.Fatal("actual merge and main refs not both checked")
			}
		})
	}
}
