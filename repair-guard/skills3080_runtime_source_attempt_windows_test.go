package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSkills3080UncertainCreatorNeverAllocatesTwice(t *testing.T) {
	for _, outcome := range []string{"timeout_after_creation", "nonzero_after_creation", "malformed_output", "process_crash"} {
		t.Run(outcome, func(t *testing.T) {
			workspace := skillsFixtureRoot(t)
			if os.Mkdir(filepath.Join(workspace, ".ci-work"), 0700) != nil {
				t.Fatal("existing private workspace")
			}
			request := filepath.Join(workspace, "request.json")
			if os.WriteFile(request, []byte("synthetic request"), 0600) != nil {
				t.Fatal("request")
			}
			getID := func(path string) skillsObjectID {
				f, e := os.Open(path)
				if e != nil {
					t.Fatal("request alias")
				}
				defer f.Close()
				id, ok := skillsHandleID(syscall.Handle(f.Fd()))
				if !ok {
					t.Fatal("request identity")
				}
				return id
			}
			id := getID(request)
			alias := getID(strings.ToUpper(filepath.ToSlash(request)))
			if id != alias {
				t.Fatal("real alias control")
			}
			w := skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", Workspace: workspace, SpecSHA: hashBytes([]byte("spec"))}
			calls := 0
			created := filepath.Join(workspace, "simulated-owned-root")
			create := func() (string, string, bool) {
				calls++
				if os.Mkdir(created, 0700) != nil {
					t.Fatal("only one allocation may occur")
				}
				return "", "", false
			}
			first := skillsFS{blocked: func(string) bool { return false }}
			_, _, ok, claimed := skillsRuntimeSourceAllocateOnce(&first, w, id, 6291, hashBytes([]byte("body")), create)
			first.close() // release every process handle, as after a crash/unknown result
			if ok || !claimed || calls != 1 {
				t.Fatal("unknown creation did not leave a durable attempt")
			}
			second := skillsFS{blocked: func(string) bool { return false }}
			defer second.close()
			_, _, _, claimed = skillsRuntimeSourceAllocateOnce(&second, w, alias, 6291, hashBytes([]byte("body")), create)
			if claimed || calls != 1 {
				t.Fatal("unknown outcome was retried through a request alias")
			}
			if info, e := os.Stat(created); e != nil || !info.IsDir() {
				t.Fatal("uncertain original root must remain for reconciliation")
			}
		})
	}
}

func TestSkills3080AttemptWriteMustFinishBeforeCreator(t *testing.T) {
	workspace := skillsFixtureRoot(t) // no .ci-work: do not create an unowned folder
	w := skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", Workspace: workspace, SpecSHA: hashBytes([]byte("spec"))}
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	calls := 0
	_, _, _, claimed := skillsRuntimeSourceAllocateOnce(&fs, w, skillsObjectID{"123", strings.Repeat("1", 32)}, 6291, hashBytes([]byte("body")), func() (string, string, bool) { calls++; return "", "", false })
	if claimed || calls != 0 {
		t.Fatal("creator ran without a durable CreateNew attempt record")
	}
}
