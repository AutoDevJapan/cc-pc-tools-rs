package inputqual

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Explicit intent remains authoritative even if status stays todo/doing/wait.
// Malformed stop markers fail closed; unrelated automatic dependency waits are
// preserved and do not manufacture an owner stop or new execution permission.
func explicitStop(raw []byte) bool {
	var row map[string]json.RawMessage
	if decode(raw, &row, false) != nil {
		return true
	}
	value := func(values map[string]json.RawMessage, key string) json.RawMessage {
		for k, v := range values {
			if strings.EqualFold(k, key) {
				return v
			}
		}
		return nil
	}
	inactive := func(v json.RawMessage, emptyString bool) bool {
		v = bytes.TrimSpace(v)
		return len(v) == 0 || bytes.Equal(v, []byte("null")) || bytes.Equal(v, []byte("false")) || (emptyString && bytes.Equal(v, []byte(`""`)))
	}
	for _, key := range []string{"stopped", "cancel_requested", "owner_stopped", "paused", "owner_postponement_recorded"} {
		if !inactive(value(row, key), false) {
			return true
		}
	}
	stopKind := bytes.TrimSpace(value(row, "stop_kind"))
	if len(stopKind) > 0 && !bytes.Equal(stopKind, []byte("null")) && !bytes.Equal(stopKind, []byte(`""`)) {
		return true
	}
	schedulerRaw := bytes.TrimSpace(value(row, "scheduler"))
	if len(schedulerRaw) > 0 && !bytes.Equal(schedulerRaw, []byte("null")) {
		var scheduler map[string]json.RawMessage
		if decode(value(row, "scheduler"), &scheduler, false) != nil {
			return true
		}
		if !inactive(value(scheduler, "owner_stop_job"), true) {
			return true
		}
	}
	return false
}
