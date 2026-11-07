package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const ciRepository = "AutoDevJapan/cc-pc-tools-rs"

var hostedWorkflows = []struct{ file, job string }{
	{"build-speed.yml", "cc-jobs"},
	{"channel-reports.yml", "verify"},
	{"bookmark-queue.yml", "test"},
	{"chatgpt-web.yml", "fixtures"},
}

func workflowSteps(t *testing.T, job map[string]any) []map[string]any {
	t.Helper()
	var steps []map[string]any
	for _, value := range job["steps"].([]any) {
		steps = append(steps, value.(map[string]any))
	}
	return steps
}

func namedWorkflowStep(t *testing.T, steps []map[string]any, name string) (int, map[string]any) {
	t.Helper()
	for i, step := range steps {
		if step["name"] == name {
			return i, step
		}
	}
	t.Fatalf("required step %q is missing", name)
	return -1, nil
}

func TestHostedRoutingRetainsPrivateAndSourceBoundaries(t *testing.T) {
	for _, target := range hostedWorkflows {
		t.Run(target.file, func(t *testing.T) {
			w := buildSpeedWorkflow(t, filepath.Join("..", "..", ".github", "workflows", target.file))
			if !reflect.DeepEqual(w["permissions"], map[string]any{"contents": "read"}) {
				t.Fatal("workflow must retain read-only repository permissions")
			}
			job := w["jobs"].(map[string]any)[target.job].(map[string]any)
			ctx := map[string]any{"github.repository": ciRepository, "github.event_name": "pull_request"}
			for _, source := range []struct {
				repo, head string
				allowed    bool
			}{
				{ciRepository, ciRepository, true},
				{ciRepository, "fork/repo", false},
				{ciRepository, "", false},
				{"other/repo", "other/repo", false},
			} {
				ctx["github.repository"], ctx["github.event.pull_request.head.repo.full_name"] = source.repo, source.head
				if got := actionsTruthy(actionsExpr(t, job["if"].(string), ctx)); got != source.allowed {
					t.Fatalf("source admission=%v for repo=%q head=%q", got, source.repo, source.head)
				}
			}
			rows := []any{map[string]any{"os": "Windows", "hosted": "windows-2025", "labels": []any{"self-hosted", "Windows", "X64", "cc-pc-tools-windows"}}}
			if strategy, ok := job["strategy"].(map[string]any); ok {
				rows = strategy["matrix"].(map[string]any)["include"].([]any)
				if len(rows) != 2 {
					t.Fatal("both Windows and Linux checks must remain")
				}
			}
			steps := workflowSteps(t, job)
			pcIndex, pc := namedWorkflowStep(t, steps, "Admit current Windows source before checkout")
			hostedIndex, hosted := namedWorkflowStep(t, steps, "Admit current hosted source before checkout")
			rustIndex, rust := namedWorkflowStep(t, steps, "Prepare isolated hosted Rust tools")
			checkoutIndex := -1
			for i, step := range steps {
				uses, _ := step["uses"].(string)
				if strings.HasPrefix(uses, "actions/checkout@") {
					checkoutIndex = i
					with := step["with"].(map[string]any)
					if with["ref"] != "${{ github.event.pull_request.head.sha || github.sha }}" || with["persist-credentials"] != false {
						t.Fatal("checkout must use the exact source head without saved credentials")
					}
				}
				if step["continue-on-error"] == true || strings.HasPrefix(uses, "actions/cache@") || strings.HasPrefix(uses, "actions/upload-artifact@") {
					t.Fatal("checks must fail normally without adding cache or artifact storage")
				}
			}
			if checkoutIndex < 0 || pcIndex >= checkoutIndex || hostedIndex >= checkoutIndex || rustIndex <= checkoutIndex {
				t.Fatal("admission must run before source checkout; Rust setup follows it")
			}
			if hosted["shell"] != "pwsh" || hosted["working-directory"] != "${{ runner.temp }}" {
				t.Fatal("hosted admission must not depend on a repository working directory")
			}
			wantEnv := map[string]any{
				"GH_TOKEN": "${{ github.token }}", "CI_REPOSITORY": "${{ github.repository }}",
				"CI_BRANCH": "${{ github.head_ref || github.ref_name }}", "CI_SOURCE_SHA": "${{ github.event.pull_request.head.sha || github.sha }}",
			}
			if !reflect.DeepEqual(hosted["env"], wantEnv) || !strings.Contains(pc["run"].(string), "$env:PC_CI_ADMISSION admit") {
				t.Fatal("hosted and PC admission must retain their separate trusted inputs")
			}
			for _, row := range rows {
				matrix := row.(map[string]any)
				osName := matrix["os"].(string)
				hostedLabel := map[string]string{"Windows": "windows-2025", "Linux": "ubuntu-24.04"}[osName]
				if hostedLabel == "" || matrix["hosted"] != hostedLabel {
					t.Fatalf("unexpected hosted image for %q", osName)
				}
				wantPC := []any{"self-hosted", osName, "X64", map[string]string{"Windows": "cc-pc-tools-windows", "Linux": "cc-pc-tools-linux"}[osName]}
				if !reflect.DeepEqual(matrix["labels"], wantPC) {
					t.Fatal("private fallback must retain the original runner labels")
				}
				ctx["matrix.hosted"], ctx["matrix.labels"] = hostedLabel, matrix["labels"]
				for _, privacy := range []struct {
					value  any
					public bool
				}{
					{false, true}, {true, false}, {nil, false}, {"false", false}, {"true", false}, {0, false}, {1, false}, {"", false},
				} {
					ctx["github.event.repository.private"] = privacy.value
					want := any(wantPC)
					if privacy.public {
						want = hostedLabel
					}
					if got := actionsExpr(t, job["runs-on"].(string), ctx); !reflect.DeepEqual(got, want) {
						t.Fatalf("privacy=%#v routes to %#v, want %#v", privacy.value, got, want)
					}
				}
				for _, environment := range []string{"github-hosted", "self-hosted"} {
					ctx["runner.environment"], ctx["runner.os"] = environment, osName
					isHosted := environment == "github-hosted"
					if actionsTruthy(actionsExpr(t, pc["if"].(string), ctx)) != (!isHosted && osName == "Windows") ||
						actionsTruthy(actionsExpr(t, hosted["if"].(string), ctx)) != isHosted ||
						actionsTruthy(actionsExpr(t, rust["if"].(string), ctx)) != isHosted {
						t.Fatal("hosted must never invoke PC admission; private runners keep their admission")
					}
				}
			}
			rustScript := rust["run"].(string)
			for _, required := range []string{"rustup toolchain install 1.95.0 --profile minimal --component clippy", "RUSTUP_TOOLCHAIN=1.95.0", "CARGO_INCREMENTAL=0", "CARGO_PROFILE_DEV_DEBUG=0", "CARGO_PROFILE_TEST_DEBUG=0"} {
				if !strings.Contains(rustScript, required) {
					t.Fatalf("hosted Rust is missing %q", required)
				}
			}
		})
	}
}

