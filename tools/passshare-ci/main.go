// Generate the two scoped PassShare workflow changes from reviewed API bytes.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

type object = map[string]any

const checkout = "actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0"

// Every integration file starts an app and runs migrations against the same
// database. Keep the Compose command as the source of its complete test list.
const serialE2E = `set -euo pipefail
config="$(docker compose -f docker/docker-compose.test.yml config --format json)"
command="$(jq -er '.services["backend-e2e"].command | if type == "array" and length == 3 and .[0] == "sh" and .[1] == "-c" then .[2] else error("unsupported backend-e2e command") end' <<< "$config")"
[[ "$command" == *"node --test "* ]] || { echo 'backend-e2e no longer contains the expected test command' >&2; exit 1; }
[[ "$command" != *'$'* ]] || { echo 'backend-e2e shell interpolation requires review' >&2; exit 1; }
command="${command//node --test /node --test --test-concurrency=1 }"
docker compose -f docker/docker-compose.test.yml run --rm backend-e2e sh -c "$command"
`

// PR checkout remains GitHub's tested merge context. A push compares the full
// before..after range, including every commit in the push and deleted files.
const changedPaths = `set -euo pipefail
desktop=false
mobile=false
if [[ "${EVENT_NAME:-pull_request}" == push || ! "$BASE_SHA" =~ ^[0-9a-f]{40}$ || "$BASE_SHA" =~ ^0+$ ]]; then
  desktop=true
  mobile=true
else
  git diff --no-renames --name-only -z "$BASE_SHA" "$GITHUB_SHA" > "$RUNNER_TEMP/passshare-paths"
  while IFS= read -r -d '' path; do
    case "$path" in
      frontend/cloud-assets.json|frontend/public/cloud-crypto/*) desktop=true; mobile=true ;;
      apps/desktop/*) desktop=true ;;
      apps/mobile/*) mobile=true ;;
      frontend/*|packages/extension/*|e2e/web/*|e2e/extension/*) ;;
      *) desktop=true; mobile=true ;;
    esac
  done < "$RUNNER_TEMP/passshare-paths"
fi
printf 'desktop=%s\nmobile=%s\n' "$desktop" "$mobile" >> "$GITHUB_OUTPUT"
printf 'Desktop tests: %s; mobile tests: %s\n' "$desktop" "$mobile" >> "$GITHUB_STEP_SUMMARY"
`

func decode(data []byte) (object, error) {
	var v object
	err := yaml.Unmarshal(data, &v)
	return v, err
}

func encode(v object) ([]byte, error) {
	var buf bytes.Buffer
	e := yaml.NewEncoder(&buf)
	e.SetIndent(2)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), e.Close()
}

func transform(q, m object) error {
	jobs := q["jobs"].(object)
	for _, raw := range jobs["docker-e2e"].(object)["steps"].([]any) {
		s := raw.(object)
		if s["name"] == "Run PostgreSQL integration tests" {
			s["run"] = serialE2E
		}
	}
	desktop := jobs["desktop-smoke"].(object)
	mobile := jobs["mobile-smoke"].(object)
	// These tests were on the same ubuntu-latest, stable Rust and Node 22,
	// same source/working directories/env. Keep every extra check instead of
	// treating the narrower smoke_ filter as the full mobile acceptance.
	mobileSteps := mobile["steps"].([]any)
	for _, raw := range mobileSteps {
		s := raw.(object)
		if s["name"] == "Verify actual React auth against delayed logout and lifecycle results" {
			s["run"] = "cargo run -p passshare-core --locked --example mobile_auth_acceptance -- apps/mobile\n"
		}
	}
	// Check the frontend before spending time on native compilation.
	var native any
	var ordered []any
	for _, s := range mobileSteps {
		if s.(object)["name"] == "Run mobile smoke tests" {
			native = s
		} else {
			ordered = append(ordered, s)
		}
	}
	if native == nil {
		return fmt.Errorf("missing original mobile native tests")
	}
	// Native tests still precede the React acceptance, which compiles core.
	for i, s := range ordered {
		if s.(object)["name"] == "Verify actual React auth against delayed logout and lifecycle results" {
			ordered = append(ordered[:i], append([]any{native}, ordered[i:]...)...)
			break
		}
	}
	mobile["steps"] = ordered
	mobile["timeout-minutes"] = 30
	m["on"] = object{"workflow_call": nil}
	delete(m, "concurrency") // The calling Quality Gate owns cancellation.
	m["jobs"] = object{"smoke": mobile}
	jobs["mobile-smoke"] = object{
		"needs": "preflight", "if": "needs.preflight.outputs.mobile == 'true'",
		"uses": "./.github/workflows/mobile-build.yml",
	}

	steps := desktop["steps"].([]any)
	var rustTest any
	ordered = nil
	for _, s := range steps {
		step := s.(object)
		if step["name"] == "Run desktop smoke tests" {
			// Shared dependency updates affect the full desktop crate. The full
			// default test command includes smoke_, native_lock and doctests once.
			step["name"] = "Run desktop tests and doctests"
			step["run"] = "cargo test --locked -p passshare-desktop\n"
			rustTest = s
		} else {
			if step["name"] == "Run cloud sync unit tests" {
				step["run"] = strings.Replace(step["run"].(string),
					"pnpm --dir apps/desktop run test", "pnpm --dir apps/desktop exec tsc --noEmit\npnpm --dir apps/desktop run test", 1)
			}
			ordered = append(ordered, s)
		}
	}
	if rustTest == nil {
		return fmt.Errorf("missing original desktop tests")
	}
	desktop["steps"] = append(ordered, rustTest)
	desktop["if"] = "needs.preflight.outputs.desktop == 'true'"

	jobs["preflight"] = object{
		"name": "Change scope and inexpensive checks", "runs-on": "ubuntu-latest", "timeout-minutes": 10,
		"outputs": object{"desktop": "${{ steps.scope.outputs.desktop }}", "mobile": "${{ steps.scope.outputs.mobile }}"},
		"steps": []any{
			object{"uses": checkout, "with": object{"fetch-depth": 0, "persist-credentials": false}},
			object{"name": "Determine affected native clients", "id": "scope", "shell": "bash",
				"env": object{"BASE_SHA": "${{ github.event.pull_request.base.sha || github.event.before }}", "EVENT_NAME": "${{ github.event_name }}"}, "run": changedPaths},
			object{"uses": "dtolnay/rust-toolchain@4be7066ada62dd38de10e7b70166bc74ed198c30"},
			object{"name": "Check mobile acceptance formatting", "run": "rustfmt --edition 2021 --check crates/passshare-core/examples/mobile_auth_acceptance.rs"},
			object{"uses": "actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16", "with": object{"go-version-file": "tools/desktop-release-check/go.mod", "cache": false}},
			object{"name": "Test candidate verifier without signing secrets", "working-directory": "tools/desktop-release-check",
				"env": object{"TEST_SCRATCH": "${{ runner.temp }}"}, "run": "go test -mod=readonly ./..."},
		},
	}
	for name, raw := range jobs {
		if name == "preflight" || name == "gate" || name == "mobile-smoke" {
			continue
		}
		j := raw.(object)
		if _, exists := j["needs"]; exists {
			return fmt.Errorf("unexpected existing dependency: %s", name)
		}
		j["needs"] = "preflight"
	}
	gate := jobs["gate"].(object)
	gate["if"] = "${{ !cancelled() }}"
	gate["needs"] = []any{"preflight", "rust-docker", "docker-e2e", "backend", "connect-types", "connect-js", "connect-go", "i18n", "frontend", "extension", "desktop-smoke", "mobile-smoke", "benchmark"}
	gate["steps"] = []any{object{
		"name": "Require successful checks or an explicitly unaffected client",
		"env":  object{"RESULTS": "${{ toJSON(needs) }}", "DESKTOP": "${{ needs.preflight.outputs.desktop }}", "MOBILE": "${{ needs.preflight.outputs.mobile }}"},
		"run":  `echo "$RESULTS" | jq -e --arg desktop "$DESKTOP" --arg mobile "$MOBILE" 'to_entries | all(if .key == "desktop-smoke" then (.value.result == "success" or (.value.result == "skipped" and $desktop == "false")) elif .key == "mobile-smoke" then (.value.result == "success" or (.value.result == "skipped" and $mobile == "false")) else .value.result == "success" end)'`,
	}}
	return nil
}

