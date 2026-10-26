package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func official3387GitFixture(t *testing.T, repository string) (string, string) {
	t.Helper()
	root := t.TempDir()
	branch := "scheduler/todo-3888-synthetic"
	for _, args := range [][]string{
		{"init", "--initial-branch=" + branch, root},
		{"-C", root, "remote", "add", "origin", "https://github.com/" + repository + ".git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = civ6GitEnvironment(os.Environ())
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create synthetic Git metadata: %v %s", err, out)
		}
	}
	return root, branch
}

func TestOfficialCI3387ProviderUsesItsOwnRepository(t *testing.T) {
	root, branch := official3387GitFixture(t, "AutoDevJapan/local-ai-console")
	if !official3894ProviderSource(root, branch) {
		t.Fatal("normal CI provider with its own repository and branch was rejected")
	}
	if skillsActorSource(root, branch) {
		t.Fatal("the public Skills source rule must still reject the private repository")
	}
	if official3894ProviderSource(root, branch+"-different") {
		t.Fatal("a different current branch was admitted")
	}
	if official3894ProviderSource(filepath.Join(root, "missing"), branch) {
		t.Fatal("an unreadable source was admitted")
	}
}

func TestOfficialCI3387ProviderRejectsAnotherRepository(t *testing.T) {
	for _, repo := range []string{"AutoDevJapan/cc-pc-tools-rs", "another-owner/local-ai-console"} {
		t.Run(repo, func(t *testing.T) {
			root, branch := official3387GitFixture(t, repo)
			if official3894ProviderSource(root, branch) {
				t.Fatal("another repository supplied CI provider authority")
			}
		})
	}
}

func TestOfficialCI3387ReaderPreservesPreparedProducer(t *testing.T) {
	reader := strings.Repeat("a", 40)
	saved := official3894Source{Head: "f5f846a8470b544846e4c9ce43306061bf5668f9", Registration: strings.Repeat("b", 64), CreatorJob: "synthetic-original", Workspace: "synthetic", Branch: "original", SpecSHA: strings.Repeat("c", 64)}
	before, _ := json.Marshal(saved)
	producer, ok := official3894ProducerHead(reader, &saved)
	if !ok || producer != saved.Head {
		t.Fatal("a repaired reader cannot verify the original prepared source without rewriting its receipt")
	}
	after, _ := json.Marshal(saved)
	if string(before) != string(after) {
		t.Fatal("reader selection changed the original producer registration")
	}
	if got, ok := official3894ProducerHead(reader, nil); !ok || got != reader {
		t.Fatal("new creation must bind to its own clean reader build")
	}
	if got, ok := official3894ProducerHead(reader, &official3894Source{Head: reader}); !ok || got != reader {
		t.Fatal("same-build receipt was rejected")
	}
	for _, bad := range []string{"", strings.Repeat("d", 40), strings.ToUpper(saved.Head)} {
		if _, ok := official3894ProducerHead(reader, &official3894Source{Head: bad}); ok {
			t.Fatal("unknown producer revision was adopted")
		}
	}
}
