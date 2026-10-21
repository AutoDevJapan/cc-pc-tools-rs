package sourcepacket

import (
	"context"
	"fmt"
)

// VerifyRemote rechecks the compiled repository/PR/ref profile and the exact
// execution commit/tree/parents without transferring source or granting a job.
// The caller supplies its existing authenticated, bounded read-only API.
func VerifyRemote(ctx context.Context, pin Pin, read func(context.Context, string, any) error) error {
	if !validPin(pin) {
		return fixedErr()
	}
	r, stage, e := verifySourceIdentity(ctx, pin, read)
	if e != nil {
		return fmt.Errorf("source identity rejected at %s", stage)
	}
	var c commitResponse
	if read(ctx, "repos/"+r.FullName+"/git/commits/"+pin.HeadSHA, &c) != nil || c.SHA != pin.HeadSHA || c.Tree.SHA != pin.TreeSHA {
		return fixedErr()
	}
	if pin.PullHeadSHA != "" && (len(c.Parents) != 2 || c.Parents[0].SHA != pin.MergeBaseSHA || c.Parents[1].SHA != pin.PullHeadSHA) {
		return fixedErr()
	}
	return nil
}
