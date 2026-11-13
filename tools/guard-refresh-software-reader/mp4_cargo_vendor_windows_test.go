//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCargoVendorSealed(t *testing.T) {
	for _, path := range []string{mp4PublicCargoRegistry, "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe", "D:/CC/Tools/codex-jobs/cargo_gate.exe"} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Skip("explicit workstation toolchain integration")
		} else if err != nil {
			t.Fatal(err)
		}
	}
	original, e := os.ReadFile(filepath.Join("..", "..", "Cargo.lock"))
	if e != nil {
		t.Fatal(e)
	}
	defer clear(original)
	root := physicalSoftwareFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ctx, config, e := prepareMP4CargoContext(ctx, softwareBuildPlan{Target: root})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := config.Close(); e != nil {
			t.Error(e)
		}
	}()
	proof := askBuildEvidence{}
	var sources []updateObject
	add := func(path, text string) {
		raw := []byte(text)
		proof.Inputs = append(proof.Inputs, buildSourceRecord{Path: path, SHA: digest(raw)})
		sources = append(sources, &fakeUpdateObject{raw: raw})
	}
	proof.Inputs = append(proof.Inputs, buildSourceRecord{Path: "Cargo.lock", SHA: digest(original)})
	sources = append(sources, &fakeUpdateObject{raw: original})
	for _, name := range []string{"cc-ask", "cc-chrome", "cc-job-policy", "cc-live", "cc-local-llm", "cc-task-title"} {
		manifest := "[package]\nname=\"" + name + "\"\nversion=\"0.1.0\"\nedition=\"2021\"\n"
		if name == "cc-ask" {
			manifest += "[dependencies]\nring=\"=0.17.14\"\n"
		}
		add(name+"/Cargo.toml", manifest)
		add(name+"/src/lib.rs", "pub fn owned(){}\n")
	}
	snapshot, e := materializeMP4BuildSnapshotContext(ctx, softwareBuildPlan{Target: root, Source: mp4Source}, proof, sources)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := snapshot.Close(); e != nil {
			t.Error(e)
		}
	}()
	manifest := filepath.Join(snapshot.Root, "vendor", "ring-0.17.14", "Cargo.toml")
	if _, e := os.Stat(manifest); e != nil {
		t.Fatal("official vendored ring input missing before compilation", e)
	}
	if _, e := os.Stat(filepath.Join(snapshot.Root, "vendor", "ring-0.17.14", "third_party", "fiat", "curve25519_64_msvc.h")); e != nil {
		t.Fatal("ring relative include input missing", e)
	}
	if !snapshot.exact() || snapshot.ManifestSHA == "" {
		t.Fatal("vendor absent from sealed manifest")
	}
	if os.WriteFile(manifest, []byte("changed"), 0600) == nil {
		t.Fatal("vendored source writable during compilation")
	}
	if os.WriteFile(filepath.Join(snapshot.Root, "vendor", "injected.rs"), []byte("foreign"), 0600) == nil {
		t.Fatal("vendor namespace accepts injected compiler input")
	}
	if strings.Contains(mp4CargoWorkspace, "ring") {
		t.Fatal("vendor repair changed application dependency recipe")
	}
	args := []string{"build", "--offline", "--locked", "--release", "--manifest-path", filepath.Join(snapshot.Root, "Cargo.toml"), "--target-dir", filepath.Join(root, "target"), "-p", "cc-ask", "--lib"}
	args = append(args, cargoVendorBuildArgs(snapshot.Root)...)
	built := askOwnedChildAt(compilerContext(ctx), ctx.Value(mp4CargoDirectory{}).(string), "D:/CC/Tools/codex-jobs/cargo_gate.exe", args...)
	defer clear(built.Raw)
	if !built.Started || !built.Assigned || !built.WaitVerified || !built.Reaped || built.TimedOut || built.Exit != 0 {
		t.Fatalf("sealed vendor compiler failed: exit=%d diagnostic=%+v", built.Exit, built.Diagnostic)
	}
	if !snapshot.exact() || !config.cargoConfigurationUnchanged() {
		t.Fatal("compiler input namespace changed during offline build")
	}
}