// Run the actual pre-checkout scripts with a local gh fixture. Stale heads and
// failed API lookups must fail even when an API error happens to return the head.
func TestHostedAdmissionRejectsStaleOrUnavailableHeads(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Fatal("PowerShell is required to exercise the hosted admission script")
	}
	for _, target := range hostedWorkflows {
		w := buildSpeedWorkflow(t, filepath.Join("..", "..", ".github", "workflows", target.file))
		job := w["jobs"].(map[string]any)[target.job].(map[string]any)
		_, step := namedWorkflowStep(t, workflowSteps(t, job), "Admit current hosted source before checkout")
		for _, fixture := range []struct {
			name, head, exit string
			admitted         bool
		}{
			{"current", strings.Repeat("a", 40), "0", true},
			{"stale", strings.Repeat("b", 40), "0", false},
			{"API failure with current head", strings.Repeat("a", 40), "7", false},
			{"missing head", "", "0", false},
		} {
			t.Run(target.file+"/"+fixture.name, func(t *testing.T) {
				script := `$ErrorActionPreference = 'Stop'
$script:observedCall = @()
function gh {
  $script:observedCall = @($args)
  $global:LASTEXITCODE = [int]$env:FIXTURE_GH_EXIT
  $env:FIXTURE_GH_HEAD
}
try {
  & {
` + step["run"].(string) + `
  }
  $expected = @('api', '--method', 'GET', 'repos/AutoDevJapan/cc-pc-tools-rs/git/ref/heads/topic%2Fa%20%23b%3F', '--jq', '.object.sha')
  if (($script:observedCall | ConvertTo-Json -Compress) -ne ($expected | ConvertTo-Json -Compress)) { exit 90 }
  'fixture-admitted'
} catch { exit 1 }
`
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, pwsh, "-NoProfile", "-NonInteractive", "-Command", script)
				hideWindow(cmd)
				cmd.WaitDelay = time.Second
				cmd.Env = append(os.Environ(), "GH_TOKEN=fixture-read-token", "CI_REPOSITORY="+ciRepository,
					"CI_BRANCH=topic/a #b?", "CI_SOURCE_SHA="+strings.Repeat("a", 40), "FIXTURE_GH_HEAD="+fixture.head, "FIXTURE_GH_EXIT="+fixture.exit)
				out, err := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatal("admission fixture exceeded its deadline")
				}
				if (err == nil) != fixture.admitted || (fixture.admitted && !strings.Contains(string(out), "fixture-admitted")) {
					t.Fatalf("admitted=%v, want %v; output=%s", err == nil, fixture.admitted, out)
				}
			})
		}
	}
}