// Compare all jobs outside the two clients and aggregation structurally; only
// adding preflight dependencies is permitted there. Commands/env remain exact.
func unchangedJobs(before, after object) error {
	a, b := before["jobs"].(object), after["jobs"].(object)
	for name, original := range a {
		if name == "desktop-smoke" || name == "mobile-smoke" || name == "gate" {
			continue
		}
		copy := object{}
		for k, v := range b[name].(object) {
			if k != "needs" {
				copy[k] = v
			}
		}
		if name == "docker-e2e" {
			var normalized []any
			for _, raw := range copy["steps"].([]any) {
				s := raw.(object)
				if s["name"] != "Run PostgreSQL integration tests" {
					normalized = append(normalized, s)
					continue
				}
				if s["run"] != serialE2E {
					return fmt.Errorf("unreviewed E2E command")
				}
				step := object{}
				for k, v := range s {
					step[k] = v
				}
				step["run"] = "docker compose -f docker/docker-compose.test.yml run --rm backend-e2e"
				normalized = append(normalized, step)
			}
			copy["steps"] = normalized
		}
		if !reflect.DeepEqual(original, copy) {
			return fmt.Errorf("unrelated job changed: %s", name)
		}
	}
	for _, key := range []string{"on", "permissions", "concurrency", "name"} {
		if !reflect.DeepEqual(before[key], after[key]) {
			return fmt.Errorf("workflow setting changed: %s", key)
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: passshare-ci <original-directory> <output-directory>")
		os.Exit(2)
	}
	if err := generate(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(input, output string) error {
	read := func(name string) (object, error) {
		b, err := os.ReadFile(filepath.Join(input, name))
		if err != nil {
			return nil, err
		}
		return decode(b)
	}
	q, err := read("quality-gate.yml")
	if err != nil {
		return err
	}
	orig, err := read("quality-gate.yml")
	if err != nil {
		return err
	}
	m, err := read("mobile-build.yml")
	if err != nil {
		return err
	}
	if err := transform(q, m); err != nil {
		return err
	}
	if err := unchangedJobs(orig, q); err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	for name, v := range map[string]object{"quality-gate.yml": q, "mobile-build.yml": m} {
		original, err := os.ReadFile(filepath.Join(input, name))
		if err != nil {
			return err
		}
		if name == "quality-gate.yml" {
			const old = "  # NOTE: cargo-audit / pnpm-audit live in security-audit.yml (single SSOT for\n  # dependency advisories, #325). Advisory scans are non-blocking on PRs (a newly\n  # published CVE must not redden unrelated PRs) and a hard gate on main + the\n  # nightly schedule. They are intentionally NOT a `needs:` of this gate."
			original = []byte(strings.Replace(string(original), old, "  # Dependency audits remain blocking in security-audit.yml for PRs, main\n  # and the nightly schedule. They are separate from this aggregation.", 1))
		}
		b, err := encodePreserving(original, v)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(output, name), b, 0644); err != nil {
			return err
		}
	}
	return nil
}
