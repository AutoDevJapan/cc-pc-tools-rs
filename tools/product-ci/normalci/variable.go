package normalci

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Mutation func(context.Context, string, string, any, any) error
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func routeVariableRepository(ctx context.Context, read API) error {
	var r Repository
	if read(ctx, "repositories/1154066087", &r) != nil || r.ID != RepoID || r.FullName != "AutoDevJapan/GoDD-MCP-Alpha" || r.Owner.Login != "AutoDevJapan" || !r.Private || r.Fork || !r.Permissions.Admin {
		return errors.New("route variable repository identity rejected")
	}
	return nil
}

// Reading the bounded inventory distinguishes absence from a failed GET/404.
// Only this task's non-sensitive name/value are returned to the caller.
func RouteVariableState(ctx context.Context, read API) (Variable, bool, error) {
	var result Variable
	found := false
	total := -1
	seen := map[string]bool{}
	for page := 1; page <= 34; page++ {
		var r struct {
			Total     *int `json:"total_count"`
			Variables *[]Variable
		}
		if read(ctx, fmt.Sprintf("repos/AutoDevJapan/GoDD-MCP-Alpha/actions/variables?per_page=30&page=%d", page), &r) != nil {
			return result, false, errors.New("route variable inventory unavailable")
		}
		if r.Total == nil || r.Variables == nil {
			return result, false, errors.New("route variable response schema rejected")
		}
		count, variables := *r.Total, *r.Variables
		if count < 0 || count > 1000 || len(variables) > 30 || (total >= 0 && total != count) {
			return result, false, errors.New("route variable inventory changed")
		}
		total = count
		for _, v := range variables {
			key := strings.ToUpper(v.Name)
			if v.Name == "" || seen[key] {
				return result, false, errors.New("route variable inventory ambiguous")
			}
			seen[key] = true
			if strings.EqualFold(v.Name, RouteVariable) {
				result = v
				found = true
			}
		}
		if len(seen) >= total {
			if len(seen) != total {
				return result, false, errors.New("route variable count rejected")
			}
			return result, found, nil
		}
		if len(variables) != 30 {
			return result, false, errors.New("route variable inventory incomplete")
		}
	}
	return result, false, errors.New("route variable bound exceeded")
}

// Caller writes its exclusive durable intent BEFORE invoking this function.
// It never PATCHes/overwrites an existing variable. The nonce also reconciles a
// lost POST response without adopting another actor's existing flag.
func CreateRouteVariable(ctx context.Context, lease string, read API, write Mutation) error {
	value := LeaseValue(lease)
	if value == "" {
		return errors.New("route lease rejected")
	}
	if e := routeVariableRepository(ctx, read); e != nil {
		return e
	}
	if _, found, e := RouteVariableState(ctx, read); e != nil {
		return e
	} else if found {
		return errors.New("route variable already exists; use recorded recovery")
	}
	sent := write(ctx, "POST", "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/variables", Variable{Name: RouteVariable, Value: value}, nil)
	actual, found, e := RouteVariableState(ctx, read)
	if e != nil || !found || actual.Value != value {
		return errors.New("route creation uncertain; retain intent")
	}
	_ = sent // a lost reply is reconciled only by this fresh, unguessable nonce.
	return routeVariableRepository(ctx, read)
}
func RemoveRouteVariable(ctx context.Context, lease string, read API, write Mutation) error {
	value := LeaseValue(lease)
	if value == "" {
		return errors.New("route lease rejected")
	}
	if e := routeVariableRepository(ctx, read); e != nil {
		return e
	}
	actual, found, e := RouteVariableState(ctx, read)
	if e != nil {
		return e
	}
	if !found {
		return nil
	}
	if actual.Value != value {
		return errors.New("route variable ownership changed")
	}
	sent := write(ctx, "DELETE", "repos/AutoDevJapan/GoDD-MCP-Alpha/actions/variables/"+RouteVariable, nil, nil)
	_, found, e = RouteVariableState(ctx, read)
	if e != nil || found {
		return errors.New("route removal unverified; retain intent")
	}
	_ = sent
	return routeVariableRepository(ctx, read)
}
