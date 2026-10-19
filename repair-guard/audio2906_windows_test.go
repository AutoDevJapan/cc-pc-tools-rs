package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func audio2906Fixture(t *testing.T) audio2906IO {
	t.Helper()
	root := dailyVideoFixtureRoot(t)
	io := audio2906IO{source: filepath.Join(root, "download.mp3"), receipt: filepath.Join(root, "receipt.json"),
		input: filepath.Join(root, "script.txt"), destination: filepath.Join(root, "production"),
		at: time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC), protected: func(string) bool { return false }, identity: audio2906Identity,
		qualified: func() bool { return true }, read: func(p string, n int64) ([]byte, bool) {
			return conversationBytes(p, n, func(string) bool { return false })
		}}
	if err := os.Mkdir(io.destination, 0700); err != nil {
		t.Fatal(err)
	}
	for p, b := range map[string]string{io.source: "synthetic media; never read by classification", io.receipt: `{"actor":2906,"production":2895,"operation":"normal Web Download"}`, io.input: "synthetic fixed script"} {
		if err := os.WriteFile(p, []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	io.receiptSHA = hashBytes([]byte(`{"actor":2906,"production":2895,"operation":"normal Web Download"}`))
	io.inputSHA = hashBytes([]byte("synthetic fixed script"))
	var err error
	io.expected, err = io.identity(io.source)
	if err != nil {
		t.Fatal(err)
	}
	return io
}

func TestAudio2906ExactDownloadWithoutMediaRead(t *testing.T) {
	io := audio2906Fixture(t)
	read := io.read
	reads := 0
	io.read = func(p string, n int64) ([]byte, bool) {
		if p != io.receipt && p != io.input {
			t.Fatal("classification tried reading a resource other than pinned operation evidence")
		}
		reads++
		return read(p, n)
	}
	if !audio2906AllowedWith(io.source, io) || reads != 4 {
		t.Fatal("verified normal download rejected")
	}
	for _, p := range []string{filepath.Dir(io.source), io.source + "-other", filepath.Join(filepath.Dir(io.source), "other.mp3"), io.destination} {
		calls := 0
		io.identity = func(string) (audio2906Metadata, error) { calls++; return io.expected, nil }
		if audio2906AllowedWith(p, io) || calls != 0 {
			t.Fatal("unrelated path reached metadata or was admitted")
		}
	}
}

func TestAudio2906RejectChangedIdentityAndMetadata(t *testing.T) {
	mutations := map[string]func(*audio2906Metadata){
		"volume": func(m *audio2906Metadata) { m.Volume++ }, "replacement": func(m *audio2906Metadata) { m.ID++ },
		"size": func(m *audio2906Metadata) { m.Size++ }, "created": func(m *audio2906Metadata) { m.Created++ },
		"written": func(m *audio2906Metadata) { m.Written++ }, "changed": func(m *audio2906Metadata) { m.Changed++ },
		"hardlink": func(m *audio2906Metadata) { m.Links++ }, "owner": func(m *audio2906Metadata) { m.Owner += "-other" },
		"junction": func(m *audio2906Metadata) { m.Attributes |= syscall.FILE_ATTRIBUTE_REPARSE_POINT },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			io := audio2906Fixture(t)
			m := io.expected
			mutate(&m)
			io.identity = func(string) (audio2906Metadata, error) { return m, nil }
			if audio2906AllowedWith(io.source, io) {
				t.Fatal("changed identity accepted")
			}
		})
	}
}

func TestAudio2906RejectEvidenceQualificationAndTime(t *testing.T) {
	for _, which := range []string{"operation", "script", "qualification", "deny-source", "deny-evidence", "before-download", "other-day"} {
		t.Run(which, func(t *testing.T) {
			io := audio2906Fixture(t)
			switch which {
			case "operation":
				if err := os.WriteFile(io.receipt, []byte(`{"actor":2907}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "script":
				if err := os.WriteFile(io.input, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "qualification":
				io.qualified = func() bool { return false }
			case "deny-source":
				io.protected = func(p string) bool { return same(p, io.source) }
			case "deny-evidence":
				io.protected = func(p string) bool { return same(p, io.receipt) }
			case "before-download":
				io.at = time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
			case "other-day":
				io.at = time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
			}
			if audio2906AllowedWith(io.source, io) {
				t.Fatal("invalid evidence or different production scope admitted")
			}
		})
	}
}

func TestAudio2906RecheckAfterEvidenceIO(t *testing.T) {
	for _, which := range []string{"metadata", "qualification", "deny"} {
		t.Run(which, func(t *testing.T) {
			io := audio2906Fixture(t)
			read := io.read
			changed := false
			io.read = func(p string, n int64) ([]byte, bool) { b, ok := read(p, n); changed = true; return b, ok }
			identity := io.identity
			io.identity = func(p string) (audio2906Metadata, error) {
				m, e := identity(p)
				if changed && which == "metadata" {
					m.Changed++
				}
				return m, e
			}
			io.qualified = func() bool { return !(changed && which == "qualification") }
			io.protected = func(string) bool { return changed && which == "deny" }
			if audio2906AllowedWith(io.source, io) {
				t.Fatal("change during evidence IO accepted")
			}
		})
	}
}

func TestAudio2906EvidenceChangesDuringClassification(t *testing.T) {
	io := audio2906Fixture(t)
	read := io.read
	io.read = func(p string, n int64) ([]byte, bool) {
		b, ok := read(p, n)
		if p == io.input {
			if err := os.WriteFile(io.receipt, []byte("tampered operation"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return b, ok
	}
	if audio2906AllowedWith(io.source, io) {
		t.Fatal("operation changed after initial read was accepted")
	}
}

func TestAudio2906RealReplacementAndHardlink(t *testing.T) {
	t.Run("replacement", func(t *testing.T) {
		io := audio2906Fixture(t)
		if err := os.Rename(io.source, io.source+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(io.source, []byte("different file"), 0600); err != nil {
			t.Fatal(err)
		}
		if audio2906AllowedWith(io.source, io) {
			t.Fatal("real replacement admitted")
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		io := audio2906Fixture(t)
		alias := io.source + ".alias"
		if err := os.Link(io.source, alias); err != nil {
			t.Fatal(err)
		}
		if audio2906AllowedWith(io.source, io) || audio2906AllowedWith(alias, io) {
			t.Fatal("real hardlink admitted")
		}
	})
}

func TestAudio2906RejectRealJunctionAncestor(t *testing.T) {
	io := audio2906Fixture(t)
	original := io.destination
	if err := os.Rename(original, original+"-real"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", original, original+"-real")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("owned junction fixture: %v (%s)", err, strings.TrimSpace(string(b)))
	}
	// Remove only the synthetic junction itself; Go TempDir owns the fixture.
	t.Cleanup(func() {
		if err := os.Remove(original); err != nil {
			t.Error(err)
		}
	})
	if audio2906AllowedWith(io.source, io) {
		t.Fatal("junction-based scope admitted")
	}
}
