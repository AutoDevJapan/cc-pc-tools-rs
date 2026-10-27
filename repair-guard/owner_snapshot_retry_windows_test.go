package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerSourceRequalifiesOneChangedSnapshotUnderSameDeadline(t *testing.T) {
	for _, mode := range []string{"one-replacement", "continuous-replacement", "projects-removed", "cancelled", "missing", "denied", "identity-denied"} {
		t.Run(mode, func(t *testing.T) {
			root, db, access := ownerSourceFixture(t)
			path := filepath.Join(t.TempDir(), "registration.json")
			encode := func(projects object) []byte {
				raw, err := json.Marshal(object{"projects": projects, "items": []any{}})
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
			initial := encode(db["projects"].(object))
			if err := os.WriteFile(path, initial, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			defer cancel()
			deadline, _ := ctx.Deadline()
			reads, identities, gitCalls := 0, 0, 0
			metadataDenied := false
			access.git = func(gitRoot string, args ...string) (string, bool) {
				gitCalls++
				if !same(gitRoot, root) {
					t.Fatal("retry changed the source root")
				}
				switch args[0] {
				case "rev-parse":
					return root + "\n" + filepath.Join(root, ".git"), true
				case "remote":
					return "https://github.com/AutoDevJapan/example.git", true
				}
				t.Fatal("unexpected Git request")
				return "", false
			}
			access.identity = func(repo string) (int64, string, string, bool) {
				identities++
				if identities == 1 || mode == "continuous-replacement" {
					switch mode {
					case "missing":
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					case "denied":
						metadataDenied = true
					default:
						raw := initial
						if mode == "projects-removed" {
							raw = encode(object{})
						}
						next := path + ".next"
						if err := os.WriteFile(next, raw, 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.Rename(next, path); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "cancelled" {
						cancel()
					}
				}
				return 12345, repo, "AutoDevJapan", mode != "identity-denied"
			}
			read := func(got context.Context) (*ownerProjectSnapshot, bool) {
				reads++
				gotDeadline, ok := got.Deadline()
				if got != ctx || !ok || !gotDeadline.Equal(deadline) {
					t.Fatal("retry replaced or extended the admission deadline")
				}
				return ownerReadProjectSnapshot(got, path, func(string) bool { return metadataDenied })
			}
			v := ownerSourceQueryWith(ctx, root, access, read)
			wantReads, wantIdentities := 1, 1
			if mode == "one-replacement" || mode == "continuous-replacement" {
				wantReads, wantIdentities = 2, 2
			} else if mode == "projects-removed" {
				wantReads = 2
			}
			if v.AccessAllowed != (mode == "one-replacement") || reads != wantReads || identities != wantIdentities || gitCalls != 2*wantIdentities {
				t.Fatalf("mode=%s allowed=%v reads=%d identities=%d git=%d", mode, v.AccessAllowed, reads, identities, gitCalls)
			}
		})
	}
}
