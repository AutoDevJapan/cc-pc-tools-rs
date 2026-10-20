package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const snapshotLimit = 8 << 20

// The local command is separate from the patched /progress slash command.
// !codex progress works in an already-running official TUI without launching
// a model, proxy, daemon, or another interactive Codex session.
func progressArgs(args []string) ([]string, bool) {
	for len(args) > 0 && args[0] == "--no-daemon" {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "progress" {
		return args[1:], true
	}
	return nil, false
}

type progressSource struct {
	Status      string `json:"status"`
	Complete    bool   `json:"complete"`
	FailedReads int    `json:"failed_reads"`
	RecordsRead int    `json:"records_read"`
}

type progressTask struct {
	ID            int64  `json:"task_id"`
	Percent       *int   `json:"percent"`
	ProgressState string `json:"progress_state"`
	Execution     string `json:"execution"`
	Phase         string `json:"phase"`
	TaskType      string `json:"task_type"`
}

type progressJob struct {
	TodoIDs   []int64 `json:"todo_ids"`
	Execution string  `json:"execution"`
	Phase     string  `json:"phase"`
}

// Only numeric measurements and fixed-vocabulary metadata are decoded.
// Arbitrary labels, raw titles, paths and producer diagnostics stay unread.
type progressSnapshot struct {
	SchemaVersion int                       `json:"schema_version"`
	HeartbeatAt   string                    `json:"heartbeat_at"`
	Status        string                    `json:"status"`
	Sources       map[string]progressSource `json:"sources"`
	Tasks         []progressTask            `json:"tasks"`
	Jobs          []progressJob             `json:"jobs"`
	Truncated     bool                      `json:"truncated"`
}

func collectorBinary(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil || len(data) > 4096 {
		return ""
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil || len(m.CollectorSHA) != 64 {
		return ""
	}
	path := filepath.Join(filepath.Dir(root), "agent-progress", "agent-progress.exe")
	if digest(path) != strings.ToLower(m.CollectorSHA) {
		return ""
	}
	return path
}

type boundedSnapshot struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedSnapshot) Write(p []byte) (int, error) {
	remaining := snapshotLimit - b.Len()
	if len(p) > remaining {
		b.overflow = true
		_, _ = b.Buffer.Write(p[:remaining])
		return remaining, errors.New("snapshot limit")
	}
	return b.Buffer.Write(p)
}

func captureSnapshot(ctx context.Context, cmd *exec.Cmd) (progressSnapshot, int, error) {
	var output boundedSnapshot
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 200 * time.Millisecond
	hideCollectorWindow(cmd)
	err := cmd.Run()
	if ctx.Err() != nil || output.overflow {
		return progressSnapshot{}, 2, errors.New("snapshot timeout or size limit")
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return progressSnapshot{}, 2, errors.New("snapshot process failed")
		}
		code = 1 // Exit 1 may contain a valid partial observation.
	}
	var s progressSnapshot
	if json.Unmarshal(output.Bytes(), &s) != nil || s.SchemaVersion != 1 ||
		(s.Status != "ok" && s.Status != "degraded" && s.Status != "unavailable") {
		return progressSnapshot{}, 2, errors.New("invalid snapshot")
	}
	if _, err := time.Parse(time.RFC3339Nano, s.HeartbeatAt); err != nil {
		return progressSnapshot{}, 2, errors.New("invalid snapshot time")
	}
	if s.partial() {
		code = 1
	}
	return s, code, nil
}

func (s progressSnapshot) partial() bool {
	if s.Status != "ok" || s.Truncated {
		return true
	}
	for _, key := range []string{"todo", "jobs"} {
		source, ok := s.Sources[key]
		if !ok || !source.Complete || source.FailedReads != 0 {
			return true
		}
	}
	return false
}

func phaseText(phase, taskType string) string {
	switch phase {
	case "testing", "test":
		return "テスト中"
	case "implementation", "editing", "file_change":
		return "実装中"
	case "review", "reviewing":
		return "レビュー中"
	case "research", "searching":
		return "調査中"
	case "building", "build":
		return "ビルド中"
	}
	switch taskType {
	case "implementation":
		return "実装中"
	case "check", "test", "testing":
		return "テスト中"
	case "review":
		return "レビュー中"
	case "research":
		return "調査中"
	}
	return "作業中"
}

func executionText(task progressTask) string {
	switch task.Execution {
	case "running":
		return phaseText(task.Phase, task.TaskType)
	case "queued", "starting":
		return "起動待ち"
	case "supervising":
		return "見張りのみ稼働"
	case "finished":
		return "担当の処理終了"
	default:
		return "稼働を確認できていません"
	}
}

