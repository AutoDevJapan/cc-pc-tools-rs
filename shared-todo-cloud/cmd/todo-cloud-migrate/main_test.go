package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/httpapi"
	"autodevjapan/shared-todo-cloud/internal/store"
	"autodevjapan/shared-todo-cloud/internal/transport"
)

func cliFixture(t *testing.T) (home, out string) {
	t.Helper()
	home = filepath.Join(t.TempDir(), "home")
	out = filepath.Join(t.TempDir(), "export")
	if os.Mkdir(home, 0700) != nil || os.WriteFile(filepath.Join(home, "todo.lock"), nil, 0600) != nil || os.WriteFile(filepath.Join(home, "todo.json"), []byte(`{"items":[{"id":1,"title":"fiction-title-never-output","note":"fiction-note-never-output","status":"future","spec":"specs/missing.md","lastJob":9007199254740993,"lastjob":null}],"next_id":2}`), 0600) != nil {
		t.Fatal("CLI fixture failed")
	}
	return home, out
}

func execute(t *testing.T, args ...string) (result, int, string) {
	t.Helper()
	var b bytes.Buffer
	code := run(context.Background(), args, &b)
	var got result
	d := json.NewDecoder(bytes.NewReader(b.Bytes()))
	if d.Decode(&got) != nil {
		t.Fatal("CLI result is not JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		t.Fatal("CLI returned extra output")
	}
	for _, secret := range []string{"fiction-title-never-output", "fiction-note-never-output", "fiction-token-never-output", "fiction-private-argument", "fiction-server-body-never-output"} {
		if strings.Contains(b.String(), secret) {
			t.Fatal("CLI leaked task, argument, credential or server body")
		}
	}
	return got, code, b.String()
}

func TestCLIExportUploadAndVerifyWithProtectedConfig(t *testing.T) {
	home, dir := cliFixture(t)
	got, code, _ := execute(t, "export", "--home", home, "--out", dir)
	if code != 0 || !got.OK || got.Operation != "export" || got.Items != 1 || got.SpecMissing != 1 || got.Finalized {
		t.Fatal("CLI export aggregate differs")
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "fixture.db"))
	if err != nil {
		t.Fatal("fixture store failed")
	}
	defer s.Close()
	token := "fiction-token-never-output-" + strings.Repeat("s", 32)
	h, err := httpapi.New(s, httpapi.Config{AdminToken: strings.Repeat("a", 40), SyncToken: token})
	if err != nil {
		t.Fatal("fixture server failed")
	}
	server := httptest.NewServer(h)
	defer server.Close()
	config := filepath.Join(t.TempDir(), "protected.dpapi")
	if transport.SaveConfig(config, transport.Config{BaseURL: server.URL, SyncToken: token, ClientID: "fiction-client", CachePath: filepath.Join(home, "todo.json")}) != nil {
		t.Fatal("protected fixture config failed")
	}
	for _, args := range [][]string{
		{"upload", "--config", config, "--export", dir, "--assets-only"},
		{"upload", "--config", config, "--export", dir},
		{"verify", "--config", config, "--export", dir},
	} {
		got, code, output := execute(t, args...)
		if code != 0 || !got.OK {
			t.Fatalf("CLI %s failed with sanitized output: %s", args[0], output)
		}
		if strings.Contains(output, server.URL) || strings.Contains(output, home) || strings.Contains(output, config) || strings.Contains(output, "missing.md") {
			t.Fatal("CLI printed URL or filesystem path")
		}
		if got.Operation != "upload_assets" && !got.Finalized {
			t.Fatal("CLI did not verify finalized root")
		}
	}
}

func TestCLIAlwaysReturnsSanitizedJSONForInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"fiction-private-argument"},
		{"export", "--unknown=fiction-private-argument"},
		{"export", "--home", "fiction-private-argument"},
		{"upload", "--config", "fiction-private-argument", "--export", "missing"},
		{"verify", "--config", "fiction-private-argument", "--export", "missing"},
		{"verify", "--help"},
	} {
		got, code, _ := execute(t, args...)
		if code == 0 || got.OK || got.Error == "" {
			t.Fatal("invalid invocation succeeded")
		}
	}
}

