package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCI3888RuntimeMetadataFilesReplaceableDuringGitHub(t *testing.T) {
	a, reg, jobs, _, caller := ci3888RuntimeFixture()
	// The Windows service runner can expose TEMP through an alias. Keep the
	// native reader's exact physical-path check and canonicalize this fixture.
	dir := skillsFixtureRoot(t)
	files := map[string][]byte{}
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		files[path] = data
		return path
	}
	issuer := write("issuer.json", []byte("synthetic independently pinned creation"))
	stdout := write("create.json", files[issuer])
	rows := []skillsTask{}
	for _, row := range reg.Tasks {
		rows = append(rows, row)
	}
	registryRaw, err := json.Marshal(object{"projects": object{"cc-pc-tools-rs": reg.Root}, "items": rows})
	if err != nil {
		t.Fatal(err)
	}
	registry := write("registry.json", registryRaw)
	jobFiles := map[string]string{}
	for id, job := range jobs {
		raw, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		jobFiles[id] = write(id+".json", raw)
	}
	a.registry = func(ctx context.Context) (skillsRegistry, bool) {
		raw, ok := ci3888RuntimeRead(registry, 1<<20)
		if !ok {
			return skillsRegistry{}, false
		}
		return official3894RegistryProjection(ownerDeadlineReader{ctx, bytes.NewReader(raw)})
	}
	a.job = func(id string) (object, bool) {
		path, ok := jobFiles[id]
		if !ok {
			return nil, false
		}
		return ci3888RuntimeJobFile(path, id)
	}
	a.read = func(path string, limit int64) ([]byte, bool) {
		switch path {
		case ci3888RuntimeCreationOutput:
			return ci3888RuntimeRead(stdout, limit)
		case ci3888RuntimeIssuerRecord:
			return ci3888RuntimeRead(issuer, limit)
		}
		return nil, false
	}
	replaced := 0
	a.github = func(context.Context) error {
		for path, raw := range files {
			staged := path + ".replacement"
			if err := os.WriteFile(staged, raw, 0600); err != nil {
				return err
			}
			if err := os.Rename(staged, path); err != nil {
				return err
			}
			replaced++
		}
		return nil
	}
	if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, caller, a); err != nil {
		t.Fatal("normal metadata replacement blocked during GitHub query:", err)
	}
	if replaced != len(files) || replaced < 7 {
		t.Fatal("the external query did not replace every real metadata fixture")
	}
}

func TestCI3888CreationCLIDoesNotGrantPurpose(t *testing.T) {
	for _, args := range [][]string{{}, {ci3888CreationFlag, ci3888RuntimeRoot}, {ci3888CreationFlag, ci3888RuntimeFlag, ci3888RuntimeRoot}, {ci3888CreationFlag, ci3888CreationFlag}, {ci3888CreationFlag + "=true"}} {
		var out bytes.Buffer
		calls := 0
		code := runCI3888CreationCLI(args, &out, func() error { calls++; return nil })
		if code != 2 || calls != 0 {
			t.Fatal("mixed/invalid creation arguments reached metadata read")
		}
	}
	for _, verifyErr := range []error{nil, errors.New("private synthetic error"), official3894Error("runtime_creation_fields_mismatch")} {
		var out bytes.Buffer
		code := runCI3888CreationCLI([]string{ci3888CreationFlag}, &out, func() error { return verifyErr })
		got, ok := conversationJSON(out.Bytes())
		if !ok || (code == 0) != (verifyErr == nil) || got["creation_verified"] != (verifyErr == nil) || bytes.Contains(out.Bytes(), []byte("private synthetic")) {
			t.Fatal("creation metadata result leaked an error or reported false success")
		}
		for _, key := range []string{"purpose_verified", "access_allowed", "child_data_access", "sdk_qualified", "sdk_consumed", "launch_performed", "original_registry_recovered"} {
			if got[key] != false {
				t.Fatalf("creation-only probe granted %s", key)
			}
		}
	}
}

func TestCI3888RuntimeForeignCallerDoesNotOpenRegistry(t *testing.T) {
	a, _, _, _, _ := ci3888RuntimeFixture()
	a.registry = func(context.Context) (skillsRegistry, bool) {
		t.Fatal("foreign caller opened the registry")
		return skillsRegistry{}, false
	}
	if err := ci3888RuntimeVerifyWith(context.Background(), ci3888RuntimeRoot, "20261030-093140-todo-4143", a); err != official3894Error("runtime_native_provider_required") {
		t.Fatal("foreign caller did not fail at its own identity boundary", err)
	}
}
