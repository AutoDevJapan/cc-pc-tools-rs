//go:build windows && amd64

package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestArchiveRejectsEscapesBeforeWriting(t *testing.T) {
	for _, name := range []string{"../evil.exe", "/evil.exe", "bin\\evil.exe", "bin/evil.exe:stream", "bin/../evil.exe", "bin./evil.exe", "bin/evil.exe "} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write([]byte("fixture")); e != nil {
			t.Fatal(e)
		}
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
		if extractZip(`Z:\never-created-pr148-fixture`, b.Bytes()) == nil {
			t.Fatalf("accepted malicious archive %q", name)
		}
	}
}
func TestAdmissionBounds(t *testing.T) {
	for _, a := range []admission{{Token: "fixture", Jobs: "0", Threads: "1"}, {Token: "fixture", Jobs: "11", Threads: "1"}, {Token: "fixture", Jobs: "1", Threads: "0"}, {Token: "fixture", Jobs: "1", Threads: "11"}, {Token: strings.Repeat("x", 1025), Jobs: "1", Threads: "1"}, {Jobs: "1", Threads: "1"}} {
		if a.validate() == nil {
			t.Fatal("accepted invalid admission")
		}
	}
	if e := (admission{Token: "fixture", Jobs: "10", Threads: "10"}).validate(); e != nil {
		t.Fatal(e)
	}
}
