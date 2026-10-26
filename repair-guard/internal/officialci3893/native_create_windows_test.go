package officialci3893

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicDirectoryCreationPinsItsOriginalObject(t *testing.T) {
	f := newFixture(t)
	p, err := f.fs.parentPins()
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	path := filepath.Join(f.parent, "atomic-created-directory")
	created, err := p.createDirectory(p.last, "atomic-created-directory", path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := identityOf(created)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+"-replaced"); err == nil {
		t.Fatal("creation handle allowed replacement")
	}
	if _, err = p.createDirectory(p.files[len(p.files)-2], "atomic-created-directory", path); err == nil {
		t.Fatal("CreateNew adopted an existing directory")
	}
	still, err := identityOf(created)
	if err != nil || still != id {
		t.Fatal("created object changed identity")
	}
}

func TestFreshAnchorRequiresActualEmptyDirectory(t *testing.T) {
	f := newFixture(t)
	if err := f.fs.EmptyParent(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.parent, "unknown-existing"), []byte("synthetic unrelated bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.fs.EmptyParent(); err == nil {
		t.Fatal("nonempty anchor adopted")
	}
}

func TestPublicEmptyDirectorySurvivesCreationAndConsumption(t *testing.T) {
	f := newFixture(t)
	raw := zipFixture(t, []Payload{{"public-empty/", nil}, {"bin/example.txt", []byte("software")}})
	f.archive.raw = raw
	f.engine.asset.Size = int64(len(raw))
	f.engine.asset.SHA256 = digest(raw)
	f.authority.evidence.Asset = f.engine.asset
	r := f.create(t)
	if info, e := os.Stat(f.path(r, "public-empty")); e != nil || !info.IsDir() {
		t.Fatal("empty public directory lost", e)
	}
	err := f.engine.Consume(context.Background(), r.ID, func(payloads []Payload) error {
		found := false
		for _, p := range payloads {
			if p.Name == "public-empty/" && len(p.Bytes) == 0 {
				found = true
			}
		}
		if !found {
			t.Error("verified empty directory missing from byte snapshot")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