func TestAncillaryWorkflowVerificationIsPreserved(t *testing.T) {
	for _, target := range []struct {
		file, job string
		runs      []string
	}{
		{"channel-reports.yml", "verify", []string{
			"cargo test --locked -p cc-channel-watch",
			"cargo test --locked -p cc-chrome -p cc-job-policy",
			"cargo test --locked -p cc-jobs --test parity registrar_launch_policy",
			"cargo test --locked -p cc-ask --test channel_reports --test commands --test ask_dedup",
			"cargo clippy --locked -p cc-ask -p cc-channel-watch -p cc-chrome -p cc-job-policy --all-targets -- -D warnings",
		}},
		{"bookmark-queue.yml", "test", []string{
			"cargo fmt --manifest-path cc-bookmark-queue/Cargo.toml --check",
			"cargo test --locked -p cc-bookmark-queue",
			"cargo clippy --locked -p cc-bookmark-queue --all-targets -- -D warnings",
			"node --test cc-bookmark-queue/tests/*.test.js",
			"cargo test --locked -p cc-ask --test commands --test resume",
		}},
	} {
		t.Run(target.file, func(t *testing.T) {
			w := buildSpeedWorkflow(t, filepath.Join("..", "..", ".github", "workflows", target.file))
			job := w["jobs"].(map[string]any)[target.job].(map[string]any)
			steps := workflowSteps(t, job)
			var runs []string
			for _, step := range steps {
				if run, ok := step["run"].(string); ok && (strings.HasPrefix(run, "cargo ") || strings.HasPrefix(run, "node ")) {
					runs = append(runs, run)
					if strings.Contains(run, "-p cc-jobs") || strings.Contains(run, "--test resume") {
						if step["if"] != "runner.os == 'Windows'" {
							t.Fatal("Windows-only tests must remain on Windows")
						}
					}
				}
			}
			if !reflect.DeepEqual(runs, target.runs) {
				t.Fatalf("verification commands changed: %v", runs)
			}
			c := w["concurrency"].(map[string]any)
			if c["group"] != "${{ github.workflow }}-pr-head-v2-${{ github.event_name == 'pull_request' && github.run_attempt == 1 && github.event.pull_request.number || github.run_id }}" ||
				c["cancel-in-progress"] != "${{ github.event_name == 'pull_request' && github.run_attempt == 1 }}" {
				t.Fatal("main and reruns must retain their existing concurrency isolation")
			}
			if target.file == "bookmark-queue.yml" {
				var node22 bool
				for _, step := range steps {
					if uses, _ := step["uses"].(string); strings.HasPrefix(uses, "actions/setup-node@") {
						node22 = step["with"].(map[string]any)["node-version"] == "22"
					}
				}
				_, rust := namedWorkflowStep(t, steps, "Prepare isolated hosted Rust tools")
				if !node22 || !strings.Contains(rust["run"].(string), "--component rustfmt") {
					t.Fatal("Node 22 and rustfmt are required for the original checks")
				}
			}
		})
	}
}
