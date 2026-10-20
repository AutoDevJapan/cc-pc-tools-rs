package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratedQuestionFirstAssignmentTaskNumberFormats(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want bool
	}{
		{"legacy", "ToDo3084の画像を生成してください。", true},
		{"legacy-assignee", "担当ToDo3084の画像を生成してください。", true},
		{"number-label", "ToDo番号3084の画像を生成してください。", true},
		{"assignee-number-label", "担当ToDo番号3084の画像を生成してください。", true},
		{"wrong-number-label", "ToDo番号3099の画像を生成してください。", false},
		{"longer-number-label", "ToDo番号30840の画像を生成してください。", false},
		{"quoted-number-label", "引用: ToDo番号3084の画像を生成してください。", false},
		{"split-number-label", "ToDo番号\n3084の画像を生成してください。", false},
		{"natural-language-introduction", "本人の最新依頼を実行する専任担当。ToDo3084の画像を生成してください。", false},
		{"two-header-tasks", "本人の最新依頼を実行する専任担当。\nToDo3084とToDo3099の作業です。", false},
		{"repeated-header-task", "本人の最新依頼を実行する専任担当。\nToDo3084とToDo3084の作業です。", false},
		{"quoted-header-task", "本人の最新依頼を実行する専任担当。\n参考は「ToDo3084の例」です。", false},
		{"code-header-task", "本人の最新依頼を実行する専任担当。\n例は`ToDo3084`です。", false},
		{"ascii-quoted-header-task", "本人の最新依頼を実行する専任担当。\n例は\"ToDo3084\"です。", false},
		{"task-inside-identifier", "NotToDo3084の作業です。", false},
		{"later-header-task", "本人の最新依頼を実行する専任担当。\n今回の担当について。\nToDo3084の画像を生成してください。", false},
		{"wrong-task", "本人の最新依頼を実行する専任担当。ToDo3099の画像を生成してください。", false},
		{"wrong-first-task-with-later-mention", "本人の最新依頼を実行する専任担当。ToDo3099の作業です。\nToDo3084の画像もあります。", false},
		{"longer-task-number", "本人の最新依頼を実行する専任担当。ToDo30840の画像を生成してください。", false},
		{"task-only-in-quote", "本人の最新依頼を実行する専任担当。\n> ToDo3084の画像を生成してください。", false},
		{"unknown-introduction", "任意の説明。ToDo3084の画像を生成してください。", false},
		{"mention-in-body", "本人の最新依頼を実行する専任担当。別の作業です。\n参考: ToDo3084", false},
		{"repeated-introduction", "本人の最新依頼を実行する専任担当。本人の最新依頼を実行する専任担当。ToDo3084", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, image := generatedFixture(t)
			r := generatedRegistryFixture(t, s)
			bundle := generatedBundle(s)
			data, err := os.ReadFile(s.Rollout)
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(data, []byte{'\n'})
			index := bundle.Assignment.Line - 1
			var record map[string]any
			if err = json.Unmarshal(lines[index], &record); err != nil {
				t.Fatal(err)
			}
			record["payload"].(map[string]any)["content"] = []any{map[string]any{"type": "input_text", "text": test.text}}
			lines[index], err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			bundle.Assignment.SHA = bytesHash(lines[index])
			if err = os.WriteFile(s.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(filepath.Dir(s.Proof), "delegated-assignment.json")
			writeGeneratedBundle(t, manifest, bundle)
			_, err = r.register(context.Background(), manifest)
			if (err == nil) != test.want {
				t.Fatalf("registration success=%v, want %v: %v", err == nil, test.want, err)
			}
			if test.want {
				if _, err = qualifyGeneratedQuestionImage(context.Background(), image, bundle.Scope, generatedQuestionSpec{Load: r.load}); err != nil {
					t.Fatal("registered exact question did not qualify", err)
				}
			}
		})
	}
}

func TestGeneratedQuestionCreationTimeMustEqualOriginalCall(t *testing.T) {
	for _, test := range []struct {
		name   string
		offset time.Duration
		want   bool
	}{
		{"exact-fractional-time", 572 * time.Millisecond, true},
		{"lost-fractional-seconds", 0, false},
		{"wrong-timezone-offset", -9*time.Hour + 572*time.Millisecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, image := generatedFixture(t)
			r := generatedRegistryFixture(t, s)
			bundle := generatedBundle(s)
			data, err := os.ReadFile(s.Rollout)
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(data, []byte{'\n'})
			index := bundle.Events[0].Line - 1
			var event map[string]any
			if err = json.Unmarshal(lines[index], &event); err != nil {
				t.Fatal(err)
			}
			event["timestamp"] = s.Created.Add(572 * time.Millisecond)
			lines[index], err = json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			bundle.Events[0].SHA = bytesHash(lines[index])
			bundle.Created = s.Created.Add(test.offset)
			bundle.Expires = bundle.Created.Add(6 * 24 * time.Hour)
			if err = os.WriteFile(s.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(filepath.Dir(s.Proof), "fractional-time.json")
			writeGeneratedBundle(t, manifest, bundle)
			_, err = r.register(context.Background(), manifest)
			if (err == nil) != test.want {
				t.Fatalf("registration success=%v, want %v: %v", err == nil, test.want, err)
			}
			if test.want {
				if _, err = qualifyGeneratedQuestionImage(context.Background(), image, bundle.Scope, generatedQuestionSpec{Load: r.load}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGeneratedQuestionNumberLabelFirstAssignmentCannotBeRelabeled(t *testing.T) {
	s, _ := generatedFixture(t)
	r := generatedRegistryFixture(t, s)
	bundle := generatedBundle(s)
	data, err := os.ReadFile(s.Rollout)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	var assignment map[string]any
	if err = json.Unmarshal(lines[1], &assignment); err != nil {
		t.Fatal(err)
	}
	payload := assignment["payload"].(map[string]any)
	payload["content"] = []any{map[string]any{"type": "input_text", "text": "ToDo番号3084\n今回の画像作成を担当する。"}}
	lines[1], err = json.Marshal(assignment)
	if err != nil {
		t.Fatal(err)
	}
	payload["content"] = []any{map[string]any{"type": "input_text", "text": "ToDo番号3099\n別の画像作成を担当する。"}}
	later, err := json.Marshal(assignment)
	if err != nil {
		t.Fatal(err)
	}
	lines = append([][]byte{lines[0], lines[1], later}, lines[2:]...)
	if err = os.WriteFile(s.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
		t.Fatal(err)
	}
	bundle.Scope.Todo = 3099
	bundle.Assignment.Line = 3
	bundle.Assignment.SHA = bytesHash(later)
	for i := range bundle.Events {
		bundle.Events[i].Line++
	}
	generation, err := os.ReadFile(bundle.Images[0].Record)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(generation, &record); err != nil {
		t.Fatal(err)
	}
	record["todo"] = 3099
	generation, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(bundle.Images[0].Record, generation, 0600); err != nil {
		t.Fatal(err)
	}
	bundle.Images[0].RecordSHA = bytesHash(generation)
	manifest := filepath.Join(filepath.Dir(s.Proof), "later-dedicated-assignment.json")
	writeGeneratedBundle(t, manifest, bundle)
	if _, err = r.register(context.Background(), manifest); err == nil {
		t.Fatal("later task-number assignment relabeled an earlier image")
	}
}
