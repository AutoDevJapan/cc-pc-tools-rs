package normalci

import (
	"errors"
	"fmt"
	"strings"
)

func ValidLease(s string) bool { return len(s) == 32 && strings.Trim(s, "0123456789abcdef") == "" }
func LeaseValue(s string) string {
	if !ValidLease(s) {
		return ""
	}
	return "todo2737-" + s
}

// Only the named job of this official run can match the ephemeral runner.
func RuntimeLabel(role string, runID int64) string {
	if runID < 1 {
		return ""
	}
	for _, r := range Routes() {
		if r.Job == role {
			return fmt.Sprintf("%s-%s-%d", r.Proposed, role, runID)
		}
	}
	return ""
}
func RunnerExpressionForLease(r Route, lease string) (string, error) {
	value := LeaseValue(lease)
	if value == "" {
		return "", errors.New("route lease rejected")
	}
	known := false
	for _, item := range Routes() {
		if item == r {
			known = true
		}
	}
	if !known {
		return "", errors.New("route role rejected")
	}
	return "${{ vars." + RouteVariable + " == '" + value + "' && github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/codex/todo2641-http-response-pseudo-20261015' && format('" + r.Proposed + "-" + r.Job + "-{0}', github.run_id) || '" + r.Previous + "' }}", nil
}
