package pressci

import (
	"cc/product-ci/normalci"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixtures() map[string][]byte {
	return map[string][]byte{
		"local-checks.yml": []byte("name: public fixture\non:\n  pull_request:\n  workflow_dispatch:\njobs:\n" +
			"  rust-core:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 90\n" +
			"  rust-integration:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 90\n    strategy:\n      fail-fast: false\n      matrix:\n        shard: [1, 2, 3, 4]\n" +
			"  rust-field-read-signing:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 90\n" +
			"  rust-aux:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 90\n" +
			"  browser-package:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 90\n" +
			"  browser-cold-start:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 20\n" +
			"  local-gates:\n    runs-on: ubuntu-24.04\n"),
		"rust-v2-https.yml":           []byte("name: public fixture\non:\n  pull_request:\njobs:\n  rust-v2-https:\n    runs-on: ${{ matrix.os }}\n    timeout-minutes: 90\n    strategy:\n      fail-fast: false\n      matrix:\n        os: [ubuntu-24.04, windows-2022]\n"),
		"windows-hidden-fixtures.yml": []byte("name: public fixture\non:\n  pull_request:\njobs:\n  junction-fixtures:\n    runs-on: windows-2022\n    timeout-minutes: 90\n"),
	}
}

func TestOutlineRetainsAllThirteenJobsAndOriginalEvents(t *testing.T) {
	count := 0
	for name, data := range fixtures() {
		out, e := outlineWorkflow(data, name)
		if e != nil {
			t.Fatal(e)
		}
		if out.Dispatch != (name == "local-checks.yml") {
			t.Fatal("manual event manufactured")
		}
		count += len(out.Jobs)
		for _, j := range out.Jobs {
			if j.ID == "local-gates" && j.TimeoutMinutes != 360 {
				t.Fatal("original absent timeout was changed")
			}
			if j.ID == "rust-integration" && (j.MatrixKey != "shard" || !strings.Contains("1234", j.MatrixValue) || j.Name != "rust-integration ("+j.MatrixValue+")") {
				t.Fatal("required shard missing")
			}
		}
	}
	if count != 13 {
		t.Fatal("required jobs omitted")
	}
	local := fixtures()["local-checks.yml"]
	for _, data := range [][]byte{
		[]byte(strings.Replace(string(local), "shard: [1, 2, 3, 4]", "shard: [1, 2, 3]", 1)),
		[]byte(strings.Replace(string(local), "fail-fast: false", "fail-fast: true", 1)),
		[]byte(strings.Replace(string(local), "runs-on: ubuntu-24.04", "runs-on: windows-2022", 1)),
		append(append([]byte{}, local...), []byte("  other:\n    runs-on: ubuntu-24.04\n")...),
		[]byte(strings.Replace(string(local), "timeout-minutes: 90", "timeout-minutes: 0", 1)),
		[]byte(strings.Replace(string(local), "  rust-core:\n", "  rust-core:\n    name: hidden\n", 1)),
	} {
		if _, e := outlineWorkflow(data, "local-checks.yml"); e == nil {
			t.Fatal("changed required inventory accepted")
		}
	}
}

func TestInspectOriginalRevalidatesAndRefusesPartialInventory(t *testing.T) {
	data := fixtures()
	var expected []workflow
	for _, w := range workflows() {
		expected = append(expected, workflow{w.name, blobID(data[w.name])})
	}
	for _, tc := range []string{"accept", "owner", "draft", "base", "ref", "tree", "blob", "content", "missing", "advance-after"} {
		t.Run(tc, func(t *testing.T) {
			calls, repoReads, contentReads := 0, 0, 0
			read := func(_ context.Context, ep string, dst any) error {
				calls++
				var value any
				switch {
				case ep == "repositories/1386702183":
					repoReads++
					owner := "AutoDevJapan"
					if tc == "owner" {
						owner = "other"
					}
					value = map[string]any{"id": RepoID, "full_name": Repository, "private": true, "fork": false, "owner": map[string]any{"login": owner}, "permissions": map[string]any{"admin": true}}
				case strings.HasSuffix(ep, "/pulls/175"):
					base, ref, head := Base, Ref, Head
					if tc == "base" {
						base = strings.Repeat("d", 40)
					}
					if tc == "ref" {
						ref = "other"
					}
					if tc == "advance-after" && contentReads == 3 {
						head = strings.Repeat("d", 40)
					}
					repo := map[string]any{"id": RepoID, "full_name": Repository, "fork": false}
					value = map[string]any{"state": "open", "draft": tc != "draft", "head": map[string]any{"sha": head, "ref": ref, "repo": repo}, "base": map[string]any{"sha": base, "repo": repo}}
				case strings.Contains(ep, "/git/ref/heads/"):
					value = map[string]any{"object": map[string]string{"sha": Head}}
				case strings.Contains(ep, "/git/commits/"):
					tree := Tree
					if tc == "tree" {
						tree = strings.Repeat("d", 40)
					}
					value = map[string]any{"sha": Head, "tree": map[string]string{"sha": tree}}
				case strings.Contains(ep, "/contents/.github/workflows/"):
					contentReads++
					if tc == "missing" {
						return errors.New("private response")
					}
					name := strings.Split(strings.Split(ep, "/workflows/")[1], "?")[0]
					blob, body := blobID(data[name]), data[name]
					if tc == "blob" {
						blob = strings.Repeat("d", 40)
					}
					if tc == "content" {
						body = append(append([]byte{}, body...), '\n')
					}
					value = map[string]string{"sha": blob, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body)}
				default:
					t.Fatal("unexpected read")
				}
				b, e := json.Marshal(value)
				if e != nil {
					return e
				}
				return json.Unmarshal(b, dst)
			}
			out, e := inspectOriginal(context.Background(), normalci.API(read), expected)
			if (e == nil) != (tc == "accept") {
				t.Fatal("original identity refusal changed")
			}
			if e == nil && (!out.SourceRevalidated || repoReads != 2 || len(out.Workflows) != 3 || out.RunnerAdmission || out.OfficialCISuccess) {
				t.Fatal("outline promoted to execution or partial result")
			}
			if tc == "owner" && (calls != 1 || contentReads != 0) {
				t.Fatal("rejection read later data")
			}
			if e != nil && (out.Repository != "" || len(out.Workflows) != 0) {
				t.Fatal("partial outline returned as valid")
			}
		})
	}
}
