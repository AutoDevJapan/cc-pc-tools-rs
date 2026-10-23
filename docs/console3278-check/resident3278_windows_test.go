package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The runtime implementation is the fixed public engine. Only these two
// compiled declarations may differ in this validation-only reader.
func TestResident3278RuntimeDiffIsCompiledDataOnly(t *testing.T) {
	for _, spec := range []struct{ file, declaration string }{
		{"model_update_windows.go", "model3081UpdateLeaves"},
		{"permanent_windows.go", "permanent3344Sources"},
	} {
		base, err := os.ReadFile(spec.file)
		if err != nil {
			t.Fatal(err)
		}
		overlay, err := os.ReadFile(filepath.Join("..", "docs", "console3278-check", spec.file))
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(`(?s)var ` + spec.declaration + ` = .*?\r?\n\}`)
		if len(re.FindAll(base, -1)) != 1 || len(re.FindAll(overlay, -1)) != 1 {
			t.Fatal("compiled declaration is missing or duplicated")
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), spec.file, overlay, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, item := range group.Specs {
				value, ok := item.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || value.Names[0].Name != spec.declaration {
					continue
				}
				ast.Inspect(value, func(node ast.Node) bool {
					switch node.(type) {
					case *ast.CallExpr, *ast.FuncLit:
						t.Error("compiled data must not execute a function")
					}
					return true
				})
			}
		}
		normalize := func(source []byte) []byte {
			t.Helper()
			out, err := format.Source(re.ReplaceAll(source, []byte("var "+spec.declaration+" = nil")))
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		if !bytes.Equal(normalize(base), normalize(overlay)) {
			t.Fatalf("runtime implementation changed outside %s", spec.declaration)
		}
	}
}

func TestResident3278OriginalProofsAndRootsStayExact(t *testing.T) {
	if len(model3081UpdateLeaves) != 1 || len(permanent3344Sources) != 2 {
		t.Fatal("scope exceeds one leaf and its two original source roots")
	}
	leaf := model3081UpdateLeaves[0]
	old := leaf.baselines[0].evidence
	if old[0] != (consoleEvidence{
		"D:/CC/Work/unattended-operations-20261013",
		"D:/CC/Work/unattended-operations-20261013/deploy-resident-2490/boot-final-acceptance-2492.json",
		"8a073f92d8de783d03fba1489081111baba2bdd0d2dce7158543bd1b433fb173",
	}) || old[1] != (consoleEvidence{
		"D:/CC/Work/unattended-operations-20261013",
		"D:/CC/Work/unattended-operations-20261013/deploy-resident-2490/refresh-boot-runtime-2492.ps1",
		"e4cb3c5634962fd38dd83c5ccb09916dac143e238aa1ef31cc13d89e5117c357",
	}) {
		t.Fatal("independent original baseline evidence changed")
	}
	root := "D:/CC/Work/unattended-operations-20261013-wt/todo-3278-console"
	owned := root + "/codex-owned-5fda5b483b11110e15979fa338534702"
	if leaf.candidate.root != root || leaf.candidate.path != owned+"/resident-guard.exe" ||
		leaf.candidate.evidence[0] != (consoleEvidence{root, owned + "/build-receipt.private.json", "faa997abb60341393c5abaccb57e00c6ec36c387a8d9d438bcb34b7354dfe928"}) ||
		permanent3344Sources[0] != (permanentSource{old[0].root, "cb47db4d325b026499fb20295158b1de8c66f427"}) ||
		permanent3344Sources[1] != (permanentSource{root, leaf.candidate.head}) {
		t.Fatal("fixed original candidate receipt or Git roots changed")
	}
}

func TestResident3278PurposeReceiptKeepsExistingContract(t *testing.T) {
	f := newModelUpdateFixture(t)
	f.leaf.kind = "resident"
	check := func(alreadyCurrent bool) {
		t.Helper()
		r := f.result(f.leaf.target)
		if r["qualified"] != true || r["domain"] != "model-runtime-update-3081-v1" || r["todo_id"] != 3081 ||
			r["target_kind"] != "resident" || r["data_access_granted"] != false || r["original_registry_recovered"] != false ||
			r["source_head"] != f.leaf.candidate.head || r["new_sha256"] != f.leaf.candidate.sha256 || r["already_current"] != alreadyCurrent {
			t.Fatal("resident data binding altered the existing purpose contract")
		}
	}
	check(false)
	if err := os.WriteFile(f.leaf.target, []byte("new model software"), 0600); err != nil {
		t.Fatal(err)
	}
	check(true)
	// Even current=new must reject if the independent original proof changes.
	if err := os.WriteFile(f.receipt, []byte("modified original proof"), 0600); err != nil {
		t.Fatal(err)
	}
	requireModelUpdateRefused(t, f.result(f.leaf.target))
}
