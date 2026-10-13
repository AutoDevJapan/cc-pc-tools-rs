package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type priorityPolicy struct {
	Items    map[string]json.RawMessage `json:"items"`
	Projects map[string]json.RawMessage `json:"projects"`
	legacy   map[string]json.RawMessage
}

type label struct {
	name, kind, department string
	rank                   int
}

// Exact, owner-supplied/public repository labels only. No title/path matching,
// prefixes, user-provided display text, or protected registry reads.
func publicLabel(project string) label {
	p := strings.ToLower(project)
	p = strings.NewReplacer("-", "", "_", "", " ", "", ":", "", "α", "alpha").Replace(p)
	rank := projectRank(project)
	l := label{kind: "public", department: rankDepartment(rank), rank: rank}
	if rank == 10 {
		return label{"受託案件", "generic", "受託部", 10}
	}
	switch p {
	case "civ6", "civ6banseiki", "banseiki", "万世紀", "万世記":
		l.name = "万世紀"
	case "tools", "pctools", "pcツール", "pcの道具", "cctools", "ccpctoolsrs", "makhmeto":
		l.name = "PCの道具"
	case "localaiwebrs", "localaiconsole", "localaiconcole":
		l.name = "localai-web-rs"
	case "opencodex":
		l.name = "OpenCodex"
	case "youtube", "発信部":
		l.name = "発信部"
	case "skills", "skill":
		l.name = "Skills"
	case "goddmcpalpha":
		l.name = "GoDD-MCP-Alpha"
	case "goddpress":
		l.name = "GoDD-Press"
	case "goddao":
		l.name = "GoDD-AO"
	case "godd13a13e1", "13a13e1":
		l.name = "13a13e1"
	case "goddrosetta", "goddrosseta":
		l.name = "GoDD-Rosetta"
	case "goddgenesis", "genesis":
		l.name = "GoDD-Genesis"
	case "goddvirtualstreamer", "virtualstreamer":
		l.name = "GoDD-VirtualStreamer"
	case "rex":
		l.name = "Re:X"
	default:
		l.name, l.kind = "名称未確認", "generic"
	}
	return l
}

func departmentRank(s string) int {
	switch s {
	case "受託部":
		return 10
	case "万世紀", "万世記", "ゲーム部":
		return 20
	case "情報システム部":
		return 30
	case "発信部":
		return 40
	case "Skills":
		return 50
	case "GoDD事業部":
		return 67
	case "自社サービス部", "自社サービス":
		return 100
	}
	return 0
}

func rankDepartment(rank int) string {
	switch {
	case rank == 10:
		return "受託部"
	case rank == 20:
		return "万世紀"
	case rank == 30 || rank == 50:
		return "情報システム部"
	case rank == 40:
		return "発信部"
	case rank >= 60 && rank <= 67:
		return "GoDD事業部"
	default:
		return "自社サービス部"
	}
}

func policyRank(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil && (n == 10 || n == 20 || n == 30 || n == 40 || n == 50 || n >= 60 && n <= 67 || n == 100) {
		return n
	}
	return 0
}

func itemLabel(item todoItem, policy priorityPolicy) label {
	return policyLabel(item, policy, fmt.Sprint(item.ID))
}

func policyLabel(item todoItem, policy priorityPolicy, policyID string) label {
	project, department := item.Project, item.Department
	raw := policy.Items[policyID]
	var owner struct {
		Project, Department *string
	}
	_ = json.Unmarshal(raw, &owner)
	if owner.Project != nil {
		project = *owner.Project
	} else if name, ok := optionalString(item.OwnerProject); ok {
		project = name
	}
	if owner.Department != nil {
		department = *owner.Department
	}
	l := publicLabel(project)
	rank := policyRank(raw)
	if rank == 0 {
		rank = departmentRank(department)
		if rank == 67 && l.rank >= 60 && l.rank <= 67 {
			rank = l.rank
		}
	}
	if rank == 0 {
		projects := policy.Projects
		if projects == nil {
			projects = policy.legacy
		}
		// Deterministic even for a malformed policy containing case aliases.
		keys := make([]string, 0, len(projects))
		for key := range projects {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if asciiEqualFold(key, project) {
				rank = policyRank(projects[key])
				break
			}
		}
	}
	if rank != 0 {
		l.rank, l.department = rank, rankDepartment(rank)
	}
	if l.rank == 10 {
		l.name, l.kind = "受託案件", "generic"
	}
	return l
}