func TestConfiguredBackupSyncsCloudBeforeExportAndFailsWithoutSync(t *testing.T) {
	home, _ := cliFixture(t)
	ledger, err := store.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	_, err = ledger.Update(store.UpdateRequest{RequestID: "seed-backup", Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"title":"cloud-current","note":"fresh value"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("s", 40)
	api, err := httpapi.New(ledger, httpapi.Config{AdminToken: strings.Repeat("a", 40), SyncToken: token})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()
	configPath, bridge := filepath.Join(home, "config.dpapi"), filepath.Join(home, "bridge.exe")
	cfg := transport.Config{BaseURL: server.URL, SyncToken: token, ClientID: "backup-fixture", CachePath: filepath.Join(home, "todo.json")}
	if transport.SaveConfig(configPath, cfg) != nil || os.WriteFile(bridge, []byte("not executed"), 0600) != nil {
		t.Fatal("backup config fixture")
	}
	marker, _ := json.Marshal(map[string]string{"bridge": bridge, "credentials": configPath})
	if os.WriteFile(filepath.Join(home, "todo.remote.json"), marker, 0600) != nil {
		t.Fatal("backup marker fixture")
	}
	out := filepath.Join(t.TempDir(), "backups")
	got, code, _ := execute(t, "backup", "--home", home, "--out", out, "--config", configPath)
	if code != 0 || got.Backup == nil || got.Backup.Items != 1 {
		t.Fatal("configured backup failed")
	}
	matches, _ := filepath.Glob(filepath.Join(out, "snapshot-*", "snapshot.json"))
	if len(matches) != 1 {
		t.Fatal("missing backup snapshot")
	}
	b, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Contains(b, []byte("cloud-current")) || bytes.Contains(b, []byte("fiction-title-never-output")) {
		t.Fatal("backup did not capture synchronized cloud state")
	}
	server.Close()
	second := filepath.Join(t.TempDir(), "offline-backup")
	got, code, _ = execute(t, "backup", "--home", home, "--out", second, "--config", configPath)
	if code == 0 || got.Error != "pre_backup_sync_failed" {
		t.Fatal("offline backup reported a current snapshot")
	}
	if _, err = os.Stat(second); !os.IsNotExist(err) {
		t.Fatal("failed sync created a backup generation")
	}
}

func TestCLIRemoteFailureContainsNoResponseBodyOrCredentials(t *testing.T) {
	home, dir := cliFixture(t)
	if _, code, _ := execute(t, "export", "--home", home, "--out", dir); code != 0 {
		t.Fatal("fixture export failed")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "fiction-server-body-never-output", http.StatusForbidden)
	}))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "protected.dpapi")
	if transport.SaveConfig(config, transport.Config{BaseURL: server.URL, SyncToken: "fiction-token-never-output", ClientID: "fiction-client", CachePath: filepath.Join(home, "todo.json")}) != nil {
		t.Fatal("fixture config failed")
	}
	got, code, _ := execute(t, "upload", "--config", config, "--export", dir)
	if code == 0 || got.OK || got.Error != "remote_http_error" || got.HTTPStatus != 403 || calls.Load() != 1 {
		t.Fatal("CLI remote error handling differs")
	}
}

func TestCLIBackupAndOfflineVerificationKeepOutputPrivate(t *testing.T) {
	home, _ := cliFixture(t)
	out := filepath.Join(t.TempDir(), "daily")
	first, code, output := execute(t, "backup", "--home", home, "--out", out)
	if code != 0 || !first.OK || first.Operation != "backup" || first.Backup == nil || first.Backup.Keep != 3 || !first.Backup.Created || first.Backup.RetainedSnapshots != 1 || !first.Backup.InventoryComplete || first.RootSHA256 != "" {
		t.Fatalf("CLI backup aggregate differs: %s", output)
	}
	second, code, _ := execute(t, "backup", "--home", home, "--out", out, "--keep", "2")
	if code != 0 || !second.OK || second.Backup == nil || !second.Backup.Reused || second.Backup.Keep != 2 || second.Backup.Created {
		t.Fatal("CLI same-day reuse or explicit keep differs")
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal("fixture backup directory unavailable")
	}
	var snapshot string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "snapshot-") && entry.IsDir() {
			snapshot = filepath.Join(out, entry.Name())
		}
	}
	if snapshot == "" {
		t.Fatal("CLI backup was not published")
	}
	verified, code, localOutput := execute(t, "verify", "--local", "--export", snapshot)
	if code != 0 || !verified.OK || verified.Operation != "verify_local" || verified.Finalized || verified.Items != 1 || verified.SpecMissing != 1 || verified.VerifiedBlobs != 1 {
		t.Fatal("offline verification did not validate the backup")
	}
	for _, value := range []string{home, out, snapshot, "snapshot-", ".todo-cloud-backup", "missing.md"} {
		if strings.Contains(output, value) || strings.Contains(localOutput, value) {
			t.Fatal("CLI backup printed a path or private reference")
		}
	}
	bad, code, _ := execute(t, "backup", "--home", home, "--out", out, "--keep", "0")
	if code == 0 || bad.OK || bad.Error != "invalid_backup_keep" {
		t.Fatal("invalid backup keep succeeded")
	}
	if os.WriteFile(filepath.Join(snapshot, "snapshot.json"), []byte("corrupt fiction"), 0600) != nil {
		t.Fatal("fixture corruption failed")
	}
	bad, code, _ = execute(t, "verify", "--local", "--export", snapshot)
	if code == 0 || bad.OK || bad.Error == "" {
		t.Fatal("offline verification accepted corrupted bytes")
	}
}
