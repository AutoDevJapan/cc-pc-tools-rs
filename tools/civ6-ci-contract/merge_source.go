package contract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// A second, finite source generation. The original contract is never redefined.
const MergeMain = "392e7f9bbcea6ee5867b78da37de1efd9f9fec48"
const MergeMainTree = "b5680a158cfcad929e49e97702eec210cc82135d"
const OldRoutedHead = "2bd3a366f8974ba54df83927102aa5bdad3983e5"
const OldRoutedTree = "a4e37a064e0f2ea316d0f15d9aaf710abf9b65f6"
const MergeGeneration = "pr149-main-392e7f9"

const retainedConcurrency = "concurrency:\n  group: ${{ github.workflow }}-${{ github.event_name }}-${{ github.event.pull_request.number || github.run_id }}\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}\n\n"

var mainWorkflows = []Workflow{
	{".github/workflows/rust.yml", "test", "72858e501824f1949b3caa4a298e6610212b27f1382e68b98bd2b7b1a11d077f", "${{ matrix.os }}"},
	{".github/workflows/native-driver.yml", "native-driver", "c2eb4b8e7710e52810e190183c30df40e114a88670357ddb83fc96c772c0414c", "windows-latest"},
	{".github/workflows/live-chain.yml", "live-chain", "d1f68680e1be885240d205a4981011a46209dbc27c4fc34a7c54f51ec891f989", "windows-latest"},
}

func MainWorkflows() []Workflow { return append([]Workflow(nil), mainWorkflows...) }

// ProposeMain preserves the exact API-pinned main bytes except these reviewed
// spans. Main already has doctests and checkout auth options; neither is added.
func ProposeMain(data []byte, w Workflow) ([]byte, error) {
	found := false
	for _, canonical := range mainWorkflows {
		if canonical == w {
			found = true
		}
	}
	if !found || Digest(data) != w.OriginalDigest {
		return nil, errors.New("fixed main workflow fingerprint differs")
	}
	out := bytes.Clone(data)
	replace := func(old, next string) error {
		if bytes.Count(out, []byte(old)) != 1 {
			return errors.New("fixed main workflow span differs")
		}
		out = bytes.Replace(out, []byte(old), []byte(next), 1)
		return nil
	}
	for _, pair := range [][2]string{
		{"jobs:\n", retainedConcurrency + "jobs:\n"},
		{"    runs-on: " + w.OriginalRoute + "\n", "    runs-on: " + Route(w) + "\n"},
		{"          ref: ${{ github.sha }}\n", "          ref: " + CheckoutRef + "\n"},
	} {
		if err := replace(pair[0], pair[1]); err != nil {
			return nil, err
		}
	}
	if w.Job == "test" {
		if err := replace("        os: [ubuntu-latest, windows-latest]\n", "        os: [windows-latest]\n"); err != nil {
			return nil, err
		}
		step := "      - name: Record release binary hashes\n        shell: pwsh\n        run: |\n"
		for _, line := range strings.Split(strings.TrimSuffix(ArtifactHashCommand, "\n"), "\n") {
			step += "          " + line + "\n"
		}
		marker := "      - name: Check existing scenario JSON\n"
		if err := replace(marker, step+marker); err != nil {
			return nil, err
		}
	}
	if _, err := parse(out); err != nil {
		return nil, err
	}
	return out, nil
}

func fixedCommit(ctx context.Context, read ReadAPI, head, tree string, parents []string) error {
	var c commit
	if err := read(ctx, "repos/"+Repository+"/git/commits/"+head, &c); err != nil {
		return err
	}
	if c.SHA != head || c.Tree.SHA != tree || (parents != nil && len(c.Parents) != len(parents)) {
		return errors.New("fixed generation commit/tree/parents differ")
	}
	for i, parent := range parents {
		if c.Parents[i].SHA != parent {
			return errors.New("fixed generation parent order differs")
		}
	}
	return nil
}

