package normalci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNonceVariableOwnershipAndLostResponse(t *testing.T) {
	ctx := context.Background()
	lease := strings.Repeat("a", 32)
	var value *Variable
	writes := 0
	failRead := false
	read := func(_ context.Context, key string, out any) error {
		if failRead {
			return errors.New("offline")
		}
		var data any
		if key == "repositories/1154066087" {
			data = map[string]any{"id": RepoID, "full_name": "AutoDevJapan/GoDD-MCP-Alpha", "private": true, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
		} else {
			vs := []Variable{}
			if value != nil {
				vs = append(vs, *value)
			}
			data = map[string]any{"total_count": len(vs), "variables": vs}
		}
		b, _ := json.Marshal(data)
		return json.Unmarshal(b, out)
	}
	write := func(_ context.Context, method, _ string, body, out any) error {
		writes++
		if method == "POST" {
			v, ok := body.(Variable)
			if !ok {
				return errors.New("invalid body")
			}
			value = &v
			return errors.New("reply lost after create")
		}
		if method == "DELETE" {
			value = nil
			return errors.New("reply lost after delete")
		}
		return errors.New("unapproved mutation")
	}
	if e := CreateRouteVariable(ctx, lease, read, write); e != nil || writes != 1 || value == nil || value.Value != LeaseValue(lease) {
		t.Fatal("nonce-owned lost creation not reconciled")
	}
	if e := CreateRouteVariable(ctx, lease, read, write); e == nil || writes != 1 {
		t.Fatal("existing flag overwritten")
	}
	if e := RemoveRouteVariable(ctx, strings.Repeat("b", 32), read, write); e == nil || writes != 1 {
		t.Fatal("another lease deleted")
	}
	failRead = true
	if e := RemoveRouteVariable(ctx, lease, read, write); e == nil || writes != 1 {
		t.Fatal("read failure treated as absence")
	}
	failRead = false
	if e := RemoveRouteVariable(ctx, lease, read, write); e != nil || writes != 2 || value != nil {
		t.Fatal("nonce-owned lost deletion not reconciled")
	}
	if e := RemoveRouteVariable(ctx, lease, read, write); e != nil || writes != 2 {
		t.Fatal("already absent flag mutated")
	}
	b, _ := json.Marshal(Variable{Name: RouteVariable, Value: LeaseValue(lease)})
	if !strings.Contains(string(b), `"name"`) || !strings.Contains(string(b), `"value"`) {
		t.Fatal("GitHub body schema changed")
	}
}

func TestRouteVariablePaginationAndMissingSchema(t *testing.T) {
	for _, fault := range []string{"", "duplicate", "missing-total", "null-array"} {
		t.Run(fault, func(t *testing.T) {
			pages := 0
			read := func(_ context.Context, key string, out any) error {
				pages++
				items := []Variable{}
				total := 31
				if strings.HasSuffix(key, "page=1") {
					for n := 0; n < 30; n++ {
						items = append(items, Variable{Name: fmt.Sprintf("VAR_%d", n), Value: "nonsensitive"})
					}
				} else {
					items = append(items, Variable{Name: strings.ToLower(RouteVariable), Value: LeaseValue(strings.Repeat("a", 32))})
					if fault == "duplicate" {
						items[0].Name = "var_0"
					}
				}
				data := map[string]any{"total_count": total, "variables": items}
				if fault == "missing-total" {
					delete(data, "total_count")
				}
				if fault == "null-array" {
					data["variables"] = nil
				}
				b, _ := json.Marshal(data)
				return json.Unmarshal(b, out)
			}
			v, found, e := RouteVariableState(context.Background(), read)
			if fault == "" {
				if e != nil || !found || pages != 2 || v.Value != LeaseValue(strings.Repeat("a", 32)) {
					t.Fatal("bounded case-insensitive inventory rejected")
				}
			} else if e == nil {
				t.Fatal("ambiguous/missing variable inventory accepted")
			}
		})
	}
}
func TestRoleRunLabelAndLeaseExpression(t *testing.T) {
	lease := strings.Repeat("c", 32)
	seen := map[string]bool{}
	for _, r := range Routes() {
		label := RuntimeLabel(r.Job, 17)
		if label == "" || seen[label] || label == RuntimeLabel(r.Job, 18) {
			t.Fatal("run/role labels collide")
		}
		seen[label] = true
		expr, e := RunnerExpressionForLease(r, lease)
		if e != nil || !strings.Contains(expr, "github.run_id") || !strings.Contains(expr, "'"+LeaseValue(lease)+"'") || !strings.HasSuffix(expr, "|| '"+r.Previous+"' }}") {
			t.Fatal("conditional original-OS fallback lost")
		}
	}
	if RuntimeLabel("unapproved", 17) != "" || RuntimeLabel("verify", 0) != "" {
		t.Fatal("unapproved job selected")
	}
	if _, e := RunnerExpressionForLease(Routes()[0], "quote'"); e == nil {
		t.Fatal("expression injection accepted")
	}
	if RuntimeLabel("windows-native-check", 17) != "cc-product-2737-1154066087-windows-windows-native-check-17" {
		t.Fatal("Windows backend protocol changed")
	}
}
