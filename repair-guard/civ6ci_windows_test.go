package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func civ6Fixture(t *testing.T) (civ6Access, *civ6Deployment, func()) {
	t.Helper()
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// CI runs as a dedicated service account. Pin the fixture's actual creator,
	// while production keeps civ6Owner from the embedded reviewed contract.
	creator, err := resourceIdentity(base, true)
	if err != nil || creator.Owner == "" {
		t.Fatalf("fixture owner unavailable: %v", err)
	}
	a := civ6Access{home: filepath.Join(base, "runtime"), receipt: filepath.Join(base, "source-receipt.json"),
		owner: creator.Owner, notBefore: 1, at: civ6Filetime(time.Now()),
		ancestor:  func(a, b string) bool { return a == b },
		protected: func(string) bool { return false }, qualified: func() (string, bool) { return civ6Floor, true },
		identity: func(p string) (audio2906Metadata, error) { return resourceIdentity(p, true) },
		read: func(p string, n int64) ([]byte, bool) {
			return conversationBytes(p, n, func(string) bool { return false })
		}}
	r := &civ6Deployment{Domain: "civ6-ci-2962-deployment-v1", ContractSHA256: hashBytes(civ6Contract), SourceHead: civ6Floor, Objects: map[string]civ6Object{}}
	save := func() {
		b, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(a.receipt, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return a, r, save
}
func civ6Record(t *testing.T, p string) civ6Object {
	t.Helper()
	m, e := resourceIdentity(p, true)
	if e != nil {
		t.Fatal(e)
	}
	return civ6Object{Volume: m.Volume, ID: m.ID, Created: m.Created, Owner: m.Owner, Size: m.Size, Written: m.Written, Changed: m.Changed, SHA256: strings.Repeat("a", 64)}
}
func civ6MakeRoot(t *testing.T, a civ6Access, r *civ6Deployment) {
	t.Helper()
	if e := os.Mkdir(a.home, 0700); e != nil {
		t.Fatal(e)
	}
	r.Parent = civ6Record(t, filepath.Dir(a.home))
	r.Root = civ6Record(t, a.home)
}

func TestCiv6InitialRootDoesNotAdoptUnknownExistingObjects(t *testing.T) {
	a, r, save := civ6Fixture(t)
	if !civ6Allowed(a.home, a) {
		t.Fatal("exclusive first root creation rejected")
	}
	for _, p := range []string{filepath.Dir(a.home), filepath.Join(a.home, "civ6-ci-queue.exe"), filepath.Join(a.home, "state"), a.home + "-other"} {
		if civ6Allowed(p, a) {
			t.Fatalf("non-root bootstrap accepted: %s", p)
		}
	}
	civ6MakeRoot(t, a, r)
	if civ6Allowed(a.home, a) {
		t.Fatal("unknown existing root adopted without external receipt")
	}
	save()
	if !civ6Allowed(a.home, a) {
		t.Fatal("bound root rejected")
	}
	p := filepath.Join(a.home, "civ6-ci-queue.exe")
	if !civ6Allowed(p, a) {
		t.Fatal("absent exact executable rejected")
	}
	if e := os.WriteFile(p, []byte("new deployment"), 0600); e != nil {
		t.Fatal(e)
	}
	if civ6Allowed(p, a) {
		t.Fatal("existing unrecorded executable adopted")
	}
	r.Objects["civ6-ci-queue.exe"] = civ6Record(t, p)
	save()
	if !civ6Allowed(p, a) {
		t.Fatal("recorded executable rejected")
	}
	if e := os.WriteFile(p, []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if civ6Allowed(p, a) {
		t.Fatal("changed executable accepted")
	}
}

func TestCiv6BoundariesAndReceiptBinding(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	save()
	for _, name := range []string{"civ6-ci-queue.exe", "civ6-ci-windows.exe", "civ6-ci-route.exe", "civ6-ci-queue.json", "actions-runner-win-x64-2.338.0.zip", "deployment-receipt.json", "civ6-ci-queue.exe.pr265.tmp", "civ6-ci-queue.json.pr265.backup", "state"} {
		if !civ6Allowed(filepath.Join(a.home, name), a) {
			t.Fatalf("exact absent leaf rejected: %s", name)
		}
	}
	for _, name := range []string{"unknown.exe", "config.json", "notify_config.json", "state/other.exe", "state/cache/a.exe", "civ6-ci-queue.exe:stream", "civ6-ci-queue.exe.bak", "civ6-ci-queue.exe.pr0.tmp", "civ6-ci-queue.exe.pr1234567.tmp", "actions-runner-win-x64-2.339.0.zip", "nested/civ6-ci-route.exe", "state/locks/os-Linux.lock", "state/entries/1-1-1/helper.exe"} {
		if civ6Allowed(filepath.Join(a.home, name), a) {
			t.Fatalf("unknown name accepted: %s", name)
		}
	}
	original := *r
	for _, mutate := range []func(){func() { r.Root.ID++ }, func() { r.Parent.ID++ }, func() { r.Root.Owner = "other" }, func() { r.SourceHead = strings.Repeat("f", 40) }, func() { r.ContractSHA256 = strings.Repeat("0", 64) }, func() { r.Domain = "other" }} {
		mutate()
		save()
		if civ6Allowed(a.home, a) {
			t.Fatal("unbound receipt accepted")
		}
		*r = original
	}
	save()
	for _, p := range []string{a.home, filepath.Dir(a.home), filepath.Join(a.home, "state"), a.receipt} {
		b := a
		b.protected = func(v string) bool { return same(v, p) }
		if civ6Allowed(filepath.Join(a.home, "state"), b) {
			t.Fatalf("live deny ignored: %s", p)
		}
	}
	b := a
	b.qualified = func() (string, bool) { return "", false }
	if civ6Allowed(a.home, b) {
		t.Fatal("unknown source accepted")
	}
	for _, body := range []string{`{}`, `{"Domain":"x","Domain":"y"}`, `{"unknown":true}`, `null`, `{} {}`} {
		if e := os.WriteFile(a.receipt, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if civ6Allowed(a.home, a) {
			t.Fatal("invalid receipt accepted")
		}
	}
}

func TestCiv6StateRequiresEachExistingIdentityAndNeverGrantsExecution(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	save()
	for _, name := range []string{"state", "state/entries", "state/cache", "state/locks", "state/evidence", "state/entries/123-1-456", "state/evidence/123-1-456"} {
		p := filepath.Join(a.home, filepath.FromSlash(name))
		if !civ6Allowed(p, a) {
			t.Fatalf("absent state dir rejected: %s", name)
		}
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
		if civ6Allowed(p, a) {
			t.Fatal("unrecorded existing state adopted")
		}
		r.Objects[name] = civ6Record(t, p)
		save()
		if !civ6Allowed(p, a) {
			t.Fatal("recorded state rejected")
		}
	}
	for _, name := range []string{"state/ledger.json", "state/.ledger-123.tmp", "state/tick.lock", "state/locks/os-Windows.lock", "state/cache/" + strings.Repeat("a", 64) + ".json", "state/entries/123-1-456/result.json", "state/entries/123-1-456/admin-collect.ps1", "state/entries/123-1-456/admin-collect-unstarted.ps1", "state/entries/123-1-456/collect-unstarted.log", "state/evidence/123-1-456/manifest.json"} {
		p := filepath.Join(a.home, filepath.FromSlash(name))
		if !civ6Allowed(p, a) {
			t.Fatalf("absent queue output rejected: %s", name)
		}
		if e := os.WriteFile(p, []byte("private output, must not be read"), 0600); e != nil {
			t.Fatal(e)
		}
		if civ6Allowed(p, a) {
			t.Fatal("unknown existing output adopted")
		}
		r.Objects[name] = civ6Record(t, p)
		save()
		// Classification must only read the one source receipt.
		b := a
		b.read = func(p string, n int64) ([]byte, bool) {
			if p != a.receipt {
				t.Fatal("resource content read")
			}
			return a.read(p, n)
		}
		if !civ6Allowed(p, b) {
			t.Fatalf("recorded state output rejected: %s", name)
		}
	}
	if civ6Allowed(filepath.Join(a.home, "state/entries/123-1-456/bootstrap.exe"), a) {
		t.Fatal("state executable authority added")
	}
	if civ6Allowed(filepath.Join(a.home, "state/evidence/123-1-456/root/unknown.json"), a) {
		t.Fatal("arbitrary evidence subtree accepted")
	}
}

func TestCiv6RejectsHardLinksAndDirectorySubstitution(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	p := filepath.Join(a.home, "civ6-ci-queue.exe")
	if e := os.WriteFile(p, []byte("binary"), 0600); e != nil {
		t.Fatal(e)
	}
	r.Objects["civ6-ci-queue.exe"] = civ6Record(t, p)
	save()
	if e := os.Link(p, filepath.Join(filepath.Dir(a.home), "alias")); e != nil {
		t.Fatal(e)
	}
	if civ6Allowed(p, a) {
		t.Fatal("multiply linked executable accepted")
	}
	q := filepath.Join(a.home, "civ6-ci-route.exe")
	if e := os.Mkdir(q, 0700); e != nil {
		t.Fatal(e)
	}
	r.Objects["civ6-ci-route.exe"] = civ6Record(t, q)
	save()
	if civ6Allowed(q, a) {
		t.Fatal("directory as executable accepted")
	}
}

func TestCiv6ContractNamesAndSourceRemainFixed(t *testing.T) {
	var c struct {
		Root, Source, Receipt string
		SourceFloor           string `json:"source_floor"`
		NotBefore             string `json:"root_created_not_before"`
		Owner                 string `json:"owner_sid"`
		ArchiveSHA            string `json:"archive_sha256"`
		ArchiveBytes          uint64 `json:"archive_bytes"`
		Leaves                []string
		RepositoryID          int64 `json:"repository_id"`
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(civ6Contract, &m) != nil {
		t.Fatal("contract invalid")
	}
	if json.Unmarshal(civ6Contract, &c) != nil {
		t.Fatal("contract invalid")
	}
	if c.Root != civ6Home || c.Source != civ6Source || c.SourceFloor != civ6Floor || !strings.HasSuffix(civ6Receipt, "/"+c.Receipt) || c.RepositoryID != 1406818713 || c.NotBefore != civ6NotBefore || c.Owner != civ6Owner || c.ArchiveSHA != civ6ArchiveSHA || c.ArchiveBytes != civ6ArchiveBytes || strings.ContainsRune(string(civ6Contract), '\r') {
		t.Fatal("canonical contract drift")
	}
	for _, name := range c.Leaves {
		if !civ6Leaves.MatchString(name) {
			t.Fatal("contract leaf drift")
		}
	}
	if len(c.Leaves) != 6 {
		t.Fatal("unexpected leaf count")
	}
}

func TestCiv6ForgedReceiptCannotAdoptRootPredatingContractOrDifferentOwner(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	save()
	// All receipt values match the real old root. Moving a public timestamp
	// into a receipt cannot change the compiled contract's independent cutoff.
	a.notBefore = r.Root.Created + 1
	a.at = r.Root.Created + 100
	if civ6Allowed(a.home, a) {
		t.Fatal("matching forged receipt adopted a root predating the contract")
	}
	a.notBefore = r.Root.Created
	if !civ6Allowed(a.home, a) {
		t.Fatal("root at contract boundary rejected")
	}
	a.owner = "different independently pinned owner"
	if civ6Allowed(a.home, a) {
		t.Fatal("receipt owner overrode contract owner")
	}
}

func TestCiv6MissingDeployedRootAndReceiptAliasesAreRefused(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	save()
	if e := os.Remove(a.home); e != nil {
		t.Fatal(e)
	}
	if civ6Allowed(a.home, a) {
		t.Fatal("lost deployed root silently recreated")
	}
	if e := os.Mkdir(a.home, 0700); e != nil {
		t.Fatal(e)
	}
	r.Root = civ6Record(t, a.home)
	if e := os.Remove(a.receipt); e != nil {
		t.Fatal(e)
	}
	save()
	if !civ6Allowed(a.home, a) {
		t.Fatal("fresh receipt fixture rejected")
	}
	b, e := os.ReadFile(a.receipt)
	if e != nil {
		t.Fatal(e)
	}
	for _, aliases := range []struct{ from, to string }{{`"Domain":`, `"domain":"override","Domain":`}, {`"Owner":`, `"owner":"override","Owner":`}} {
		bad := strings.Replace(string(b), aliases.from, aliases.to, 1)
		if e := os.WriteFile(a.receipt, []byte(bad), 0600); e != nil {
			t.Fatal(e)
		}
		if civ6Allowed(a.home, a) {
			t.Fatal("case alias accepted")
		}
	}
}

func TestCiv6ObjectCreationAndSourceAncestryStayBound(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	p := filepath.Join(a.home, "civ6-ci-queue.exe")
	if e := os.WriteFile(p, []byte("new"), 0600); e != nil {
		t.Fatal(e)
	}
	r.Objects[filepath.Base(p)] = civ6Record(t, p)
	save()
	original := a.identity
	a.identity = func(path string) (audio2906Metadata, error) {
		m, e := original(path)
		if same(path, p) {
			m.Created = r.Root.Created - 1
		}
		return m, e
	}
	w := r.Objects[filepath.Base(p)]
	w.Created = r.Root.Created - 1
	r.Objects[filepath.Base(p)] = w
	save()
	if civ6Allowed(p, a) {
		t.Fatal("old existing child adopted")
	}
	a.identity = original
	newHead := strings.Repeat("b", 40)
	a.qualified = func() (string, bool) { return newHead, true }
	a.ancestor = func(old, new string) bool { return old == civ6Floor && (new == civ6Floor || new == newHead) }
	if !civ6Allowed(a.home, a) {
		t.Fatal("ordinary later source commit revoked a valid deployment")
	}
	r.SourceHead = strings.Repeat("f", 40)
	save()
	if civ6Allowed(a.home, a) {
		t.Fatal("unrelated receipt source admitted")
	}
}

func TestCiv6GitEnvironmentCannotRedirectSource(t *testing.T) {
	got := civ6GitEnvironment([]string{"PATH=keep", "GIT_DIR=other", "git_work_tree=other", "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=remote.origin.url"})
	if len(got) != 1 || got[0] != "PATH=keep" {
		t.Fatal("Git override retained")
	}
}

func TestCiv6SourceIsAnExactGitRootAndCannotDiscoverAncestorRepository(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	currentFixtureGit(t, root, "init", "-q")
	if !civ6GitRoot(root) {
		t.Fatal("exact repository rejected")
	}
	child := filepath.Join(root, "not-a-repository")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if civ6GitRoot(child) {
		t.Fatal("ancestor repository qualified an unrelated child directory")
	}
}

func TestCiv6IndependentOwnerChecksAndClockFailClosed(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	p := filepath.Join(a.home, "civ6-ci-queue.exe")
	if err := os.WriteFile(p, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Objects[filepath.Base(p)] = civ6Record(t, p)
	save()
	if !civ6Allowed(p, a) {
		t.Fatal("initial owner fixture rejected")
	}
	for _, target := range []string{p, a.receipt} {
		b := a
		b.identity = func(path string) (audio2906Metadata, error) {
			m, e := a.identity(path)
			if same(path, target) {
				m.Owner = "different-owner"
			}
			return m, e
		}
		if civ6Allowed(p, b) {
			t.Fatal("child or receipt owner mismatch accepted")
		}
	}
	a.at = a.notBefore - 1
	if civ6Allowed(p, a) {
		t.Fatal("before-contract clock accepted")
	}
}

func TestCiv6JunctionAndReplacedRootAreRefused(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	save()
	moved := a.home + "-original"
	if e := os.Rename(a.home, moved); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(a.home, 0700); e != nil {
		t.Fatal(e)
	}
	if civ6Allowed(a.home, a) {
		t.Fatal("replacement root adopted")
	}
	if e := os.Remove(a.home); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", a.home, moved)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("junction fixture unavailable: %v %s", e, out)
	}
	defer os.Remove(a.home)
	if civ6Allowed(a.home, a) || civ6Allowed(filepath.Join(a.home, "civ6-ci-queue.exe"), a) {
		t.Fatal("junction adopted")
	}
}

func TestCiv6ReceiptHardlinkAndMissingQualificationNeverRead(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	save()
	if e := os.Link(a.receipt, a.receipt+"-alias"); e != nil {
		t.Fatal(e)
	}
	a.read = func(string, int64) ([]byte, bool) { t.Fatal("unqualified receipt read"); return nil, false }
	if civ6Allowed(a.home, a) {
		t.Fatal("hardlinked receipt accepted")
	}
	a.qualified = func() (string, bool) { return "", false }
	if civ6Allowed(a.home, a) {
		t.Fatal("unqualified source accepted")
	}
	a.qualified = func() (string, bool) { t.Fatal("unrelated namespace caused source access"); return "", false }
	if civ6Allowed(filepath.Join(a.home, "notify_config.json"), a) {
		t.Fatal("unknown leaf accepted")
	}
}

func TestCiv6ArchiveReceiptMetadataIsCheckedWithoutReadingArchiveContent(t *testing.T) {
	a, r, save := civ6Fixture(t)
	civ6MakeRoot(t, a, r)
	p := filepath.Join(a.home, "actions-runner-win-x64-2.338.0.zip")
	if e := os.WriteFile(p, []byte("small fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	r.Objects[filepath.Base(p)] = civ6Record(t, p)
	save()
	if civ6Allowed(p, a) {
		t.Fatal("wrong archive size/digest accepted")
	}
	w := r.Objects[filepath.Base(p)]
	w.Size = 104063355
	w.SHA256 = "f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f"
	r.Objects[filepath.Base(p)] = w
	save()
	original := a.identity
	a.identity = func(path string) (audio2906Metadata, error) {
		m, e := original(path)
		if same(path, p) {
			m.Size = 104063355
		}
		return m, e
	}
	if !civ6Allowed(p, a) {
		t.Fatal("exact archive metadata/digest rejected")
	}
}