// VerifyOldRoutedSource is immutable historical verification, independent of
// the current branch. It grants no admission, execution or cleanup authority.
func VerifyOldRoutedSource(ctx context.Context, read ReadAPI) error {
	if err := fixedCommit(ctx, read, OldRoutedHead, OldRoutedTree, []string{OriginalHead}); err != nil {
		return err
	}
	expected := map[string]string{}
	for _, w := range Workflows() {
		b, err := contents(ctx, read, w.Path, OriginalHead)
		if err != nil {
			return err
		}
		want, err := Propose(b, w)
		if err != nil {
			return err
		}
		got, err := contents(ctx, read, w.Path, OldRoutedHead)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			return errors.New("old routed source differs")
		}
		expected[w.Path] = blobID(got)
	}
	return checkFixedTree(ctx, read, OriginalTree, OldRoutedTree, expected)
}

// VerifyHistoricalInitial checks a recorded old execution without requiring
// its branch to remain current. It cannot authorize starting that execution.
func VerifyHistoricalInitial(ctx context.Context, p Plan, read ReadAPI) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Head != OldRoutedHead || p.Tree != OldRoutedTree || p.Event != "pull_request" || p.PR != 149 || p.Branch != OriginalBranch || p.Attempt != 1 || p.OS() != "Windows" || p.Workflow == CodeQLWorkflow {
		return errors.New("only the fixed historical initial Windows source is observed")
	}
	if err := VerifyOldRoutedSource(ctx, read); err != nil {
		return err
	}
	for _, w := range Workflows() {
		want, err := contents(ctx, read, w.Path, OldRoutedHead)
		if err != nil {
			return err
		}
		got, err := contents(ctx, read, w.Path, p.WorkflowSHA)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) || (w.Path == p.Workflow && Digest(got) != p.WorkflowDigest) {
			return errors.New("historical executable workflow differs")
		}
	}
	var run Run
	if err := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/1", Repository, p.RunID), &run); err != nil {
		return err
	}
	if run.ID != p.RunID || run.RunAttempt != 1 || run.HeadSHA != OldRoutedHead || run.HeadBranch != OriginalBranch || run.Event != "pull_request" || run.Status != "completed" || !MatchRunPath(p, run.Path) || !sameRepo(run.Repository) || !sameRepo(run.HeadRepo) || len(run.PullRequests) != 1 || run.PullRequests[0].Number != 149 {
		return errors.New("historical official run differs")
	}
	return nil
}

func checkFixedTree(ctx context.Context, read ReadAPI, baseTree, newTree string, expected map[string]string) error {
	var a, b treeReply
	if err := read(ctx, "repos/"+Repository+"/git/trees/"+baseTree+"?recursive=1", &a); err != nil {
		return err
	}
	if err := read(ctx, "repos/"+Repository+"/git/trees/"+newTree+"?recursive=1", &b); err != nil {
		return err
	}
	return checkTreeFrom(a, b, baseTree, newTree, expected)
}

// VerifyMergeSource binds a derived commit to exactly [old tip, fixed main],
// the reviewed three blobs, and every unchanged path/mode/type of fixed main.
func VerifyMergeSource(ctx context.Context, head, tree string, read ReadAPI) error {
	if !ValidSHA(head) || !ValidSHA(tree) {
		return errors.New("merged source identity required")
	}
	if err := VerifyOldRoutedSource(ctx, read); err != nil {
		return err
	}
	if err := fixedCommit(ctx, read, MergeMain, MergeMainTree, nil); err != nil {
		return err
	}
	if err := fixedCommit(ctx, read, head, tree, []string{OldRoutedHead, MergeMain}); err != nil {
		return err
	}
	expected := map[string]string{}
	for _, w := range MainWorkflows() {
		b, err := contents(ctx, read, w.Path, MergeMain)
		if err != nil {
			return err
		}
		want, err := ProposeMain(b, w)
		if err != nil {
			return err
		}
		got, err := contents(ctx, read, w.Path, head)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			return errors.New("merged workflow differs from fixed main proposal")
		}
		expected[w.Path] = blobID(got)
	}
	return checkFixedTree(ctx, read, MergeMainTree, tree, expected)
}
