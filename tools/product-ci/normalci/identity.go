// Package normalci contains the existing read-only admission policy. Both CLI
// and guest controller link this same implementation; requests select no code.
package normalci

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Candidate struct {
	ID  int64
	PR  int
	SHA string
}
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private, Fork bool
	Owner         struct{ Login string }
	Permissions   struct{ Admin bool }
}
type Pull struct {
	State string
	Head  struct {
		SHA, Ref string
		Repo     Repository
	}
	Base struct{ Repo Repository }
}
type API func(context.Context, string, any) error

func Verify(ctx context.Context, c Candidate, read API) (Repository, error) {
	var r Repository
	if e := read(ctx, fmt.Sprintf("repositories/%d", c.ID), &r); e != nil {
		return r, e
	}
	if r.ID != c.ID || !r.Private || r.Fork || !r.Permissions.Admin ||
		(r.Owner.Login != "AutoDevJapan" && r.Owner.Login != "BM1314" && r.Owner.Login != "mashfromband") ||
		!strings.HasPrefix(r.FullName, r.Owner.Login+"/") || strings.Count(r.FullName, "/") != 1 {
		return Repository{}, errors.New("repository identity rejected")
	}
	var p Pull
	if e := read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &p); e != nil {
		return Repository{}, e
	}
	if p.State != "open" || p.Head.SHA != c.SHA || p.Head.Repo.ID != c.ID || p.Base.Repo.ID != c.ID ||
		p.Head.Repo.FullName != r.FullName || p.Base.Repo.FullName != r.FullName || p.Head.Repo.Fork {
		return Repository{}, errors.New("fixed same-repository PR head rejected")
	}
	var ref struct{ Object struct{ SHA string } }
	// The PR API and branch API must agree; the fixed revision never follows a ref.
	if e := read(ctx, "repos/"+r.FullName+"/git/ref/heads/"+url.PathEscape(p.Head.Ref), &ref); e != nil {
		return Repository{}, e
	}
	if ref.Object.SHA != c.SHA {
		return Repository{}, errors.New("branch head changed")
	}
	return r, nil
}
