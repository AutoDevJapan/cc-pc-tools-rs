package officialci3893

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CC_3893_FIXTURE_ROOT") != "" {
		os.Exit(m.Run())
	}
	// Formal CI already owns its job's TEMP. Use one new ASCII child, instead
	// of putting an intentionally invalid/non-ASCII archive test name in the
	// trusted native parent path. PC development supplies a safe-delete anchor.
	root, e := os.MkdirTemp(os.TempDir(), "official-ci3894-synthetic-")
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot create synthetic fixture anchor")
		os.Exit(1)
	}
	if e = os.Setenv("CC_3893_FIXTURE_ROOT", root); e != nil {
		fmt.Fprintln(os.Stderr, "cannot bind synthetic fixture anchor")
		os.Exit(1)
	}
	code := m.Run()
	if e = os.RemoveAll(root); e != nil {
		fmt.Fprintln(os.Stderr, "synthetic fixture cleanup failed")
		code = 1
	}
	os.Exit(code)
}
