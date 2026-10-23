package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"cc/product-ci/inputqual"
)

func successorCLI(ctx context.Context, args []string) error {
	qualify := len(args) == 1 && args[0] == "current-pr-successor-qualify"
	inspect := len(args) == 1 && args[0] == "current-pr-successor-inspect"
	job := (len(args) == 3 || len(args) == 4) && args[0] == "current-pr-successor-job"
	if !qualify && !inspect && !job {
		return errors.New("current-pr-successor-qualify/inspect | current-pr-successor-job <new-owned-dir> <Linux-helper> [gitleaks-ELF]")
	}
	s, err := inputqual.ConstructSuccessor()
	if err != nil {
		return err
	}
	b, err := consumeCurrentPRSnapshot(s)
	if err != nil {
		return err
	}
	if job && ((b.Kind == "secret" && len(args) != 4) || (b.Kind == "codeql" && len(args) != 3)) {
		return errors.New("successor execution arguments rejected")
	}
	if qualify {
		return json.NewEncoder(os.Stdout).Encode(s.Report())
	}
	p, err := planCurrentPR(ctx, b, get)
	if err != nil {
		return err
	}
	if err = checkCurrentPRReady(ctx, p, get); err != nil {
		return err
	}
	if err = checkCodeQLQueue(ctx, p, get); err != nil {
		return err
	}
	if inspect {
		return json.NewEncoder(os.Stdout).Encode(p)
	}
	scanner := ""
	if len(args) == 4 {
		scanner = args[3]
	}
	return executeProductPlan(ctx, structCandidate(p), p, args[1], args[2], scanner)
}
