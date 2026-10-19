// passshare-ci-windows prepares reviewed workflow bytes. It never sets statuses.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"cc/passshare-ci-windows/internal/contract"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) != 4 || args[0] != "propose" {
		return errors.New("usage: passshare-ci-windows propose <fixed-desktop.yml> <current-main-quality-gate.yml> <new-candidate.yml>")
	}
	before, e := os.ReadFile(args[1])
	if e != nil {
		return e
	}
	qg, e := os.ReadFile(args[2])
	if e != nil {
		return e
	}
	candidate, e := contract.Propose(before, qg)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(candidate)
	e = errors.Join(e, f.Sync(), f.Close())
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"workflow": contract.Workflow, "sha256": contract.Digest(candidate), "candidate_only": true, "official_success": false})
}
