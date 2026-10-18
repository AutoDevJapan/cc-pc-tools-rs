package pr148scope

import (
	"errors"
	"fmt"
	"strings"
)

func ValidSHA(s string) bool { return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == "" }

type TreeEntry struct{ Path, Mode, Type, SHA string }
type TreeReply struct {
	SHA       string
	Truncated bool
	Entries   []TreeEntry `json:"tree"`
}

// Compare the entire recursive index, including modes, submodules and empty
// directories. Only three blobs and their ancestor tree IDs may differ.
func CheckTreeDelta(old, next TreeReply, expected map[string]string) error {
	if old.Truncated || next.Truncated || old.SHA != Tree || !ValidSHA(next.SHA) || len(old.Entries) != len(next.Entries) || len(expected) != 3 {
		return errors.New("complete fixed tree comparison unavailable")
	}
	prior := map[string]TreeEntry{}
	for _, x := range old.Entries {
		if x.Path == "" || !ValidSHA(x.SHA) {
			return errors.New("invalid original tree entry")
		}
		if _, ok := prior[x.Path]; ok {
			return errors.New("duplicate original tree entry")
		}
		prior[x.Path] = x
	}
	changed := 0
	for _, y := range next.Entries {
		x, ok := prior[y.Path]
		if !ok || x.Mode != y.Mode || x.Type != y.Type || !ValidSHA(y.SHA) {
			return fmt.Errorf("unexpected tree entry or mode: %s", y.Path)
		}
		delete(prior, y.Path)
		if want, ok := expected[y.Path]; ok {
			if y.Type != "blob" || y.SHA != want || y.SHA == x.SHA {
				return fmt.Errorf("proposal bytes differ: %s", y.Path)
			}
			changed++
		} else if x.SHA != y.SHA && !(y.Type == "tree" && (y.Path == ".github" || y.Path == ".github/workflows")) {
			return fmt.Errorf("unexpected source modification: %s", y.Path)
		}
	}
	if len(prior) != 0 || changed != 3 {
		return errors.New("proposal tree coverage differs")
	}
	return nil
}
