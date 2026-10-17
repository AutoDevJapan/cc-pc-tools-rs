package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func releaseTestBlob(data []byte, role string) (releaseBlobReply, releaseBlobPin) {
	return releaseBlobReply{gitBlobHash(data), base64.StdEncoding.EncodeToString(data), "base64", len(data)}, releaseBlobPin{role, gitBlobHash(data), digest(data)}
}

func TestReleaseBlobBindsExactBytesAndKeepsOwnSnapshot(t *testing.T) {
	data := []byte("fixed fixture bytes\n")
	reply, pin := releaseTestBlob(data, "fixture")
	frozen, err := freezeReleaseBlob(reply, pin)
	if err != nil {
		t.Fatal(err)
	}
	reply.Content = base64.StdEncoding.EncodeToString([]byte("changed API response"))
	data[0] = 'X'
	first, err := frozen.verifiedCopy()
	if err != nil || string(first) != "fixed fixture bytes\n" {
		t.Fatal("consumed bytes differ from verified snapshot")
	}
	first[0] = 'Y'
	second, err := frozen.verifiedCopy()
	if err != nil || string(second) != "fixed fixture bytes\n" {
		t.Fatal("consumer altered frozen snapshot")
	}
	frozen.data[0] = 'Z'
	if _, err := frozen.verifiedCopy(); err == nil {
		t.Fatal("damaged snapshot accepted")
	}
	b, _ := json.Marshal(frozen)
	if string(b) != "{}" {
		t.Fatal("private source snapshot serialized")
	}
}

func TestReleaseBlobRejectsTamperingBeforeParsing(t *testing.T) {
	reply, pin := releaseTestBlob([]byte("test"), "fixture")
	for _, change := range []func(*releaseBlobReply){
		func(r *releaseBlobReply) { r.SHA = strings.Repeat("a", 40) },
		func(r *releaseBlobReply) { r.Size++ },
		func(r *releaseBlobReply) { r.Size = 2 << 20 },
		func(r *releaseBlobReply) { r.Encoding = "utf-8" },
		func(r *releaseBlobReply) { r.Content = "opaque-sensitive-invalid-base64" },
		func(r *releaseBlobReply) { r.Content = base64.StdEncoding.EncodeToString([]byte("evil")) },
	} {
		bad := reply
		change(&bad)
		if _, err := freezeReleaseBlob(bad, pin); err == nil || strings.Contains(err.Error(), "opaque-sensitive") {
			t.Fatal("tampered reply accepted or raw input leaked")
		}
	}
	badPin := pin
	badPin.sha256 = strings.Repeat("0", 64)
	if _, err := freezeReleaseBlob(reply, badPin); err == nil {
		t.Fatal("SHA256 binding ignored")
	}
}

func TestReleaseInventoryCannotLeakNamesOrAuthorizeJobs(t *testing.T) {
	data := []byte(`on: workflow_dispatch
permissions: {contents: read}
env:
  EXAMPLE: ${{ toJSON(secrets) }}
jobs:
  opaque-sensitive-job:
    runs-on: ubuntu-latest
    permissions: {actions: write, opaque-sensitive-capability: opaque-sensitive-access}
    secrets: inherit
    steps:
      - env:
          EXAMPLE: ${{ github['token'] }}
        run: |
          docker compose -f 'opaque-sensitive-compose.yml' -p "$PROJECT" run --rm service pytest
          docker run --env opaque-sensitive-variable=opaque-sensitive-value image
`)
	reply, pin := releaseTestBlob(data, "full-verification")
	var repo repository
	repo.ID, repo.FullName = 1285649124, "AutoDevJapan/opaque-sensitive-repository"
	calls := 0
	report, err := readReleaseInventoryBlobs(context.Background(), repo, []releaseBlobPin{pin}, func(_ context.Context, endpoint string, out any) error {
		calls++
		if endpoint != "repos/"+repo.FullName+"/git/blobs/"+pin.blob {
			return errors.New("unexpected read")
		}
		b, _ := json.Marshal(reply)
		return json.Unmarshal(b, out)
	})
	if err != nil || calls != 1 || report.ExecutionAuthorized || len(report.Workflows) != 1 {
		t.Fatalf("read-only inventory rejected: %v", err)
	}
	w := report.Workflows[0]
	if w.RootEnvSecretMentions != 1 || w.WorkflowTokenMentions != 1 || w.Jobs[0].SecretField != "inherit" {
		t.Fatal("root/whole-object/inherited secret or bracket token reference missed")
	}
	if w.Permissions.Declaration != "explicit-mapping" || w.Permissions.Entries[0].Capability != "contents" || w.Permissions.Entries[0].Access != "read" || w.Jobs[0].Permissions.Unresolved != 2 {
		t.Fatal("declared permissions confused with inheritance or unknown values")
	}
	if len(w.Jobs[0].Steps[0].DockerCalls) != 2 {
		t.Fatal("real literal Docker calls not parsed")
	}
	b, _ := json.Marshal(report)
	if strings.Contains(string(b), "opaque-sensitive") || strings.Contains(string(b), "PROJECT") || strings.Contains(string(b), "pytest") {
		t.Fatal("raw source, name or argument leaked")
	}
}

func TestReleaseInventoryStopsAtReadFailure(t *testing.T) {
	calls := 0
	_, err := readReleaseInventoryBlobs(context.Background(), repository{}, releaseBlobPins, func(context.Context, string, any) error {
		calls++
		return errors.New("opaque-sensitive-provider-error")
	})
	if calls != 1 || err == nil || strings.Contains(err.Error(), "opaque-sensitive") {
		t.Fatal("continued after failed read or leaked provider error")
	}
}

func TestReleaseWorkflowRejectsAmbiguousInputAndNeverExecutesShell(t *testing.T) {
	for _, source := range []string{
		"jobs: {one: {steps: [], steps: []}}",
		"jobs:\n  one: &job {steps: []}\n  two: *job\n",
		"jobs: []",
		"opaque-sensitive-invalid: [",
	} {
		if _, err := inspectReleaseWorkflow("fixture", []byte(source)); err == nil || strings.Contains(err.Error(), "opaque-sensitive") {
			t.Fatal("ambiguous YAML accepted or leaked")
		}
	}
	calls, expressions, err := inspectReleaseShell("echo 'docker run fake'\n# docker run fake\ndocker run $(opaque_sensitive_command) image\ndocker compose -f \"${{ inputs.file }}\" up\n")
	if err != nil || expressions != 1 || len(calls) != 2 || calls[0].Arguments[2].Kind != "shell-expansion" || !calls[1].UnresolvedComposeRef {
		t.Fatal("quoted/comment text confused with a call, or expansion treated as a fixed value")
	}
	calls, _, err = inspectReleaseShell("docker compose -p run -f 'fixture.yml' exec service true")
	if err != nil || len(calls) != 1 || calls[0].Operation != "compose-exec" {
		t.Fatal("project value mistaken for a compose subcommand")
	}
	if _, _, err = inspectReleaseShell("docker run '"); err == nil || strings.Contains(err.Error(), "docker") {
		t.Fatal("invalid shell accepted or source leaked")
	}
}
