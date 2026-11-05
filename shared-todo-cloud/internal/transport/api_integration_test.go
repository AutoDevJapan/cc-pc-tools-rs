package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/httpapi"
	"autodevjapan/shared-todo-cloud/internal/store"
)

func TestCutoverAgainstHTTPAPIWithSyncCredential(t *testing.T) {
	ledger, err := store.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	// Seed the synthetic migrated state before the final local delta.
	lease, err := ledger.Acquire("fixture-import", "fixture-import-acquire")
	if err != nil {
		t.Fatal(err)
	}
	d, err := parseDocument([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	rows := []json.RawMessage{d.items[1], d.items[2]}
	if _, err = ledger.Commit(store.CommitRequest{RequestID: "fixture-import-commit", LeaseID: lease.ID, BaseRevision: 0, Upserts: rows, Meta: wire(d.meta)}); err != nil {
		t.Fatal(err)
	}
	if err = ledger.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	api, err := httpapi.New(ledger, httpapi.Config{AdminToken: strings.Repeat("a", 40), SyncToken: strings.Repeat("s", 40)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()
	home := t.TempDir()
	cfg := Config{BaseURL: server.URL, SyncToken: strings.Repeat("s", 40), ClientID: "fixture-pc", CachePath: filepath.Join(home, "todo.json")}
	client, err := NewClient(cfg.BaseURL, cfg.SyncToken)
	if err != nil {
		t.Fatal(err)
	}
	initialVersion, err := client.version(context.Background())
	if err != nil || initialVersion.Revision != 1 || initialVersion.StateID == "" {
		t.Fatal("actual version endpoint did not return ledger identity")
	}
	configPath, bridgePath := filepath.Join(home, "fixture.dpapi"), filepath.Join(home, "fixture-bridge.exe")
	if err = SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(bridgePath, []byte("fixture-not-executed"), 0600); err != nil {
		t.Fatal(err)
	}
	original := bytes.Replace([]byte(fixture), []byte(`"UPPER"`), []byte(`"LATEST"`), 1)
	if err = os.WriteFile(cfg.CachePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err != nil || !result.Activated || !result.LeaseReleased {
		t.Fatalf("API cutover failed: %v", err)
	}
	snap, err := ledger.Snapshot()
	if err != nil || snap.Revision != 2 {
		t.Fatal("API did not commit final delta")
	}
	afterVersion, err := client.version(context.Background())
	if err != nil || afterVersion.Revision != 2 || afterVersion.StateID != snap.StateID || afterVersion.StateID == initialVersion.StateID {
		t.Fatal("actual version endpoint did not advance identity atomically")
	}
	if !rawEqual(snap.Document, read(t, cfg.CachePath)) {
		t.Fatal("actual HTTP API and cache differ")
	}
	state, stateErr := readCacheState(cfg.CachePath)
	if stateErr != nil || state.StateID != snap.StateID {
		t.Fatal("activated cache lost its committed state identity")
	}
	if !bytes.Equal(read(t, result.BackupPath), original) {
		t.Fatal("actual API path lost the raw original")
	}
}

func TestCutoverUploadsFinalAssetsFromOwnedHome(t *testing.T) {
	ledger, c, s, home := assetFixture(t, nil)
	candidate, specAlias, spec, archivePath, archive := newAssetCandidate(t, s, home)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := Config{BaseURL: c.base, SyncToken: strings.Repeat("s", 40), ClientID: "fixture-cutover", CachePath: filepath.Join(home, "todo.json")}
	configPath, bridgePath := filepath.Join(home, "fixture.dpapi"), filepath.Join(home, "bridge.exe")
	if SaveConfig(configPath, cfg) != nil || os.WriteFile(bridgePath, []byte("not executed"), 0600) != nil || os.WriteFile(cfg.CachePath, candidate, 0600) != nil {
		t.Fatal("cutover fixture setup")
	}
	result, err := Cutover(context.Background(), cfg, configPath, bridgePath)
	if err != nil || !result.Activated {
		t.Fatalf("asset cutover: %v", err)
	}
	for alias, data := range map[string][]byte{specAlias: spec, archivePath: archive} {
		meta, err := ledger.ResolveAsset(alias)
		if err != nil || meta.SHA256 != digest(data) {
			t.Fatal("final local reference was committed without its owned asset")
		}
	}
}
