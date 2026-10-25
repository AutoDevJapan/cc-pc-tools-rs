package main

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func omb3255TransportFixture(t *testing.T, resume bool) []map[string]any {
	t.Helper()
	method := "thread/start"
	params := map[string]any{"model": "requested-model-only", "effort": "max", "unknown": "synthetic-private-value"}
	if resume {
		method = "thread/resume"
		params["threadId"] = "synthetic-native-thread"
	}
	messages := []map[string]any{
		{"id": 1, "method": method, "params": params},
		{"id": 1, "result": map[string]any{"thread": map[string]any{"id": "synthetic-native-thread"}, "model": "pre-turn-model-only"}},
		{"id": 2, "method": "turn/start", "params": map[string]any{"threadId": "synthetic-native-thread", "model": "requested-model-only", "effort": "max"}},
		{"id": 2, "result": map[string]any{"turn": map[string]any{"id": "synthetic-native-turn"}}},
		{"method": "turn/completed", "params": map[string]any{"threadId": "synthetic-native-thread", "turn": map[string]any{"id": "synthetic-native-turn", "status": "completed"}}},
	}
	rows := make([]map[string]any, len(messages))
	for i, msg := range messages {
		dir := "in"
		if i == 0 || i == 2 {
			dir = "out"
		}
		rows[i] = map[string]any{"at": time.UnixMilli(1792848637000 + int64(i)*1000).UTC().Format(time.RFC3339Nano), "dir": dir, "source": "codex.app-server", "msg": msg}
	}
	return rows
}