func writeProgress(w io.Writer, s progressSnapshot, selected map[int64]bool) {
	running, unknown, queued := 0, 0, 0
	for _, job := range s.Jobs {
		switch job.Execution {
		case "running":
			running++
		case "queued", "starting", "supervising":
			queued++
		case "finished", "not_observed":
		default:
			unknown++
		}
	}
	_, _ = fmt.Fprintf(w, "実行を確認した担当: %d件。起動待ち・見張り: %d件。稼働未確認: %d件。\n", running, queued, unknown)
	stamp, _ := time.Parse(time.RFC3339Nano, s.HeartbeatAt)
	_, _ = fmt.Fprintf(w, "取得時刻: %s\n", stamp.Local().Format("2006-01-02 15:04:05 MST"))
	partial := s.partial()
	for _, key := range []string{"todo", "jobs", "priority"} {
		if source, ok := s.Sources[key]; ok {
			if source.FailedReads > 0 {
				_, _ = fmt.Fprintf(w, "読めなかった記録: %d件（%s）。\n", source.FailedReads, key)
			}
		}
	}
	if partial {
		_, _ = io.WriteString(w, "部分取得です。確認できたデータを表示します。停止・完了とは判定しません。\n")
	}
	displayed, found := 0, map[int64]bool{}
	for _, task := range s.Tasks {
		if task.ID <= 0 || (!selected[task.ID] && (len(selected) > 0 || task.Execution != "running")) {
			continue
		}
		found[task.ID] = true
		if displayed >= 40 {
			continue
		}
		percent := "進捗未計測"
		if task.Percent != nil && *task.Percent >= 0 && *task.Percent <= 100 && (task.ProgressState == "measured" || task.ProgressState == "done") {
			percent = fmt.Sprintf("%d%%", *task.Percent)
		}
		_, _ = fmt.Fprintf(w, "ToDo %d: %s、%s\n", task.ID, executionText(task), percent)
		displayed++
	}
	ids := make([]int64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if !found[id] {
			_, _ = fmt.Fprintf(w, "ToDo %d: 今回の取得では確認できませんでした。\n", id)
		}
	}
	if displayed == 0 && len(selected) == 0 {
		if partial {
			_, _ = io.WriteString(w, "取得できた範囲では表示対象がありません。全担当の停止は確認できません。\n")
		} else {
			_, _ = io.WriteString(w, "取得できた範囲では処理中のToDoはありません。\n")
		}
	}
	if len(s.Tasks) > displayed && displayed == 40 {
		_, _ = io.WriteString(w, "表示は40件までです。codex progress --task <番号> で個別に確認できます。\n")
	}
}

func runProgress(args []string, root, pkg string, stdout, stderr io.Writer) int {
	selected := map[int64]bool{}
	collectorArgs := []string{"--once", "--deadline", "4s"}
	check := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--task":
			i++
			if i >= len(args) {
				_, _ = io.WriteString(stderr, "ToDo番号を指定してください。\n")
				return 2
			}
			id, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || id <= 0 {
				_, _ = io.WriteString(stderr, "ToDo番号が不正です。\n")
				return 2
			}
			if !selected[id] {
				selected[id] = true
				collectorArgs = append(collectorArgs, "--task", strconv.FormatInt(id, 10))
			}
		case "--help", "-h":
			_, _ = io.WriteString(stdout, "codex progress [--task <番号>] [--check]\n進捗を一度だけ取得します。--checkは起動経路の照合だけを行います。\n")
			return 0
		default:
			_, _ = io.WriteString(stderr, "codex progressの引数が不正です。\n")
			return 2
		}
	}
	collector := collectorBinary(root)
	if check {
		_, enabled := choose(nil, root, pkg, os.Getenv("CC_AGENT_PROGRESS_DISABLED") == "1")
		_, _ = fmt.Fprintf(stdout, "進捗TUIの起動照合: %t。取得処理のハッシュ一致: %t。\n", enabled, collector != "")
		if !enabled {
			_, _ = io.WriteString(stdout, "TUIの版・ハッシュ・無効化設定を確認してください。公式Codexの経路は維持しています。\n")
		}
		if collector == "" || !enabled {
			return 1
		}
		return 0
	}
	if collector == "" {
		_, _ = io.WriteString(stderr, "進捗の取得処理がmanifestのハッシュと一致しません。\n")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, collector, collectorArgs...)
	cmd.Env = collectorEnvironment(os.Environ())
	snapshot, code, err := captureSnapshot(ctx, cmd)
	if err != nil {
		_, _ = io.WriteString(stderr, "進捗を取得できませんでした。担当の停止や完了を意味しません。\n")
		return 2
	}
	writeProgress(stdout, snapshot, selected)
	return code
}

func collectorEnvironment(env []string) []string {
	// The local metadata reader needs OS locations, never account credentials.
	allowed := map[string]bool{"SYSTEMROOT": true, "WINDIR": true, "PATH": true, "TEMP": true, "TMP": true,
		"USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "PROGRAMDATA": true}
	result := make([]string, 0, len(allowed))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if allowed[strings.ToUpper(key)] {
			result = append(result, entry)
		}
	}
	return result
}