func optionalString(raw json.RawMessage) (string, bool) {
	var s *string
	err := json.Unmarshal(raw, &s)
	if err != nil || s == nil {
		return "", false
	}
	return *s, true
}

func asciiEqualFold(a, b string) bool {
	lower := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' {
				return r + ('a' - 'A')
			}
			return r
		}, s)
	}
	return lower(a) == lower(b)
}

func measuredProgress(item todoItem) (*int, string) {
	if item.Status == "done" {
		n := 100
		return &n, "done"
	}
	if len(item.Progress) == 0 || string(item.Progress) == "null" {
		return nil, "not_measured"
	}
	var n int
	if json.Unmarshal(item.Progress, &n) != nil || n < 0 || n > 100 {
		return nil, "invalid"
	}
	return &n, "measured"
}

func safeTaskType(raw json.RawMessage) string {
	switch rawString(raw) {
	case "coding", "code", "implementation":
		return "implementation"
	case "review", "code-review":
		return "review"
	case "research", "investigation":
		return "research"
	case "debugging", "fix":
		return "debugging"
	case "testing", "test":
		return "testing"
	case "refactor", "refactoring":
		return "refactoring"
	case "maintenance":
		return "maintenance"
	case "deployment", "deploy":
		return "deployment"
	case "writing", "documentation":
		return "documentation"
	case "design", "planning", "general", "analysis", "build", "verification":
		return rawString(raw)
	case "verify":
		return "verification"
	default:
		return "unknown"
	}
}

