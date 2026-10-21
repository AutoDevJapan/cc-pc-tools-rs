package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"strings"
	"testing"
)

func TestPressCheckoutOwnerGrantKeepsEveryOtherPrivateSDKGrant(t *testing.T) {
	p := sourcepacket.Pin{Product: sourcepacket.PressProduct, Workflow: "windows-hidden-fixtures.yml"}
	before := runnerAccessCommands()
	after, e := runnerAccessCommandsFor(p)
	if e != nil || len(before) != len(after) {
		t.Fatal("Press ACL policy rejected")
	}
	changed := 0
	for i, row := range before {
		for j, word := range row {
			if after[i][j] != word {
				changed++
				if j != 0 || word != sourceRoot || after[i][j] != sourceRootFor(p) || row[1] != "/setowner" {
					t.Fatal("Press widened an existing grant")
				}
			}
		}
	}
	if changed != 1 || !strings.Contains(sourceRootFor(p), `\GoDD-Press\GoDD-Press`) {
		t.Fatal("checkout owner path not exactly selected")
	}
	p.Product = "arbitrary"
	if _, e := runnerAccessCommandsFor(p); e == nil {
		t.Fatal("unknown ACL profile accepted")
	}
}
