package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	workflowPath = "../../.github/workflows/unattended-readiness.yml"
	// The repair guard maps the job names of this workflow to its path.
	identityPath = "../../repair-guard/skills3080_update_ci_windows.go"
	partitionIf  = "runner.os == 'Windows' && matrix.module == 'unattended-runner'"
	ordinaryIf   = "runner.os != 'Windows' || matrix.module != 'unattended-runner'"
	helperDir    = "tools/unattended-race-partition"
)

type workflowStep struct {
	Name             string `yaml:"name"`
	If               string `yaml:"if"`
	WorkingDirectory string `yaml:"working-directory"`
	Run              string `yaml:"run"`
}

type workflowJob struct {
	TimeoutMinutes string `yaml:"timeout-minutes"`
	Strategy       struct {
		Matrix struct {
			OS     []string `yaml:"os"`
			Module []string `yaml:"module"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []workflowStep `yaml:"steps"`
}

type workflowFile struct {
	On struct {
		Push struct {
			Paths []string `yaml:"paths"`
		} `yaml:"push"`
		PullRequest struct {
			Paths []string `yaml:"paths"`
		} `yaml:"pull_request"`
	} `yaml:"on"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

func readWorkflow(t *testing.T) workflowFile {
	t.Helper()
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	var w workflowFile
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatalf("parse %s: %v", workflowPath, err)
	}
	return w
}

// stepIndex returns the only step that satisfies match.
func stepIndex(t *testing.T, steps []workflowStep, what string, match func(workflowStep) bool) int {
	t.Helper()
	found := -1
	for i, step := range steps {
		if !match(step) {
			continue
		}
		if found >= 0 {
			t.Fatalf("more than one step %s: %d and %d", what, found, i)
		}
		found = i
	}
	if found < 0 {
		t.Fatalf("no step %s", what)
	}
	return found
}

func TestWorkflowRunsThePartitionsOnlyForWindowsUnattendedRunner(t *testing.T) {
	job := readWorkflow(t).Jobs["go"]
	ordinary := stepIndex(t, job.Steps, "runs go test -race on ./...", func(s workflowStep) bool {
		return strings.HasPrefix(s.Run, "go test -race -timeout ") && strings.HasSuffix(strings.TrimSpace(s.Run), " ./...")
	})
	fixtures := stepIndex(t, job.Steps, "builds the native fixtures", func(s workflowStep) bool {
		return s.Name == "Build the native question acceptance fixtures"
	})
	selfTest := stepIndex(t, job.Steps, "tests the helper", func(s workflowStep) bool {
		return s.WorkingDirectory == helperDir && strings.HasPrefix(s.Run, "go test -race -count=1 ")
	})
	partitions := stepIndex(t, job.Steps, "runs the partitions", func(s workflowStep) bool {
		return s.WorkingDirectory == helperDir && strings.HasPrefix(s.Run, "go run . ")
	})
	if got := job.Steps[ordinary].If; got != ordinaryIf {
		t.Errorf("ordinary go test runs if %q, want %q", got, ordinaryIf)
	}
	if strings.Contains(job.Steps[ordinary].Run, "600s") {
		t.Errorf("ordinary go test still carries the Windows package deadline: %q", job.Steps[ordinary].Run)
	}
	for _, i := range []int{fixtures, selfTest, partitions} {
		if got := job.Steps[i].If; got != partitionIf {
			t.Errorf("step %q runs if %q, want %q", job.Steps[i].Name, got, partitionIf)
		}
	}
	if !(fixtures < selfTest && selfTest < partitions) {
		t.Errorf("steps run in order fixtures %d, helper tests %d, partitions %d", fixtures, selfTest, partitions)
	}

	fields := strings.Fields(job.Steps[partitions].Run)
	cfg, err := parseArgs(fields[3:], io.Discard)
	if err != nil {
		t.Fatalf("the workflow passes invalid arguments %q: %v", fields[3:], err)
	}
	if !cfg.race {
		t.Error("the workflow disables the race detector")
	}
	mod, err := os.ReadFile(filepath.Join(cfg.dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(string(mod), "\n"); strings.TrimSpace(first) != "module github.com/AutoDevJapan/cc-pc-tools-rs/unattended-runner" {
		t.Errorf("-dir %s is not the unattended-runner module: %q", cfg.dir, first)
	}

	limit := regexp.MustCompile(`^\$\{\{ matrix\.os == 'Windows' && matrix\.module == 'unattended-runner' && (\d+) \|\| 10 \}\}$`).FindStringSubmatch(job.TimeoutMinutes)
	if limit == nil {
		t.Fatalf("unexpected job timeout %q", job.TimeoutMinutes)
	}
	minutes, _ := strconv.Atoi(limit[1])
	// Setup with the native fixtures, the helper tests and vet/build take
	// about 8 minutes besides the suite budget.
	if jobLimit := time.Duration(minutes) * time.Minute; jobLimit < cfg.suiteTimeout+8*time.Minute {
		t.Errorf("job limit %v leaves less than 8m beside the %v suite budget", jobLimit, cfg.suiteTimeout)
	}
}

func TestWorkflowStartsWhenTheHelperChanges(t *testing.T) {
	w := readWorkflow(t)
	for event, paths := range map[string][]string{"push": w.On.Push.Paths, "pull_request": w.On.PullRequest.Paths} {
		if !slices.Contains(paths, helperDir+"/**") {
			t.Errorf("%s paths %q omit %s/**", event, paths, helperDir)
		}
	}
}

func TestWorkflowJobNamesMatchTheRepairGuardIdentity(t *testing.T) {
	w := readWorkflow(t)
	job := w.Jobs["go"]
	var names []string
	for _, os := range job.Strategy.Matrix.OS {
		for _, module := range job.Strategy.Matrix.Module {
			names = append(names, "go ("+os+", "+module+")")
		}
	}
	if _, ok := w.Jobs["repair-guard"]; ok {
		names = append(names, "repair-guard")
	}
	mapped := workflowNamesInSource(t, identityPath, ".github/workflows/unattended-readiness.yml")
	slices.Sort(names)
	slices.Sort(mapped)
	if !slices.Equal(names, mapped) {
		t.Fatalf("workflow job names %q differ from the names that %s maps to this workflow %q; update both together", names, identityPath, mapped)
	}
}

// workflowNamesInSource returns the string cases of the switch clause whose
// body returns the workflow path.
func workflowNamesInSource(t *testing.T, path, workflow string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	quoted := strconv.Quote(workflow)
	var names []string
	ast.Inspect(file, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		returnsWorkflow := false
		for _, stmt := range clause.Body {
			ast.Inspect(stmt, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Value == quoted {
					returnsWorkflow = true
				}
				return true
			})
		}
		if !returnsWorkflow {
			return true
		}
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("%s: unexpected case value %T", path, expr)
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		return false
	})
	if len(names) == 0 {
		t.Fatalf("%s maps no job name to %s", path, workflow)
	}
	return names
}
