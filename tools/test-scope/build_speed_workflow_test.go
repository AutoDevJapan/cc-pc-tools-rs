package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func buildSpeedWorkflow(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func TestBuildSpeedPreservesWorkflowBody(t *testing.T) {
	before := buildSpeedWorkflow(t, "testdata/build-speed.yml")
	after := buildSpeedWorkflow(t, filepath.Join("..", "..", ".github", "workflows", "build-speed.yml"))
	delete(before, "concurrency")
	delete(after, "concurrency")
	// The baseline records the runner, admission, checkout, selection and test
	// contract. Update it deliberately when that contract changes.
	if !reflect.DeepEqual(before, after) {
		t.Fatal("workflow changed outside concurrency; review and update the body contract")
	}
}

func TestBuildSpeedConcurrency(t *testing.T) {
	w := buildSpeedWorkflow(t, filepath.Join("..", "..", ".github", "workflows", "build-speed.yml"))
	c := w["concurrency"].(map[string]any)
	job := w["jobs"].(map[string]any)["cc-jobs"].(map[string]any)
	const owner = "AutoDevJapan/cc-pc-tools-rs"
	for _, tt := range []struct {
		name, event, ref, attempt, run, pr, repository, headRepo, group string
		cancel, admitted                                                bool
	}{
		{"main first", "push", "refs/heads/main", "1", "1001", "", owner, "", "main", true, true},
		{"main successor", "push", "refs/heads/main", "1", "1002", "", owner, "", "main", true, true},
		{"old main rerun", "push", "refs/heads/main", "2", "1001", "", owner, "", "1001", false, true},
		{"main later rerun", "push", "refs/heads/main", "3", "1002", "", owner, "", "1002", false, true},
		{"PR first", "pull_request", "refs/pull/42/merge", "1", "2001", "42", owner, owner, "42", true, true},
		{"PR successor", "pull_request", "refs/pull/42/merge", "1", "2002", "42", owner, owner, "42", true, true},
		{"other PR", "pull_request", "refs/pull/43/merge", "1", "2003", "43", owner, owner, "43", true, true},
		{"PR rerun", "pull_request", "refs/pull/42/merge", "2", "2001", "42", owner, owner, "2001", false, true},
		{"fork PR", "pull_request", "refs/pull/44/merge", "1", "2004", "44", owner, "fork/repo", "44", true, false},
		{"missing PR repo", "pull_request", "refs/pull/45/merge", "1", "2005", "45", owner, "", "45", true, false},
		{"wrong owner", "push", "refs/heads/main", "1", "3001", "", "other/repo", "", "main", true, false},
		{"dispatch", "workflow_dispatch", "refs/heads/main", "1", "4001", "", owner, "", "4001", false, true},
		{"schedule", "schedule", "refs/heads/main", "1", "4002", "", owner, "", "4002", false, true},
		{"other branch", "push", "refs/heads/feature", "1", "4003", "", owner, "", "4003", false, true},
		{"tag", "push", "refs/tags/main", "1", "4004", "", owner, "", "4004", false, true},
		{"PR target", "pull_request_target", "refs/heads/main", "1", "4005", "42", owner, owner, "4005", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := map[string]any{
				"github.workflow": "Build speed", "github.event_name": tt.event,
				"github.ref": tt.ref, "github.run_attempt": tt.attempt,
				"github.run_id": tt.run, "github.event.pull_request.number": tt.pr,
				"github.repository": tt.repository, "github.event.pull_request.head.repo.full_name": tt.headRepo,
			}
			if got := expandActions(t, c["group"].(string), ctx); got != "Build speed-pr-head-v2-"+tt.group {
				t.Errorf("group=%q, want suffix %q", got, tt.group)
			}
			if got := actionsTruthy(actionsExpr(t, c["cancel-in-progress"].(string), ctx)); got != tt.cancel {
				t.Errorf("cancel=%v, want %v", got, tt.cancel)
			}
			// This is the existing job guard, not the event trigger or the
			// authoritative pc-ci admission. Unsupported events stay unsupported.
			if got := actionsTruthy(actionsExpr(t, job["if"].(string), ctx)); got != tt.admitted {
				t.Errorf("repository job guard=%v, want %v", got, tt.admitted)
			}
			ctx["github.workflow"] = "Other workflow"
			if got := expandActions(t, c["group"].(string), ctx); got != "Other workflow-pr-head-v2-"+tt.group {
				t.Errorf("workflow groups collide: %q", got)
			}
		})
	}
}

