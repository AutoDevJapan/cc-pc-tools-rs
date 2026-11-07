package main

import (
	"errors"
	"strings"
	"testing"
)

func TestSkills3080ConsumerProjectReadsOnlyTheProvenPublishedBlob(t *testing.T) {
	for _, kind := range []string{"valid", "executable_regular", "missing", "symlink", "directory", "other_name", "duplicate", "wrong_blob", "read_failure", "protected"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte("fixed public project instructions\n")
			id := skillsGitBlobID(body)
			e := skillsCreatedEvidence{Writer: skillsTestManifest().Writer, ManifestCommit: strings.Repeat("a", 40)}
			s := skillsFS{blocked: func(string) bool { return kind == "protected" }}
			calls := 0
			git := func(root string, args ...string) ([]byte, error) {
				calls++
				if !same(root, e.Writer.Workspace) {
					t.Fatal("other root selected")
				}
				if len(args) == 4 && args[0] == "ls-tree" && args[1] == e.ManifestCommit && args[2] == "--" && args[3] == "AGENTS.md" {
					mode := "100644"
					if kind == "executable_regular" {
						mode = "100755"
					}
					if kind == "symlink" {
						mode = "120000"
					}
					if kind == "directory" {
						mode = "040000"
					}
					name := "AGENTS.md"
					if kind == "other_name" {
						name = "private.md"
					}
					line := mode + " blob " + id + "\t" + name + "\n"
					if kind == "missing" {
						line = ""
					}
					if kind == "duplicate" {
						line += line
					}
					return []byte(line), nil
				}
				if len(args) == 3 && args[0] == "cat-file" && args[1] == "blob" && args[2] == id {
					if kind == "read_failure" {
						return nil, errors.New("fixture")
					}
					if kind == "wrong_blob" {
						return []byte("unpublished other instructions"), nil
					}
					return body, nil
				}
				t.Fatal("unexpected Git namespace")
				return nil, nil
			}
			ok := skillsConsumerProjectFromCreatedWith(&s, e, git)
			want := kind == "valid" || kind == "executable_regular"
			if ok != want || (want && string(s.bootstrapProjects[e.ManifestCommit]) != string(body)) {
				t.Fatalf("project %s accepted=%v", kind, ok)
			}
			if kind == "protected" && calls != 0 {
				t.Fatal("protection checked after object access")
			}
		})
	}
}
