package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCiv6FixedRetryDiagnosticsRequireRecordedIdentity(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	rel := "state/entries/39423559387-1-118630300541/prestart-1/execute.log"
	path := filepath.Join(a.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"event":"execute-end","ok":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	for cur := path; cur != a.home; cur = filepath.Dir(cur) {
		v, _ := filepath.Rel(a.home, cur)
		r.Objects[filepath.ToSlash(v)] = civ6Record(t, cur)
	}
	save()
	if !civ6Allowed(path, a) {
		t.Fatal("recorded own retry diagnostic rejected")
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if civ6Allowed(path, a) {
		t.Fatal("changed copied diagnostic accepted")
	}
}

func TestCiv6RetryDiagnosticNamesAreFiniteAndCredentialFree(t *testing.T) {
	for _, p := range []string{
		"state/entries/39423559387-1-118630300541/prestart-1/execute.log",
		"state/evidence/39423559387-1-118630300541/prestart-1/manifest.json",
		"state/evidence/39423559387-1-118630300541/prestart-1/root/runtime/sdk-sccache-server.log",
		"state/evidence/39423559387-1-118630300541/prestart-1/root/launcher-result.json",
		"state/entries/39423559387-1-118630300541/prestart-2/execute.log",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner-configure-start.json",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner-configure-result.json",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner-listener-start.json",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner-listener-result.json",
	} {
		if dir, ok := civ6Name(filepath.FromSlash(p)); !ok || dir {
			t.Fatal("known diagnostic rejected", p)
		}
	}
	for _, p := range []string{
		"state/evidence/39423559387-1-118630300541/prestart-1/root/runtime/runner/.credentials",
		"state/evidence/39423559387-1-118630300541/prestart-1/root/runtime/runner/_diag/Runner.log",
		"state/evidence/39423559387-1-118630300541/prestart-1/root/runtime/sdk-msvc.log",
		"state/evidence/39423559387-1-118630300541/prestart-1/root/unknown.json",
		"state/entries/39423559387-1-118630300541/prestart-3/execute.log",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner/.credentials",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner/_diag/Runner.log",
		"state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner-unknown-result.json",
		"state/entries/39423559387-1-118630300542/prestart-1/execute.log",
		"state/entries/39423559387-1-118630300541/recovery-0/unknown.json",
	} {
		if _, ok := civ6Name(filepath.FromSlash(p)); ok {
			t.Fatal("unqualified sibling accepted", p)
		}
	}
}

func TestCiv6SecondRetryReceiptNeedsIdentity(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	rel := "state/evidence/39423559387-1-118630300541/prestart-2/root/runtime/runner-configure-result.json"
	path := filepath.Join(a.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"started":true,"ok":false,"exit":1,"observed":["http_404"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if civ6Allowed(path, a) {
		t.Fatal("unrecorded future receipt accepted")
	}
	for cur := path; cur != a.home; cur = filepath.Dir(cur) {
		v, _ := filepath.Rel(a.home, cur)
		r.Objects[filepath.ToSlash(v)] = civ6Record(t, cur)
	}
	save()
	if !civ6Allowed(path, a) {
		t.Fatal("recorded next retry receipt rejected")
	}
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if civ6Allowed(path, a) {
		t.Fatal("changed receipt accepted")
	}
}
