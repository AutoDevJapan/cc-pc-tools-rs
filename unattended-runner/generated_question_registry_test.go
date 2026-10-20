package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generatedBundle(s generatedQuestionSpec) generatedQuestionBundle {
	return generatedQuestionBundle{Domain: generatedEvidenceDomain, Scope: s.Scope, Session: s.Session, Proof: s.Proof, ProofSHA: s.ProofSHA, Rollout: s.Rollout, Created: s.Created, Expires: s.Expires, Cell: s.Cell, Content: generatedContentReview{true, true, true, "generator reviewed fictional data for this owner question"}, Images: s.Images, Events: s.Events, SessionSHA: s.SessionSHA, Assignment: s.Assignment}
}
func generatedRegistryFixture(t *testing.T, s generatedQuestionSpec) generatedQuestionRegistry {
	t.Helper()
	root := filepath.Dir(s.Proof)
	copyKey := func(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
	return generatedQuestionRegistry{Home: filepath.Join(root, "purpose-store"), Artifacts: filepath.Join(root, "generated_images"), Sessions: filepath.Join(root, "sessions"), Guard: s.Guard, Read: s.Read, Task: func(todo uint64) bool { return todo == 3084 || todo == 3099 }, Destination: func(scope generatedQuestionScope) bool {
		return scope.Owner == s.Scope.Owner && scope.Guild == s.Scope.Guild && scope.Forum == s.Scope.Forum
	}, SourceClear: s.SourceClear, Now: s.Now, Protect: copyKey, Unprotect: copyKey}
}
func writeGeneratedBundle(t *testing.T, path string, b generatedQuestionBundle) {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedQuestionRegistrySupportsLaterTaskWithoutCodeOrDeployment(t *testing.T) {
	s, image := generatedFixture(t)
	r := generatedRegistryFixture(t, s)
	ctx := context.Background()
	spec := generatedQuestionSpec{Load: r.load}
	if _, err := qualifyGeneratedQuestionImage(ctx, image, s.Scope, spec); err == nil {
		t.Fatal("unregistered image qualified")
	}
	manifest := filepath.Join(filepath.Dir(s.Proof), "evidence.json")
	writeGeneratedBundle(t, manifest, generatedBundle(s))
	var out bytes.Buffer
	if generatedQuestionRegisterCLI([]string{"--evidence", manifest}, &out, r) != 0 {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), image) || strings.Contains(out.String(), "client_resource") {
		t.Fatal("registration exported source or reclassified it")
	}
	if _, err := qualifyGeneratedQuestionImage(ctx, image, s.Scope, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := r.register(ctx, manifest); err != nil {
		t.Fatal("identical registration must be idempotent", err)
	}
	// A new generation, task, session and question use the same running validator.
	later, laterImage := generatedFixtureAt(t, filepath.Dir(s.Proof), 3099, "01234567-0123-0123-0123-012345678999")
	later.Scope.Body = strings.Repeat("c", 64)
	later.Scope.Decision = strings.Repeat("d", 64)
	writeGeneratedBundle(t, manifest, generatedBundle(later))
	if _, err := r.register(ctx, manifest); err != nil {
		t.Fatal("later native generation did not register", err)
	}
	if _, err := qualifyGeneratedQuestionImage(ctx, laterImage, later.Scope, spec); err != nil {
		t.Fatal("later question requires no code pin", err)
	}
	if _, err := qualifyGeneratedQuestionImage(ctx, image, s.Scope, spec); err != nil {
		t.Fatal("registering a later task changed existing qualification", err)
	}
	if _, err := qualifyGeneratedQuestionImage(ctx, image, later.Scope, spec); err == nil {
		t.Fatal("original image reused under later task")
	}
}

func TestGeneratedQuestionRegistryRejectsForgedScopeSourcesAndReceipts(t *testing.T) {
	for _, kind := range []string{"other-task-summary", "other-destination", "copy", "native-bytes", "native-tool", "assignment-role", "no-content-review", "signed-receipt", "missing-key", "protected-evidence"} {
		t.Run(kind, func(t *testing.T) {
			s, image := generatedFixture(t)
			r := generatedRegistryFixture(t, s)
			b := generatedBundle(s)
			manifest := filepath.Join(filepath.Dir(s.Proof), "evidence.json")
			ctx := context.Background()
			switch kind {
			case "other-task-summary":
				b.Scope.Todo = 3099
				data, _ := os.ReadFile(b.Images[0].Record)
				var g map[string]any
				_ = json.Unmarshal(data, &g)
				g["todo"] = 3099
				data, _ = json.Marshal(g)
				_ = os.WriteFile(b.Images[0].Record, data, 0600)
				b.Images[0].RecordSHA = bytesHash(data)
			case "other-destination":
				b.Scope.Forum = "999"
			case "copy":
				data, _ := os.ReadFile(image)
				copyPath := filepath.Join(filepath.Dir(image), "exec-01234567-0123-0123-0123-012345678903.png")
				_ = os.WriteFile(copyPath, data, 0600)
				b.Images[0].Path = copyPath
				gdata, _ := os.ReadFile(b.Images[0].Record)
				var g map[string]any
				_ = json.Unmarshal(gdata, &g)
				g["originalPath"] = copyPath
				gdata, _ = json.Marshal(g)
				_ = os.WriteFile(b.Images[0].Record, gdata, 0600)
				b.Images[0].RecordSHA = bytesHash(gdata)
			case "native-bytes":
				data := []byte("\x89PNG\r\n\x1a\nchanged")
				_ = os.WriteFile(image, data, 0600)
				b.Images[0].SHA256 = bytesHash(data)
				b.Images[0].Size = int64(len(data))
			case "native-tool":
				data, _ := os.ReadFile(s.Rollout)
				lines := bytes.Split(data, []byte{'\n'})
				line := b.Events[0].Line - 1
				lines[line] = bytes.ReplaceAll(lines[line], []byte(`"name":"exec"`), []byte(`"name":"exec_command"`))
				b.Events[0].Name = "exec_command"
				b.Events[0].SHA = bytesHash(lines[line])
				_ = os.WriteFile(s.Rollout, bytes.Join(lines, []byte{'\n'}), 0600)
			case "assignment-role":
				data, _ := os.ReadFile(s.Rollout)
				lines := bytes.Split(data, []byte{'\n'})
				line := b.Assignment.Line - 1
				lines[line] = bytes.ReplaceAll(lines[line], []byte(`"role":"user"`), []byte(`"role":"assistant"`))
				b.Assignment.SHA = bytesHash(lines[line])
				_ = os.WriteFile(s.Rollout, bytes.Join(lines, []byte{'\n'}), 0600)
			case "no-content-review":
				b.Content.NoConfidential = false
			case "protected-evidence":
				r.Guard = func(context.Context, string) (bool, error) { return false, nil }
			}
			writeGeneratedBundle(t, manifest, b)
			_, err := r.register(ctx, manifest)
			if kind != "signed-receipt" && kind != "missing-key" {
				if err == nil {
					t.Fatal("forged or ineligible evidence registered")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "missing-key" {
				_ = os.Remove(filepath.Join(r.Home, "purpose-key.dpapi"))
			} else {
				p := filepath.Join(r.Home, generatedReceiptName(s.Scope, image))
				data, _ := os.ReadFile(p)
				var e generatedQuestionEnvelope
				_ = json.Unmarshal(data, &e)
				e.MAC = strings.Repeat("0", 64)
				data, _ = json.Marshal(e)
				_ = os.WriteFile(p, data, 0600)
			}
			if _, err = qualifyGeneratedQuestionImage(ctx, image, s.Scope, generatedQuestionSpec{Load: r.load}); err == nil {
				t.Fatal("tampered signature/key accepted")
			}
		})
	}
}
