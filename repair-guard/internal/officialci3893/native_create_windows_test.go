package officialci3893

import (
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
