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

type generatedDiagnosticOutput struct {
	Stage, Code           string
	Validated, Registered bool
}

func TestGeneratedQuestionDiagnosticStopsBeforeUnqualifiedEvidenceRead(t *testing.T) {
	s, _ := generatedFixture(t)
	r := generatedRegistryFixture(t, s)
	manifest := filepath.Join(filepath.Dir(s.Proof), "evidence.json")
	writeGeneratedBundle(t, manifest, generatedBundle(s))
	// A healthy control uses the same read hooks and source fixture. The denied
	// run must fail at the guard, not at a broken fixture or missing file.
	if _, err := r.check(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	reads := 0
	r.Read = func(string, int64) ([]byte, error) {
		reads++
		return nil, os.ErrPermission
	}
	r.Guard = func(context.Context, string) (bool, error) { return false, nil }
	var out bytes.Buffer
	if code := generatedQuestionEvidenceCLI([]string{"--evidence", manifest, "--diagnostic"}, &out, r, true); code != 1 {
		t.Fatalf("exit=%d, output=%s", code, out.String())
	}
	var report generatedDiagnosticOutput
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err, out.String())
	}
	if report.Stage != "evidence_guard" || report.Code != "original_evidence_not_qualified" || report.Validated || report.Registered || reads != 0 {
		t.Fatalf("report=%+v reads=%d", report, reads)
	}
	if strings.Contains(out.String(), manifest) || strings.Contains(out.String(), s.Session) {
		t.Fatal("diagnostic exposed an input path or native session")
	}
}

func TestGeneratedQuestionDiagnosticReportsContractWithoutRegistering(t *testing.T) {
	for _, fictional := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "real-owner-data"}[fictional], func(t *testing.T) {
			s, _ := generatedFixture(t)
			r := generatedRegistryFixture(t, s)
			bundle := generatedBundle(s)
			bundle.Content.Fictional = fictional
			manifest := filepath.Join(filepath.Dir(s.Proof), "evidence.json")
			writeGeneratedBundle(t, manifest, bundle)
			before, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			code := generatedQuestionEvidenceCLI([]string{"--evidence", manifest, "--diagnostic"}, &out, r, true)
			var report generatedDiagnosticOutput
			if err = json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err, out.String())
			}
			wantStage, wantCode, wantExit := "complete", "evidence_validated", 0
			if !fictional {
				wantStage, wantCode, wantExit = "content_review", "fictional_data_required", 1
			}
			if code != wantExit || report.Stage != wantStage || report.Code != wantCode || report.Validated != fictional || report.Registered {
				t.Fatalf("exit=%d report=%+v", code, report)
			}
			after, err := os.ReadFile(manifest)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("check changed original evidence", err)
			}
			if _, err = os.Stat(r.Home); !os.IsNotExist(err) {
				t.Fatal("diagnostic created a key or registration", err)
			}
		})
	}
}

func TestGeneratedQuestionDiagnosticCannotChangeRegisterOrLegacyCLI(t *testing.T) {
	s, _ := generatedFixture(t)
	r := generatedRegistryFixture(t, s)
	manifest := filepath.Join(filepath.Dir(s.Proof), "evidence.json")
	writeGeneratedBundle(t, manifest, generatedBundle(s))
	r.Guard = func(context.Context, string) (bool, error) { return false, nil }
	for _, diagnostic := range []bool{false, true} {
		args := []string{"--evidence", manifest}
		if diagnostic {
			args = append(args, "--diagnostic")
		}
		var out bytes.Buffer
		if code := generatedQuestionEvidenceCLI(args, &out, r, false); code != 1 || out.String() != "generated question evidence not registered\n" {
			t.Fatalf("registration changed: exit=%d output=%s", code, out.String())
		}
	}
	var out bytes.Buffer
	if code := generatedQuestionEvidenceCLI([]string{"--evidence", manifest}, &out, r, true); code != 1 || out.String() != "generated question evidence not registered\n" {
		t.Fatalf("legacy check changed: exit=%d output=%s", code, out.String())
	}
}

func TestGeneratedQuestionDiagnosticSeparatesReadSchemaScopeAndNativeFailures(t *testing.T) {
	for _, kind := range []string{"read", "schema", "scope", "native"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := generatedFixture(t)
			r := generatedRegistryFixture(t, s)
			manifest := filepath.Join(filepath.Dir(s.Proof), "evidence.json")
			bundle := generatedBundle(s)
			writeGeneratedBundle(t, manifest, bundle)
			if _, err := r.check(context.Background(), manifest); err != nil {
				t.Fatal("healthy control failed", err)
			}
			wantStage := ""
			switch kind {
			case "read":
				read := r.Read
				r.Read = func(path string, limit int64) ([]byte, error) {
					if path == manifest {
						return nil, os.ErrPermission
					}
					return read(path, limit)
				}
				wantStage = "evidence_read"
			case "schema":
				if err := os.WriteFile(manifest, []byte(`{"not_a_bundle":true}`), 0600); err != nil {
					t.Fatal(err)
				}
				wantStage = "evidence_schema"
			case "scope":
				bundle.Scope.Todo++
				writeGeneratedBundle(t, manifest, bundle)
				wantStage = "bundle_contract"
			case "native":
				if err := os.WriteFile(s.Rollout, []byte("unrelated sensitive native record"), 0600); err != nil {
					t.Fatal(err)
				}
				wantStage = "image_validation"
			}
			var out bytes.Buffer
			if code := generatedQuestionEvidenceCLI([]string{"--evidence", manifest, "--diagnostic"}, &out, r, true); code != 1 {
				t.Fatalf("exit=%d output=%s", code, out.String())
			}
			var report generatedDiagnosticOutput
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Stage != wantStage || report.Validated || report.Registered {
				t.Fatalf("report=%+v error=%v", report, err)
			}
			if strings.Contains(out.String(), "sensitive") || strings.Contains(out.String(), "permission") || strings.Contains(out.String(), manifest) {
				t.Fatal("diagnostic exposed rejected input or an internal error")
			}
		})
	}
}