func TestOMB3255TransportIdentityRejectsAmbiguity(t *testing.T) {
	message := func(rows []map[string]any, i int) map[string]any { return rows[i]["msg"].(map[string]any) }
	tests := map[string]func([]map[string]any) []map[string]any{
		"only request":           func(r []map[string]any) []map[string]any { return r[:1] },
		"no completion":          func(r []map[string]any) []map[string]any { return r[:4] },
		"thread response id":     func(r []map[string]any) []map[string]any { message(r, 1)["id"] = 8; return r },
		"turn response id":       func(r []map[string]any) []map[string]any { message(r, 3)["id"] = 8; return r },
		"string RPC id":          func(r []map[string]any) []map[string]any { message(r, 0)["id"] = "1"; return r },
		"reused RPC id":          func(r []map[string]any) []map[string]any { message(r, 2)["id"] = 1; return r },
		"thread response absent": func(r []map[string]any) []map[string]any { message(r, 1)["result"] = map[string]any{}; return r },
		"thread response null":   func(r []map[string]any) []map[string]any { message(r, 1)["result"] = nil; return r },
		"response RPC failure": func(r []map[string]any) []map[string]any {
			message(r, 3)["error"] = map[string]any{"message": "synthetic private error"}
			return r
		},
		"wrong turn request thread": func(r []map[string]any) []map[string]any {
			message(r, 2)["params"].(map[string]any)["threadId"] = "other-thread"
			return r
		},
		"missing turn response": func(r []map[string]any) []map[string]any { message(r, 3)["result"] = map[string]any{}; return r },
		"invalid returned id": func(r []map[string]any) []map[string]any {
			message(r, 3)["result"].(map[string]any)["turn"].(map[string]any)["id"] = "private path/value"
			return r
		},
		"same native ids": func(r []map[string]any) []map[string]any {
			message(r, 3)["result"].(map[string]any)["turn"].(map[string]any)["id"] = "synthetic-native-thread"
			return r
		},
		"wrong completed thread": func(r []map[string]any) []map[string]any {
			message(r, 4)["params"].(map[string]any)["threadId"] = "other-thread"
			return r
		},
		"wrong completed turn": func(r []map[string]any) []map[string]any {
			message(r, 4)["params"].(map[string]any)["turn"].(map[string]any)["id"] = "other-turn"
			return r
		},
		"failed completion": func(r []map[string]any) []map[string]any {
			message(r, 4)["params"].(map[string]any)["turn"].(map[string]any)["status"] = "failed"
			return r
		},
		"error in completion": func(r []map[string]any) []map[string]any {
			message(r, 4)["params"].(map[string]any)["turn"].(map[string]any)["error"] = "private error"
			return r
		},
		"wrong source":         func(r []map[string]any) []map[string]any { r[3]["source"] = "claude.sdk.message"; return r },
		"wrong direction":      func(r []map[string]any) []map[string]any { r[1]["dir"] = "out"; return r },
		"invalid direction":    func(r []map[string]any) []map[string]any { r[1]["dir"] = "unknown"; return r },
		"wrong timestamp type": func(r []map[string]any) []map[string]any { r[1]["at"] = 12; return r },
		"before window":        func(r []map[string]any) []map[string]any { r[0]["at"] = "2026-10-24T13:30:36Z"; return r },
		"after window":         func(r []map[string]any) []map[string]any { r[4]["at"] = "2026-10-24T13:35:30Z"; return r },
		"reverse timestamp":    func(r []map[string]any) []map[string]any { r[3]["at"] = r[0]["at"]; return r },
		"duplicate response": func(r []map[string]any) []map[string]any {
			return append(r[:2:2], append([]map[string]any{r[1]}, r[2:]...)...)
		},
		"duplicate completion": func(r []map[string]any) []map[string]any { return append(r, r[4]) },
		"second execution": func(r []map[string]any) []map[string]any {
			return append(r, map[string]any{"at": "2026-10-24T13:31:00Z", "dir": "out", "source": "codex.app-server", "msg": message(r, 0)})
		},
		"case alias not response id": func(r []map[string]any) []map[string]any {
			delete(message(r, 1), "id")
			message(r, 1)["ID"] = 1
			return r
		},
		"null message":   func(r []map[string]any) []map[string]any { r[1]["msg"] = nil; return r },
		"invalid method": func(r []map[string]any) []map[string]any { message(r, 1)["method"] = 13; return r },
		"resume result differs": func(r []map[string]any) []map[string]any {
			message(r, 0)["params"].(map[string]any)["threadId"] = "different-requested-thread"
			return r
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			rows := mutate(omb3255TransportFixture(t, true))
			candidate, ok := omb3255TransportIdentity(strings.NewReader(omb3255TransportJSON(t, rows)), time.UnixMilli(1792848636233), time.UnixMilli(1792848928582))
			if ok || candidate != (omb3255NativeCandidate{}) {
				t.Fatalf("unavailable must discard candidate: %+v %v", candidate, ok)
			}
		})
	}
}

func TestOMB3255TransportIdentityConsumesBoundedWholeInput(t *testing.T) {
	good := omb3255TransportJSON(t, omb3255TransportFixture(t, false))
	bad := map[string]string{
		"empty": "", "truncated": strings.TrimSuffix(good, "\n"),
		"trailing malformed":           good + "{broken}\n",
		"duplicate key":                strings.Replace(good, `"id":1`, `"id":1,"id":8`, 1),
		"duplicate nested unknown key": good + `{"source":"other","unknown":{"x":1,"x":2}}` + "\n",
		"line bound":                   good + strings.Repeat(" ", (64<<10)+1) + "\n",
		"total bound":                  good + strings.Repeat("\n", 1<<20),
		"depth bound":                  good + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "\n",
		"invalid utf8":                 good + string([]byte{0xff}) + "\n",
		"BOM":                          "\ufeff" + good,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			candidate, ok := omb3255TransportIdentity(strings.NewReader(raw), time.UnixMilli(1792848636233), time.UnixMilli(1792848928582))
			if ok || candidate != (omb3255NativeCandidate{}) {
				t.Fatalf("partial candidate escaped: %+v %v", candidate, ok)
			}
		})
	}
	input := io.MultiReader(strings.NewReader(good), omb3255TransportErrorReader{})
	if c, ok := omb3255TransportIdentity(input, time.UnixMilli(1792848636233), time.UnixMilli(1792848928582)); ok || c != (omb3255NativeCandidate{}) {
		t.Fatal("read error retained a partial candidate")
	}
	for _, scope := range [][2]time.Time{{{}, time.Now()}, {time.Now(), {}}, {time.Unix(2, 0), time.Unix(1, 0)}} {
		if _, ok := omb3255TransportIdentity(strings.NewReader(good), scope[0], scope[1]); ok {
			t.Fatal("invalid scope accepted")
		}
	}
	if _, ok := omb3255TransportIdentity(nil, time.Unix(1, 0), time.Unix(2, 0)); ok {
		t.Fatal("nil input accepted")
	}
	for name, input := range map[string]string{
		"exact total": good + strings.Repeat("\n", (1<<20)-len(good)),
		"exact line":  good + strings.Repeat(" ", 64<<10) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if c, ok := omb3255TransportIdentity(strings.NewReader(input), time.UnixMilli(1792848636233), time.UnixMilli(1792848928582)); !ok || c.threadID == "" || c.turnID == "" {
				t.Fatal("exact boundary positive control failed")
			}
		})
	}
}

