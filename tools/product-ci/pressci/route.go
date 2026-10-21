package pressci

import (
	"bytes"
	"cc/product-ci/normalci"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const RouteVariable = "CC_PRODUCT_2766_CI"

func LeaseValue(lease string) string {
	if !normalci.ValidLease(lease) {
		return ""
	}
	return "todo2766-" + lease
}

// Roles identify every original matrix instance, not just github.job. A
// separate role/run/attempt label prevents two shards or retries sharing a
// single ephemeral registration.
func RoleJob(workflow, role string) (Job, bool) {
	var jobs []Job
	switch workflow {
	case "local-checks.yml":
		for _, id := range []string{"rust-core", "rust-field-read-signing", "rust-aux", "browser-package", "browser-cold-start", "local-gates"} {
			timeout := 90
			if id == "browser-cold-start" {
				timeout = 20
			}
			if id == "local-gates" {
				timeout = 360
			}
			jobs = append(jobs, Job{Workflow: ".github/workflows/" + workflow, ID: id, Name: id, OS: "Linux", TimeoutMinutes: timeout})
		}
		for _, value := range []string{"1", "2", "3", "4"} {
			jobs = append(jobs, Job{Workflow: ".github/workflows/" + workflow, ID: "rust-integration", Name: "rust-integration (" + value + ")", OS: "Linux", MatrixKey: "shard", MatrixValue: value, TimeoutMinutes: 90})
		}
	case "rust-v2-https.yml":
		for _, value := range []string{"ubuntu-24.04", "windows-2022"} {
			osName := "Linux"
			if value == "windows-2022" {
				osName = "Windows"
			}
			jobs = append(jobs, Job{Workflow: ".github/workflows/" + workflow, ID: "rust-v2-https", Name: "rust-v2-https (" + value + ")", OS: osName, MatrixKey: "os", MatrixValue: value, TimeoutMinutes: 90})
		}
	case "windows-hidden-fixtures.yml":
		jobs = []Job{{Workflow: ".github/workflows/" + workflow, ID: "junction-fixtures", Name: "junction-fixtures", OS: "Windows", TimeoutMinutes: 90}}
	}
	for _, j := range jobs {
		if Role(j) == role {
			return j, true
		}
	}
	return Job{}, false
}
func Role(j Job) string {
	if j.MatrixValue != "" {
		return j.ID + "-" + j.MatrixValue
	}
	return j.ID
}

func RuntimeLabel(workflow, role string, runID int64, attempt int) string {
	j, ok := RoleJob(workflow, role)
	if !ok || runID < 1 || attempt < 1 {
		return ""
	}
	return fmt.Sprintf("cc-product-2766-1386702183-%s-%s-%d-%d", strings.ToLower(j.OS), role, runID, attempt)
}

func runnerExpression(workflow string, j Job, lease string) (string, error) {
	value := LeaseValue(lease)
	known, ok := RoleJob(workflow, Role(j))
	if value == "" || !ok || j != known {
		return "", errors.New("Press route role rejected")
	}
	event := "github.event_name == 'pull_request' && github.event.pull_request.number == 175 && github.head_ref == '" + Ref + "' && github.event.pull_request.draft == true"
	if workflow == "local-checks.yml" {
		event = "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/" + Ref + "'"
	}
	previous := "'ubuntu-24.04'"
	if j.OS == "Windows" {
		previous = "'windows-2022'"
	}
	role := j.ID
	args := "github.run_id, github.run_attempt"
	if j.MatrixKey != "" {
		role += "-{0}"
		args = "matrix." + j.MatrixKey + ", " + args
		if j.MatrixKey == "os" {
			previous = "matrix.os"
		}
	}
	n := 0
	if j.MatrixKey != "" {
		n = 1
	}
	// matrix.os chooses the platform-specific prefix as part of the same scalar.
	osExpression := "'" + strings.ToLower(j.OS) + "'"
	if j.MatrixKey == "os" {
		osExpression = "matrix.os == 'windows-2022' && 'windows' || 'linux'"
	}
	format := "'cc-product-2766-1386702183-{0}-" + role
	// OS is always argument zero. Matrix values, when present, use argument one.
	if j.MatrixKey != "" {
		format = strings.Replace(format, "-"+j.ID+"-{0}", "-"+j.ID+"-{1}", 1)
	}
	format += fmt.Sprintf("-{%d}-{%d}'", n+1, n+2)
	return "${{ vars." + RouteVariable + " == '" + value + "' && " + event + " && format(" + format + ", " + osExpression + ", " + args + ") || " + previous + " }}", nil
}

// PrepareRoutes preserves every byte except original runs-on scalar tokens.
// It accepts only one of the three immutable original b2 workflow blobs.
func PrepareRoutes(name string, data []byte, lease string) ([]byte, error) {
	want := ""
	for _, w := range workflows() {
		if w.name == name {
			want = w.blob
		}
	}
	if want == "" || blobID(data) != want {
		return nil, errors.New("Press original route blob rejected")
	}
	return prepareRoutes(name, data, lease)
}

func prepareRoutes(name string, data []byte, lease string) ([]byte, error) {
	outline, e := outlineWorkflow(data, name)
	if e != nil {
		return nil, e
	}
	root, e := normalci.ParseYAML(data)
	if e != nil {
		return nil, e
	}
	type edit struct {
		start, end int
		value      string
	}
	var edits []edit
	seen := map[string]bool{}
	for _, j := range outline.Jobs {
		if seen[j.ID] {
			continue
		}
		seen[j.ID] = true
		n := normalci.Field(normalci.Field(normalci.Field(root, "jobs"), j.ID), "runs-on")
		if n == nil || n.Style != 0 {
			return nil, errors.New("Press plain route token required")
		}
		value, e := runnerExpression(name, j, lease)
		if e != nil {
			return nil, e
		}
		offset := -1
		line, column := 1, 1
		for i, ch := range string(data) {
			if line == n.Line && column == n.Column {
				offset = i
				break
			}
			if ch == '\n' {
				line, column = line+1, 1
			} else {
				column++
			}
		}
		if offset < 0 || offset+len(n.Value) > len(data) || string(data[offset:offset+len(n.Value)]) != n.Value {
			return nil, errors.New("Press route byte position rejected")
		}
		edits = append(edits, edit{offset, offset + len(n.Value), value})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	previous := 0
	for _, edit := range edits {
		if edit.start < previous {
			return nil, errors.New("Press overlapping routes")
		}
		out.Write(data[previous:edit.start])
		out.WriteString(edit.value)
		previous = edit.end
	}
	out.Write(data[previous:])
	parsed, e := normalci.ParseYAML(out.Bytes())
	if e != nil {
		return nil, e
	}
	for _, j := range outline.Jobs {
		expected, e := runnerExpression(name, j, lease)
		n := normalci.Field(normalci.Field(normalci.Field(parsed, "jobs"), j.ID), "runs-on")
		if e != nil || n == nil || n.Value != expected {
			return nil, errors.New("Press prepared route reparse rejected")
		}
	}
	return out.Bytes(), nil
}
