package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func skillsRevisionFixture(t *testing.T) (skillsManifest, skillsManifest, skillsCreatedEvidence, string) {
	t.Helper()
	m := skillsParsedFixture(t)
	output := func(p, text string) skillsOutput {
		b := []byte(text)
		return skillsOutput{p, hashBytes(b), base64.StdEncoding.EncodeToString(b), b}
	}
	m.Files = nil
	for i := 0; i < 52; i++ {
		m.Files = append(m.Files, output(fmt.Sprintf("design-reference-evidence/references/source%02d.txt", i), fmt.Sprintf("public content %d", i)))
	}
	m.Files[0] = output("design-reference-evidence/SKILL.md", "# Own public test\n")
	m.Files[27] = output(skillsRevisionTarget, "{\"stage\":\"old\"}\n")
	next := m
	next.Files = append([]skillsOutput(nil), m.Files...)
	next.Files[27] = output(skillsRevisionTarget, "{\"stage\":\"revision1\"}\n")
	history := m.Files[27]
	history.Target = skillsRevisionHistory
	next.Files = append(next.Files, history)
	root := skillsFixtureRoot(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	if n, reason := skillsCreate(m, root, &fs); reason != "" || n != 52 {
		t.Fatalf("fixture create %d/%s", n, reason)
	}
	r := skillsRequest{Writer: m.Writer, Review: &skillsResultRequest{Task: 3931}}
	e, ok := skillsIssueCreated(r, strings.Repeat("a", 64), m, root, &fs, skillsProducer{strings.Repeat("b", 64), strings.Repeat("c", 40)})
	if !ok {
		t.Fatal("fixture issue")
	}
	return m, next, e, root
}

func TestSkills3080RevisionPhysicalTransactionAndRecovery(t *testing.T) {
	for _, stop := range []string{"none", "before_archive", "after_archive", "before_publish", "after_publish"} {
		t.Run(stop, func(t *testing.T) {
			old, next, original, root := skillsRevisionFixture(t)
			fs := skillsFS{blocked: func(string) bool { return false }}
			e, reason := skillsRevisionPrepare(old, original, root, next, &fs)
			fs.close()
			if reason != "" {
				t.Fatalf("prepare: %s", reason)
			}
			if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillsRevisionTarget))); err != nil || !bytes.Equal(b, old.Files[27].body) {
				t.Fatal("prepare changed original")
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			changes, reason := skillsRevisionApply(e, original, root, &fs, func(stage string) bool { return stage != stop })
			fs.close()
			if stop == "none" {
				if reason != "" || changes != 2 {
					t.Fatalf("apply %d/%s", changes, reason)
				}
			} else if reason != "writer" {
				t.Fatalf("stop not observed %s", reason)
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			recovered, reason := skillsRevisionApply(e, original, root, &fs, func(string) bool { return true })
			fs.close()
			if reason != "" || changes+recovered != 2 {
				t.Fatalf("recovery %d+%d/%s", changes, recovered, reason)
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			again, reason := skillsRevisionApply(e, original, root, &fs, func(string) bool { return true })
			fs.close()
			if again != 0 || reason != "" {
				t.Fatalf("replay mutated: %d/%s", again, reason)
			}
			for _, i := range []int{27, 52} {
				fs = skillsFS{blocked: func(string) bool { return false }}
				result, reason := skillsConsumeCurrent(next, skillsRevisionSnapshot(original, e, next), root, &fs, []int{i})
				if reason != "" || result["files"].([]object)[0]["content_utf8"] != string(next.Files[i].body) {
					t.Fatalf("same-buffer %d/%s", i, reason)
				}
				fs.close()
			}
			if !skillsRevisionAbsent(filepath.Join(root, filepath.FromSlash(skillsRevisionPending))) {
				t.Fatal("stage not renamed")
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			_, reason = skillsRevisionPrepare(old, original, root, next, &fs)
			fs.close()
			if reason == "" {
				t.Fatal("prepared a successful transaction twice")
			}
		})
	}
}

func TestSkills3080RevisionRejectsSubstitutionAndCollisions(t *testing.T) {
	for _, kind := range []string{"old_bytes", "old_id", "old_hardlink", "old_alias", "parent_id", "parent_reparse", "history_exists", "pending_exists", "protected", "pending_id", "history_id", "foreign_history", "foreign_target_after_archive", "foreign_pending_after_archive", "pending_hardlink", "pending_bytes", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			old, next, original, root := skillsRevisionFixture(t)
			target := filepath.Join(root, filepath.FromSlash(skillsRevisionTarget))
			pending := filepath.Join(root, filepath.FromSlash(skillsRevisionPending))
			history := filepath.Join(root, filepath.FromSlash(skillsRevisionHistory))
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			mutate := func(p string, b []byte) { must(os.WriteFile(p, b, 0600)) }
			switch kind {
			case "old_bytes":
				mutate(target, []byte("different bytes"))
			case "old_id":
				must(os.Rename(target, target+".retained"))
				mutate(target, old.Files[27].body)
			case "old_hardlink":
				must(os.Link(target, target+".alias"))
			case "old_alias":
				root += "."
			case "parent_id":
				original.Directories[0].ID.File = strings.Repeat("f", 32)
			case "parent_reparse":
				refs := filepath.Dir(target)
				must(os.Rename(refs, refs+".retained"))
				cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", refs, refs+".retained")
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("junction fixture: %v %s", err, b)
				}
			case "history_exists":
				must(os.Mkdir(filepath.Dir(history), 0700))
				mutate(history, old.Files[27].body)
			case "pending_exists":
				mutate(pending, next.Files[27].body)
			}
			fs := skillsFS{blocked: func(string) bool { return kind == "protected" }}
			e, reason := skillsRevisionPrepare(old, original, root, next, &fs)
			fs.close()
			prepareNegative := kind == "old_bytes" || kind == "old_id" || kind == "old_hardlink" || kind == "old_alias" || kind == "parent_id" || kind == "parent_reparse" || kind == "history_exists" || kind == "pending_exists" || kind == "protected"
			if prepareNegative {
				if reason == "" {
					t.Fatal("unqualified prepare accepted")
				}
				return
			}
			if reason != "" {
				t.Fatal(reason)
			}
			switch kind {
			case "pending_id":
				must(os.Rename(pending, pending+".retained"))
				mutate(pending, next.Files[27].body)
			case "history_id":
				must(os.Rename(filepath.Dir(history), filepath.Dir(history)+".retained"))
				must(os.Mkdir(filepath.Dir(history), 0700))
			case "foreign_history":
				mutate(history, old.Files[27].body)
			case "pending_hardlink":
				must(os.Link(pending, pending+".alias"))
			case "pending_bytes":
				mutate(pending, []byte("modified"))
			case "foreign_target_after_archive", "foreign_pending_after_archive":
				fs = skillsFS{blocked: func(string) bool { return false }}
				_, reason = skillsRevisionApply(e, original, root, &fs, func(stage string) bool { return stage != "after_archive" })
				fs.close()
				if reason != "writer" {
					t.Fatal("setup did not interrupt")
				}
				if kind == "foreign_target_after_archive" {
					mutate(target, next.Files[27].body)
				} else {
					must(os.Rename(pending, pending+".retained"))
					mutate(pending, next.Files[27].body)
				}
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			changes, reason := skillsRevisionApply(e, original, root, &fs, func(string) bool { return kind != "stopped" })
			fs.close()
			if reason == "" || changes != 0 {
				t.Fatalf("unqualified mutation %d/%s", changes, reason)
			}
			p := target
			if strings.HasPrefix(kind, "foreign_") && kind != "foreign_history" {
				p = history
			}
			b, err := os.ReadFile(p)
			if err != nil || !bytes.Equal(b, old.Files[27].body) {
				t.Fatal("original bytes lost")
			}
		})
	}
}