func TestCargoVendorChecksumProtocolRejectsAliasesAndDuplicateKeys(t *testing.T) {
	sha := strings.Repeat("a", 64)
	base := `{"package":"` + sha + `","files":{"src/lib.rs":"` + sha + `"}}`
	if files, packageSHA, ok := cargoVendorChecksums([]byte(base)); !ok || len(files) != 1 || packageSHA != sha {
		t.Fatal("valid official checksum format rejected")
	}
	for _, raw := range []string{
		strings.Replace(base, `"package":`, `"package":"`+sha+`","package":`, 1),
		strings.Replace(base, `"src/lib.rs":"`+sha+`"`, `"src/lib.rs":"`+sha+`","SRC/lib.rs":"`+sha+`"`, 1),
		strings.Replace(base, "src/lib.rs", "../foreign.rs", 1),
		strings.Replace(base, "src/lib.rs", "src/../foreign.rs", 1),
		strings.Replace(base, "src/lib.rs", "C:/foreign.rs", 1),
		strings.Replace(base, "src/lib.rs", ".cargo-checksum.json", 1),
		strings.Replace(base, `"package":"`+sha+`"`, `"package":null`, 1),
		base + `{}`,
	} {
		if _, _, ok := cargoVendorChecksums([]byte(raw)); ok {
			t.Fatal("unbound checksum metadata accepted")
		}
	}
}

func TestCargoVendorGeneratedMetadataIsOnlySelfConsistency(t *testing.T) {
	for _, fault := range []string{"normal", "file_changed", "package_changed", "missing_package", "extra_package", "extra_file", "missing_file", "link", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			vendor := filepath.Join(root, "vendor")
			packageRoot := filepath.Join(vendor, "owned-1.0.0")
			if os.Mkdir(vendor, 0700) != nil || os.Mkdir(packageRoot, 0700) != nil {
				t.Fatal("fixture directories")
			}
			sha := strings.Repeat("a", 64)
			lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + sha + "\"\n")
			raw := []byte("owned source\n")
			checksum, e := json.Marshal(map[string]any{"package": sha, "files": map[string]string{"source.rs": digest(raw)}})
			if e != nil || os.WriteFile(filepath.Join(packageRoot, ".cargo-checksum.json"), checksum, 0600) != nil || os.WriteFile(filepath.Join(packageRoot, "source.rs"), raw, 0600) != nil {
				t.Fatal("fixture inputs")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "file_changed":
				e = os.WriteFile(filepath.Join(packageRoot, "source.rs"), []byte("changed"), 0600)
			case "package_changed":
				e = os.WriteFile(filepath.Join(packageRoot, ".cargo-checksum.json"), []byte(strings.Replace(string(checksum), sha, strings.Repeat("b", 64), 1)), 0600)
			case "missing_package":
				e = os.Rename(packageRoot, filepath.Join(vendor, "foreign-1.0.0"))
			case "extra_package":
				e = os.Mkdir(filepath.Join(vendor, "foreign-1.0.0"), 0700)
			case "extra_file":
				e = os.WriteFile(filepath.Join(packageRoot, "injected.rs"), raw, 0600)
			case "missing_file":
				e = os.Remove(filepath.Join(packageRoot, "source.rs"))
			case "link":
				if e = os.Remove(filepath.Join(packageRoot, "source.rs")); e == nil {
					e = os.Symlink(filepath.Join(packageRoot, ".cargo-checksum.json"), filepath.Join(packageRoot, "source.rs"))
				}
			case "canceled":
				cancel()
			}
			if e != nil {
				t.Fatal("fixture mutation", e)
			}
			s := &softwareBuildSnapshot{Root: root, paths: map[string]bool{root: true}}
			defer func() {
				if e := s.Close(); e != nil {
					t.Error(e)
				}
			}()
			err := s.validateCargoVendor(ctx, lock)
			if (err == nil) != (fault == "normal") {
				t.Fatal("unexpected admission", fault, err)
			}
			if fault == "normal" {
				if !s.exact() || s.sealDirectories() != nil {
					t.Fatal("validated namespace not sealed")
				}
				if os.WriteFile(filepath.Join(packageRoot, "source.rs"), raw, 0600) == nil {
					t.Fatal("held compiler file changed")
				}
			}
		})
	}
}

func TestCargoVendorRejectsSelfConsistentFilesWithoutLockedArchive(t *testing.T) {
	root := physicalSoftwareFixture(t)
	packageRoot := filepath.Join(root, "vendor", "owned-1.0.0")
	if os.MkdirAll(packageRoot, 0700) != nil {
		t.Fatal("fixture directories")
	}
	sha := strings.Repeat("a", 64)
	raw := []byte("unverified archive contents\n")
	metadata, _ := json.Marshal(map[string]any{"package": sha, "files": map[string]string{"source.rs": digest(raw)}})
	if os.WriteFile(filepath.Join(packageRoot, "source.rs"), raw, 0600) != nil || os.WriteFile(filepath.Join(packageRoot, ".cargo-checksum.json"), metadata, 0600) != nil {
		t.Fatal("fixture files")
	}
	lock := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + sha + "\"\n")
	s := &softwareBuildSnapshot{Root: root, paths: map[string]bool{root: true}}
	defer s.Close()
	if s.admitCargoVendor(context.Background(), lock) == nil {
		t.Fatal("self-consistent files admitted without hashing locked archive bytes")
	}
}
