package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsumerTreeMatchesIndependentGitAndRejectsMutation(t *testing.T) {
	v := winPlan{Root: t.TempDir()}
	source := filepath.Join(v.Root, "source")
	if e := os.MkdirAll(filepath.Join(source, "foo"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"foo/a", "foo.bar", "日本語.txt"} {
		if e := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte("fixture\n"), 0600); e != nil {
			t.Fatal(e)
		}
		mode := "100644"
		if name == "foo/a" {
			mode = "100755"
		}
		v.Entries = append(v.Entries, winEntry{Name: name, Mode: mode})
	}
	git := func(args ...string) string {
		c := cmd(context.Background(), `C:\Program Files\Git\cmd\git.exe`, append([]string{"-C", source, "-c", "core.autocrlf=false"}, args...)...)
		c.Env = []string{`SystemRoot=C:\Windows`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=NUL`}
		b, e := c.Output()
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-q")
	git("add", "--all")
	git("update-index", "--chmod=+x", "foo/a")
	v.Tree = git("write-tree")
	if e := winSourceTree(v); e != nil {
		t.Fatalf("Git oracle did not match: %v", e)
	}
	if e := os.WriteFile(filepath.Join(source, "foo.bar"), []byte("changed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if winSourceTree(v) == nil {
		t.Fatal("changed bytes accepted")
	}
	os.WriteFile(filepath.Join(source, "foo.bar"), []byte("fixture\n"), 0600)
	sha := strings.Repeat("a", 40)
	git("update-index", "--add", "--cacheinfo", "160000,"+sha+",vendor/x")
	v.Entries = append(v.Entries, winEntry{Name: "vendor/x", Mode: "160000", SHA: sha})
	v.Tree = git("write-tree")
	if e := winSourceTree(v); e != nil {
		t.Fatalf("gitlink oracle mismatch: %v", e)
	}
}