// Evaluate only the expression subset used above: literals, property lookup,
// ==, !=, &&, || and parentheses. Go's parser supplies syntax and precedence;
// logical operators return operands and equality coerces numbers as Actions
// specifies. Unsupported syntax fails the test rather than passing silently.
// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions
func actionsExpr(t *testing.T, input string, ctx map[string]any) any {
	t.Helper()
	input = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(input), "${{"), "}}"))
	var converted strings.Builder
	for i := 0; i < len(input); i++ {
		if input[i] != '\'' {
			if input[i] == '"' || input[i] == '`' {
				t.Fatal("unsupported Actions quoting")
			}
			converted.WriteByte(input[i])
			continue
		}
		var literal strings.Builder
		closed := false
		for i++; i < len(input); i++ {
			if input[i] == '\'' {
				if i+1 < len(input) && input[i+1] == '\'' {
					literal.WriteByte('\'')
					i++
					continue
				}
				closed = true
				break
			}
			literal.WriteByte(input[i])
		}
		if !closed {
			t.Fatal("unterminated Actions string")
		}
		converted.WriteString(strconv.Quote(literal.String()))
	}
	node, err := parser.ParseExpr(converted.String())
	if err != nil {
		t.Fatal(err)
	}
	var path func(ast.Expr) string
	path = func(n ast.Expr) string {
		switch x := n.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			return path(x.X) + "." + x.Sel.Name
		default:
			t.Fatalf("unsupported property: %T", n)
			return ""
		}
	}
	var eval func(ast.Expr) any
	eval = func(n ast.Expr) any {
		switch x := n.(type) {
		case *ast.ParenExpr:
			return eval(x.X)
		case *ast.Ident:
			if x.Name == "true" || x.Name == "false" {
				return x.Name == "true"
			}
			if x.Name == "null" {
				return nil
			}
		case *ast.SelectorExpr:
			return ctx[path(x)]
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, err := strconv.Unquote(x.Value)
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			if x.Kind == token.INT {
				v, err := strconv.ParseFloat(x.Value, 64)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
		case *ast.BinaryExpr:
			left := eval(x.X)
			switch x.Op {
			case token.LAND:
				if !actionsTruthy(left) {
					return left
				}
				return eval(x.Y)
			case token.LOR:
				if actionsTruthy(left) {
					return left
				}
				return eval(x.Y)
			case token.EQL, token.NEQ:
				right := eval(x.Y)
				var equal bool
				if l, ok := left.(string); ok {
					if r, ok := right.(string); ok {
						equal = strings.EqualFold(l, r)
					} else {
						equal = actionsNumber(left) == actionsNumber(right)
					}
				} else {
					equal = actionsNumber(left) == actionsNumber(right)
				}
				return equal == (x.Op == token.EQL)
			}
		}
		t.Fatalf("unsupported Actions expression: %T", n)
		return nil
	}
	return eval(node)
}

func actionsNumber(v any) float64 {
	if v == nil || v == false || v == "" {
		return 0
	}
	if v == true {
		return 1
	}
	n, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	if err != nil {
		return math.NaN()
	}
	return n
}

func actionsTruthy(v any) bool {
	return v != nil && v != false && v != float64(0) && v != ""
}

func expandActions(t *testing.T, input string, ctx map[string]any) string {
	t.Helper()
	var result strings.Builder
	for {
		start := strings.Index(input, "${{")
		if start < 0 {
			return result.String() + input
		}
		end := strings.Index(input[start+3:], "}}")
		if end < 0 {
			t.Fatal("unterminated Actions interpolation")
		}
		end += start + 3
		result.WriteString(input[:start])
		result.WriteString(fmt.Sprint(actionsExpr(t, input[start+3:end], ctx)))
		input = input[end+2:]
	}
}

func TestActionsExpressionFixtureSemantics(t *testing.T) {
	for expression, want := range map[string]any{
		"true && 'chosen' || 'fallback'":  "chosen",
		"false && 'chosen' || 'fallback'": "fallback",
		"'false' && 'truthy'":             "truthy",
		"'1' == 1":                        true,
		"'MAIN' == 'main'":                true,
		"'word' == 0":                     false,
		"'it''s'":                         "it's",
	} {
		if got := actionsExpr(t, expression, nil); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", expression, got, want)
		}
	}
}