func TestSkills3080RevisionCLIAndNoGeneralDispatch(t *testing.T) {
	for _, op := range []string{"qualify-skills3080-revision", "prepare-skills3080-revision", "apply-skills3080-revision", "consume-skills3080-revision"} {
		var out bytes.Buffer
		calls := 0
		if !skillsFlag([]string{"--" + op}) || runSkillsCLI([]string{"--" + op, "D:/request.json"}, &out, func(got, _ string) object { calls++; return object{"qualified": got == op} }) != 0 || calls != 1 {
			t.Fatalf("missing dispatch %s", op)
		}
	}
	// IDs and byte content are not request fields.
	for _, key := range []string{"root", "receipt", "old_id", "new_id", "content_base64", "backup"} {
		b, _ := json.Marshal(object{key: "caller supplied"})
		var r skillsRevisionRequest
		if skillsStrict(b, &r) {
			t.Fatalf("accepted %s", key)
		}
	}
}

func TestSkills3080RevisionOnlyExactPublicMetadataDelta(t *testing.T) {
	old, next, _, _ := skillsRevisionFixture(t)
	for _, x := range []struct {
		m    *skillsManifest
		name string
	}{{&old, "old"}, {&next, "new"}} {
		b, err := os.ReadFile("testdata/skills3080-revision1-" + x.name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		x.m.Files[27].body = b
		x.m.Files[27].SHA = hashBytes(b)
		x.m.Files[27].Content = base64.StdEncoding.EncodeToString(b)
	}
	next.Files[52] = old.Files[27]
	next.Files[52].Target = skillsRevisionHistory
	for _, kind := range []string{"valid", "changed51", "wrong_target", "history_bytes", "new_bytes", "source_rights", "writer", "missing_history", "extra_file"} {
		t.Run(kind, func(t *testing.T) {
			m := next
			m.Files = append([]skillsOutput(nil), next.Files...)
			m.Sources = append([]skillsSource(nil), next.Sources...)
			switch kind {
			case "changed51":
				m.Files[2].SHA = strings.Repeat("f", 64)
			case "wrong_target":
				m.Files[27].Target = "other/references/sources.json"
			case "history_bytes":
				m.Files[52].body = append(append([]byte{}, m.Files[52].body...), byte(' '))
			case "new_bytes":
				m.Files[27].body = []byte("fake")
			case "source_rights":
				m.Sources[0].License.Scope = "changed"
			case "writer":
				m.Writer.Job = "20261026-140000-todo-3080"
			case "missing_history":
				m.Files = m.Files[:52]
			case "extra_file":
				m.Files = append(m.Files, m.Files[0])
			}
			if skillsRevisionManifestShape(old, m) != (kind == "valid") {
				t.Fatalf("%s", kind)
			}
		})
	}
}

func TestSkills3080RevisionProcessCrashRecovery(t *testing.T) {
	if root := os.Getenv("CC_REVISION_TEST_ROOT"); root != "" {
		var e skillsRevisionEvidence
		var original skillsCreatedEvidence
		if json.Unmarshal([]byte(os.Getenv("CC_REVISION_TEST_EVIDENCE")), &e) != nil || json.Unmarshal([]byte(os.Getenv("CC_REVISION_TEST_ORIGINAL")), &original) != nil {
			os.Exit(21)
		}
		fs := skillsFS{blocked: func(string) bool { return false }}
		_, reason := skillsRevisionApply(e, original, root, &fs, func(stage string) bool {
			if stage == os.Getenv("CC_REVISION_TEST_STOP") {
				os.Exit(19)
			}
			return true
		})
		fs.close()
		if reason != "" {
			os.Exit(22)
		}
		os.Exit(23)
	}
	for _, stop := range []string{"before_archive", "after_archive", "before_publish", "after_publish"} {
		t.Run(stop, func(t *testing.T) {
			old, next, original, root := skillsRevisionFixture(t)
			fs := skillsFS{blocked: func(string) bool { return false }}
			e, reason := skillsRevisionPrepare(old, original, root, next, &fs)
			fs.close()
			if reason != "" {
				t.Fatal(reason)
			}
			eb, _ := json.Marshal(e)
			ob, _ := json.Marshal(original)
			cmd := exec.Command(os.Args[0], "-test.run=^TestSkills3080RevisionProcessCrashRecovery$")
			cmd.Env = append(os.Environ(), "CC_REVISION_TEST_ROOT="+root, "CC_REVISION_TEST_STOP="+stop, "CC_REVISION_TEST_EVIDENCE="+string(eb), "CC_REVISION_TEST_ORIGINAL="+string(ob))
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			if b, err := cmd.CombinedOutput(); err == nil || cmd.ProcessState.ExitCode() != 19 {
				t.Fatalf("child did not terminate at boundary: %v %s", err, b)
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			_, reason = skillsRevisionApply(e, original, root, &fs, func(string) bool { return true })
			fs.close()
			if reason != "" {
				t.Fatalf("post-crash recovery %s", reason)
			}
			fs = skillsFS{blocked: func(string) bool { return false }}
			result, reason := skillsConsumeCurrent(next, skillsRevisionSnapshot(original, e, next), root, &fs, []int{27, 52})
			fs.close()
			if reason != "" || len(result["files"].([]object)) != 2 {
				t.Fatalf("post-crash readback %s", reason)
			}
		})
	}
}
