package inputqual

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestOriginalStopIntentPrecedesPurposeQualification(t *testing.T) {
	for _, task := range []int{3059, 3056, 3089} {
		for _, field := range []string{"stopped", "cancel_requested", "owner_stopped", "paused", "stop_kind", "owner_postponement_recorded", "scheduler.owner_stop_job", "Stopped", "OWNER_STOPPED", "scheduler.OWNER_STOP_JOB"} {
			t.Run(strings.Join([]string{strconv.Itoa(task), field}, "/"), func(t *testing.T) {
				f := newFixture(t)
				if _, err := qualifyPurpose(f.p.pin, f.p, true); err != nil {
					t.Fatal("eligible control failed")
				}
				var db map[string]any
				if json.Unmarshal(mustReadFixture(t, f.p.db), &db) != nil {
					t.Fatal("fixture tasks")
				}
				for _, v := range db["items"].([]any) {
					r := v.(map[string]any)
					if int(r["id"].(float64)) != task {
						continue
					}
					switch field {
					case "stop_kind":
						r[field] = "person"
					case "scheduler.owner_stop_job", "scheduler.OWNER_STOP_JOB":
						r["scheduler"] = map[string]any{strings.TrimPrefix(field, "scheduler."): "original-owner-stop"}
					default:
						r[field] = true
					}
				}
				f.put(f.p.db, db)
				for _, construct := range []bool{false, true} {
					_, err := qualifyPurpose(f.p.pin, f.p, construct)
					if err == nil || !strings.HasSuffix(err.Error(), "original_task_scope") {
						t.Fatal("explicit stop with unchanged status passed original task scope")
					}
				}
			})
		}
	}
}
