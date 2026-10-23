package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// gitConfigList is what Git itself reads from a configuration file: one
// "name" or "name\nvalue" per variable, in file order.
func gitConfigList(t *testing.T, config string) []string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(name, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "config", "--file", name, "--null", "--list")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Env = append(childEnvironment(os.Environ()), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git config --list: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

// Every configuration the reader accepts means to it what it means to Git.
func TestConfigParserAgreesWithGit(t *testing.T) {
	origin := "https://github.com/AutoDevJapan/cc-pc-tools-rs.git"
	for name, config := range map[string]string{
		"written by Git":           "[core]\n\trepositoryformatversion = 0\n\tbare = false\n[remote \"origin\"]\n\turl = " + origin + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
		"quotes and comments":      "# top\n[remote \"origin\"] ; header\n\turl = \"" + origin + "\" # note\n\tpushurl = \"a#b;c\"\n",
		"case and spacing":         "[REMOTE \"origin\"]\n  URL=" + origin + "#x\n",
		"byte order mark and CRLF": "\ufeff[remote \"origin\"]\r\n\turl = " + origin + "\r\n",
		"escapes and blanks":       "[a \"b\\\"c\\\\d\"]\n\tk = \"q\\\\ \\t\\n\\b\" mid  tail \t\n\tflag\n\tempty =\n",
		"no final newline":         "[core]\n\tbare = false",
		"origin twice":             "[remote \"origin\"]\n\turl = a\n[remote \"origin\"]\n\turl = b\n",
	} {
		t.Run(name, func(t *testing.T) {
			entries, ok := parseConfig([]byte(config))
			if !ok {
				t.Fatalf("parseConfig refused %q", config)
			}
			var got []string
			for _, e := range entries {
				if e.key == "" {
					continue
				}
				name := e.section + "."
				if e.subsection != "" {
					name += e.subsection + "."
				}
				name += e.key
				if e.hasValue {
					name += "\n" + e.value
				}
				got = append(got, name)
			}
			if want := gitConfigList(t, config); !slices.Equal(got, want) {
				t.Fatalf("reader read\n%q\nGit read\n%q", got, want)
			}
		})
	}
}

// Lines the reader might not read as Git does are refused, which only narrows
// what qualifies.
func TestConfigParserRefusesWhatItDoesNotRead(t *testing.T) {
	for name, config := range map[string]string{
		"continuation line":            "[core]\n\tbare = fal\\\nse\n",
		"variable after a header":      "[core] bare = false\n",
		"old subsection form":          "[remote.origin]\n\turl = x\n",
		"empty subsection":             "[core \"\"]\n\tbare = true\n",
		"other escape in a subsection": "[remote \"or\\igin\"]\n\turl = x\n",
		"open quote":                   "[core]\n\tbare = \"false\n",
		"unknown escape":               "[core]\n\tbare = \\q\n",
		"variable outside a section":   "bare = false\n",
		"comment after a name":         "[core]\n\tbare # true\n",
		"control character":            "[core]\n\tbare = fa\x01lse\n",
		"lone carriage return":         "[core]\n\tbare = false\r\tx = y\n",
		"open header":                  "[core\n\tbare = false\n",
	} {
		if _, ok := parseConfig([]byte(config)); ok {
			t.Errorf("%s: parseConfig accepted %q", name, config)
		}
	}
}

func TestPackedRefs(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	header := "# pack-refs with: peeled fully-peeled sorted \n"
	for name, data := range map[string]string{
		"written by Git":         header + a + " refs/heads/work\n" + b + " refs/tags/v1\n^" + a + "\n",
		"no header in any order": b + " refs/tags/v1\n" + a + " refs/heads/work\n",
		"not declared sorted":    "# pack-refs with: peeled \n" + b + " refs/tags/v1\n" + a + " refs/heads/work\n",
	} {
		refs, ok := packedRefs([]byte(data))
		if !ok || len(refs) != 2 || refs["refs/heads/work"] != a || refs["refs/tags/v1"] != b {
			t.Errorf("%s: packedRefs = %v, %v", name, refs, ok)
		}
	}
	for name, data := range map[string]string{
		"repeated name":                 a + " refs/heads/work\n" + b + " refs/heads/work\n",
		"peel without a ref":            "^" + a + "\n",
		"second peel":                   b + " refs/tags/v1\n^" + a + "\n^" + a + "\n",
		"header not first":              a + " refs/heads/work\n# pack-refs with: peeled\n",
		"short object name":             "abc refs/heads/work\n",
		"name outside refs":             a + " HEAD\n",
		"no final newline":              a + " refs/heads/work",
		"declared sorted, out of order": header + b + " refs/tags/v1\n" + a + " refs/heads/work\n",
		"declared sorted, longer first": header + a + " refs/heads/work/x\n" + b + " refs/heads/work\n",
		"no space after the colon":      "# pack-refs with:peeled fully-peeled sorted \n" + a + " refs/heads/work\n",
		"another comment first":         "# written by hand\n" + a + " refs/heads/work\n",
		"unknown trait":                 "# pack-refs with: peeled future \n" + a + " refs/heads/work\n",
	} {
		if _, ok := packedRefs([]byte(data)); ok {
			t.Errorf("%s: packedRefs accepted %q", name, data)
		}
	}
}
