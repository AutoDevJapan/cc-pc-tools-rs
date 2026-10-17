package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// Parse Cargo's actual field shapes, including aliased dev/target dependencies.
func fixture(t *testing.T) metadata {
	t.Helper()
	root := t.TempDir()
	join := func(s string) string { return filepath.Join(root, s) }
	pkg := func(name string, deps []map[string]any, doc bool) map[string]any {
		return map[string]any{"id": name + "-id", "name": name, "manifest_path": join(name + "/Cargo.toml"), "dependencies": deps,
			"targets": []map[string]any{{"kind": []string{"lib"}, "doctest": doc}}}
	}
	raw, err := json.Marshal(map[string]any{
		"workspace_root": root, "workspace_members": []string{"core-id", "app-id", "consumer-id", "unrelated-id"},
		"packages": []map[string]any{
			pkg("core", nil, true),
			pkg("app", []map[string]any{{"name": "renamed", "kind": "dev", "path": join("core"), "target": "cfg(windows)"}}, false),
			pkg("consumer", []map[string]any{{"name": "app", "kind": "build", "path": join("app")}}, true),
			pkg("unrelated", nil, false),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var m metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAffectedClosure(t *testing.T) {
	m := fixture(t)
	cases := []struct {
		file string
		want []string
		docs []string
	}{
		{"core/src/lib.rs", []string{"app", "consumer", "core"}, []string{"consumer", "core"}},
		{"app/src/main.rs", []string{"app", "consumer"}, []string{"consumer"}},
		{"unrelated/src/lib.rs", []string{"unrelated"}, []string{}},
		// A prompt/fixture is an input, even though its extension is Markdown.
		{"core/prompts/owner.md", []string{"app", "consumer", "core"}, []string{"consumer", "core"}},
		{"docs/guide.md", []string{}, []string{}},
		{"unattended-runner/main.go", []string{}, []string{}},
	}
	for _, tt := range cases {
		t.Run(tt.file, func(t *testing.T) {
			p, err := selectPlan(m, []string{tt.file}, "")
			if err != nil || !reflect.DeepEqual(p.Packages, tt.want) || !reflect.DeepEqual(p.DocPackages, tt.docs) {
				t.Fatalf("plan=%+v error=%v; want=%v docs=%v", p, err, tt.want, tt.docs)
			}
			wantMode := "affected"
			if len(tt.want) == 0 {
				wantMode = "none"
			}
			if p.Mode != wantMode {
				t.Fatalf("mode=%q want=%q", p.Mode, wantMode)
			}
		})
	}
}

func TestFullReasons(t *testing.T) {
	for _, file := range []string{"Cargo.lock", "Cargo.toml", ".cargo/config.toml", "rust-toolchain.toml", ".config/nextest.toml", ".github/workflows/build-speed.yml", "tools/test-scope/main.go", "tools/linux-runner-bridge/main.go", "tools/product-ci/runner.go", "removed-member/Cargo.toml", "removed-member/src/lib.rs"} {
		t.Run(file, func(t *testing.T) {
			p, err := selectPlan(fixture(t), []string{file}, "")
			if err != nil || p.Mode != "full" || len(p.Packages) != 4 || len(p.Reasons) != 1 {
				t.Fatalf("plan=%+v error=%v", p, err)
			}
		})
	}
	p, err := selectPlan(fixture(t), nil, "comparison_base_unavailable")
	if err != nil || p.Mode != "full" || len(p.Packages) != 4 {
		t.Fatalf("missing comparison skipped tests: %+v %v", p, err)
	}
}

func TestCyclesAndRenamesRetainBothSides(t *testing.T) {
	m := fixture(t)
	m.Packages[0].Deps = m.Packages[2].Deps // core -> app -> core dev-dependency cycle
	p, err := selectPlan(m, []string{"core/old.rs", "unrelated/new.rs"}, "")
	if err != nil || len(p.Packages) != 4 || p.Mode != "affected" {
		t.Fatalf("cycle or rename lost coverage: %+v %v", p, err)
	}
}

func TestInvalidInputFailsInsteadOfSelectingNothing(t *testing.T) {
	for _, file := range []string{"../escape.rs", "/absolute.rs", "core/../other.rs", "", `core\src\lib.rs`} {
		if _, err := selectPlan(fixture(t), []string{file}, ""); err == nil {
			t.Fatalf("invalid Git path accepted: %q", file)
		}
	}
	m := fixture(t)
	m.Members = append(m.Members, "missing-id")
	if _, err := selectPlan(m, []string{"docs/guide.md"}, ""); err == nil {
		t.Fatal("incomplete metadata selected no tests")
	}
	m = fixture(t)
	m.Packages[0].Name = "--workspace"
	if _, err := selectPlan(m, []string{"core/src/lib.rs"}, ""); err == nil {
		t.Fatal("package name could become a Cargo flag")
	}
}