type omb3255TransportErrorReader struct{}

func (omb3255TransportErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic read error")
}

func TestOMB3255TransportIdentityWindowAndNonContextBoundary(t *testing.T) {
	rows := omb3255TransportFixture(t, false)
	start, _ := time.Parse(time.RFC3339Nano, rows[0]["at"].(string))
	end, _ := time.Parse(time.RFC3339Nano, rows[4]["at"].(string))
	// Outside-window and other-provider records do not become the selected pair.
	input := `{"at":"2026-10-24T12:00:00Z","dir":"out","source":"codex.app-server","msg":{"method":"thread/start"}}` + "\n" +
		`{"source":"other-provider","msg":{"model":"private-unknown","effort":"max"}}` + "\n" + omb3255TransportJSON(t, rows)
	candidate, ok := omb3255TransportIdentity(strings.NewReader(input), start, end)
	if !ok || candidate.threadID == "" || candidate.turnID == "" {
		t.Fatal("inclusive scope positive control failed")
	}
	encoded, err := json.Marshal(candidate)
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("candidate has no public JSON fields: %s %v", encoded, err)
	}
	observed := omb3255ProjectContext(omb3255ContextScope{candidate.threadID, candidate.turnID, start, end}, strings.NewReader(input))
	if observed != omb3255ContextUnavailable() {
		t.Fatal("OMB transport must not become Codex context or model/effort evidence")
	}
}

func TestOMB3255TransportRPCExactNumericIDs(t *testing.T) {
	for _, raw := range []string{"0", "1", "9007199254740991"} {
		if _, ok := omb3255TransportRPC(json.RawMessage(raw)); !ok {
			t.Fatalf("exact producer id rejected: %s", raw)
		}
	}
	for _, raw := range []string{"", `"1"`, "null", "-1", "1.0", "1e0", "9007199254740992", "99999999999999999999999999999999"} {
		if _, ok := omb3255TransportRPC(json.RawMessage(raw)); ok {
			t.Fatalf("non-exact id accepted: %s", raw)
		}
	}
}

func omb3255TransportJSON(t *testing.T, rows []map[string]any) string {
	t.Helper()
	var out strings.Builder
	for _, row := range rows {
		b, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(b)
		out.WriteByte('\n')
	}
	return out.String()
}

func TestOMB3255TransportIdentityReturnedRPCPair(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "resume"}[resume], func(t *testing.T) {
			got, ok := omb3255TransportIdentity(strings.NewReader(omb3255TransportJSON(t, omb3255TransportFixture(t, resume))), time.UnixMilli(1792848636233), time.UnixMilli(1792848928582))
			if !ok || got.threadID != "synthetic-native-thread" || got.turnID != "synthetic-native-turn" {
				t.Fatalf("returned RPC pair unavailable: candidate=%+v available=%v", got, ok)
			}
		})
	}
}