func positiveIDs(ids []int64) []int64 {
	set := map[int64]bool{}
	for _, id := range ids {
		if id > 0 {
			set[id] = true
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func pending(ids []int64, items map[int64]todoItem) []int64 {
	var out []int64
	for _, id := range ids {
		if item, ok := items[id]; !ok || item.Status != "done" {
			out = append(out, id)
		}
	}
	return positiveIDs(out)
}

func wait(kind string, ids []int64) WaitReason {
	w := WaitReason{Kind: kind, TodoIDs: positiveIDs(ids)}
	if kind == "recovery_unverified" {
		w.Label = "復旧状況が未確認"
	}
	return w
}

func readyReason(r *readyWait, now time.Time, stale time.Duration) WaitReason {
	w := wait("readiness", nil)
	if r.Condition == "chatgpt-plus" {
		w.Kind = "subscription_readiness"
	} else if r.Condition == "retry-time" {
		w.Kind = "technical_retry"
		c := epochClock(r.NotBefore, now, stale)
		w.NotBefore = &c // Future is expected for a deadline, not for an activity.
	}
	return w
}

func taskWaits(item todoItem, all map[int64]todoItem, now time.Time, stale time.Duration) []WaitReason {
	out := []WaitReason{}
	if item.Status == "done" {
		return out
	}
	if item.Status == "recovery_unverified" {
		out = append(out, wait("recovery_unverified", nil))
	}
	ids := pending(append(append([]int64{}, item.WaitOn...), item.WorkWaitOn...), all)
	if len(ids) > 0 {
		out = append(out, wait("dependency", ids))
	}
	if item.WaitReady != nil {
		out = append(out, readyReason(item.WaitReady, now, stale))
	}
	if item.Status == "block" {
		kind := "blocked_unspecified"
		// The canonical owner project is explicit; arbitrary note text is not.
		if item.Project == "owner" {
			kind = "owner_reply"
		}
		out = append(out, wait(kind, nil))
	} else if item.Status == "wait" && len(out) == 0 {
		out = append(out, wait("unknown", nil))
	}
	return out
}

func jobWaits(j jobMeta, now time.Time, stale time.Duration) []WaitReason {
	out := []WaitReason{}
	if j.Status != "queued" && j.Status != "starting" {
		return out
	}
	if j.QueueWaitReason != nil {
		r := j.QueueWaitReason
		kind := "unknown"
		switch r.Code {
		case "todo_dependency", "work_owner":
			kind = "dependency"
		case "todo_readiness":
			kind = "readiness"
		case "retry_cooldown":
			kind = "rate_limit"
		case "running_limit", "child_limit", "child_priority", "lowest_priority":
			kind = "execution_slot"
		case "intake_owner":
			kind = "other_worker"
		case "supervisor_missing":
			kind = "supervisor_restart"
		case "cancel_requested":
			kind = "cancellation"
		case "todo_store_unavailable":
			kind = "source_unavailable"
		case "model_capacity":
			kind = "model_capacity"
		}
		ids := r.WaitOn
		if kind == "dependency" {
			ids = append(append(append([]int64{}, ids...), j.WorkWaitOn...), r.OwnerTodoID)
		}
		out = append(out, wait(kind, ids))
		for _, r := range r.WaitReady {
			if r.Wait != nil {
				out = append(out, readyReason(r.Wait, now, stale))
			}
		}
	}
	if j.LastFailureKind == "capacity" || strings.HasPrefix(j.Activity, "順番待ち: モデルの混雑により") {
		out = append(out, wait("model_capacity", nil))
	}
	c := epochClock(j.RetryNotBefore, now, stale)
	if c.State == "future" {
		w := wait("rate_limit", nil)
		w.NotBefore = &c
		out = append(out, w)
	}
	if j.CancelRequested {
		out = append(out, wait("cancellation", nil))
	}
	if len(out) == 0 {
		out = append(out, wait("unknown", nil))
	}
	return out
}

type processObservation struct {
	alive, known bool
	created      *uint64
	session      uint32
}

type processProbe func(uint32) processObservation

func presence(pid uint32, expected *uint64, probe processProbe) string {
	if pid == 0 {
		return "not_recorded"
	}
	p := probe(pid)
	if !p.known {
		return "unknown"
	}
	if !p.alive {
		return "absent"
	}
	if expected == nil || p.created == nil {
		return "alive_identity_unknown"
	}
	if *expected != *p.created {
		return "identity_mismatch"
	}
	return "verified_alive"
}

func liveStatus(s string) bool { return s == "queued" || s == "starting" || s == "running" }
func terminalStatus(s string) bool {
	return s == "succeeded" || s == "failed" || s == "cancelled" || s == "lost"
}

func execution(j jobMeta, worker, supervisor string) string {
	if worker == "verified_alive" {
		return "running"
	}
	// A reused or inaccessible PID cannot resurrect a finalized record. Keep
	// its presence evidence, but require an exact live worker identity to
	// promote a terminal job back to running.
	if terminalStatus(j.Status) {
		return "finished"
	}
	if worker == "alive_identity_unknown" || supervisor == "alive_identity_unknown" {
		return "alive_identity_unknown"
	}
	if worker == "unknown" || supervisor == "unknown" {
		return "unknown"
	}
	if supervisor == "verified_alive" && j.Status == "running" {
		return "supervising"
	}
	if j.Status == "queued" {
		return "queued"
	}
	if j.Status == "starting" {
		return "starting"
	}
	if j.Status == "running" {
		return "not_observed"
	}
	if terminalStatus(j.Status) {
		return "finished"
	}
	return "unknown"
}

func phase(j jobMeta) string {
	if j.Status == "queued" {
		return "waiting"
	}
	if j.Status == "starting" {
		return "starting"
	}
	if terminalStatus(j.Status) {
		return "finished"
	}
	if j.Status != "running" {
		return "unknown"
	}
	if j.ContextPhase == "summarize" {
		return "summarizing"
	}
	if j.Activity == "thinking" {
		return "thinking"
	}
	for prefix, name := range map[string]string{
		"running: ": "tool", "mcp: ": "tool", "editing: ": "editing",
		"searching: ": "research", "agent: ": "delegating", "said: ": "reporting", "plan ": "planning",
	} {
		if strings.HasPrefix(j.Activity, prefix) {
			return name
		}
	}
	return "unknown"
}
