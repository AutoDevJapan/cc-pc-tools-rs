//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCargoDerivedLockRequiresOriginalVersionSourceChecksum(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	base := []byte("version = 4\n[[package]]\nname = \"owned\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"" + checksum + "\"\n")
	if !cargoLockKeepsOriginalPins(base, base) {
		t.Fatal("same pins rejected")
	}
	for _, changed := range []string{strings.Replace(string(base), "1.0.0", "2.0.0", 1), strings.Replace(string(base), checksum, strings.Repeat("b", 64), 1), strings.Replace(string(base), "registry+https://github.com/rust-lang/crates.io-index", "git+unbound", 1), "version = 4\n"} {
		if cargoLockKeepsOriginalPins(base, []byte(changed)) {
			t.Fatal("unbound derived lock accepted")
		}
	}
}

func TestSnapshotCloseReportsRestoreFailureAndContinuesOtherCleanup(t *testing.T) {
	root := physicalSoftwareFixture(t)
	build := softwareBuildPlan{Target: root}
	raw := []byte("fn main(){}\n")
	proof := askBuildEvidence{Inputs: []buildSourceRecord{{Path: "cc-ask/src/main.rs", SHA: digest(raw)}}}
	snapshot, e := materializeMP4BuildSnapshot(build, proof, []updateObject{&fakeUpdateObject{raw: raw}})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	e = snapshot.closeWithRestore(func(a snapshotDirectoryACL) error {
		calls++
		flags := uint32(4 | 0x20000000)
		if a.protected {
			flags = 4 | 0x80000000
		}
		updateSetSecurity.Call(uintptr(a.handle), 1, uintptr(flags), 0, 0, a.dacl, 0)
		return errors.New("injected_restore_observation_failure")
	})
	if e == nil || calls == 0 || len(snapshot.files) != 0 || len(snapshot.dirs) != 0 || len(snapshot.sealed) != 0 {
		t.Fatal("restore failure swallowed or cleanup abandoned")
	}
	if e := os.WriteFile(filepath.Join(snapshot.Root, "released.txt"), []byte("released"), 0600); e != nil {
		t.Fatal("actual test cleanup not restored", e)
	}
}

func TestSnapshotConstructionFailureIncludesPrimaryAndCleanupFailure(t *testing.T) {
	root := physicalSoftwareFixture(t)
	raw := []byte("package main\nfunc main(){}\n")
	proof := askBuildEvidence{Inputs: []buildSourceRecord{{Path: "unattended-runner/main.go", SHA: digest(raw)}}}
	primary := errors.New("injected_partial_seal_failure")
	restore := errors.New("injected_restore_observation_failure")
	closed := 0
	result, e := materializeMP4BuildSnapshotWith(context.Background(), softwareBuildPlan{Target: root, GoRunner: true}, proof, []updateObject{&fakeUpdateObject{raw: raw}}, func(s *softwareBuildSnapshot) error {
		if e := s.sealDirectories(); e != nil {
			return e
		}
		return primary
	}, func(s *softwareBuildSnapshot) error {
		closed++
		return errors.Join(s.Close(), restore)
	})
	if result != nil || !errors.Is(e, primary) || !errors.Is(e, restore) || !errors.Is(e, errSnapshotCleanup) || closed != 1 {
		t.Fatal("construction or cleanup failure discarded", e, closed)
	}
	if e := os.WriteFile(filepath.Join(root, "source", "restored.txt"), []byte("restored"), 0600); e != nil {
		t.Fatal("owned cleanup was not performed", e)
	}
}

func TestSnapshotRejectsUnlistedInputInsertedBeforeFinalSeal(t *testing.T) {
	root := physicalSoftwareFixture(t)
	raw := []byte("fn main(){}\n")
	proof := askBuildEvidence{Inputs: []buildSourceRecord{{Path: "cc-ask/src/main.rs", SHA: digest(raw)}}}
	result, e := materializeMP4BuildSnapshotWith(context.Background(), softwareBuildPlan{Target: root}, proof, []updateObject{&fakeUpdateObject{raw: raw}}, func(s *softwareBuildSnapshot) error {
		if e := os.WriteFile(filepath.Join(s.Root, "cc-ask", "build.rs"), []byte("fn main(){}"), 0600); e != nil {
			return e
		}
		return s.sealDirectories()
	}, func(s *softwareBuildSnapshot) error { return s.Close() })
	if result != nil || !errors.Is(e, errUpdateObject) {
		t.Fatal("unlisted compiler input survived final sealed namespace check", e)
	}
}

func TestCargoExactChildEnvironmentDoesNotChangeOrdinaryChildren(t *testing.T) {
	parent := []string{"PATH=owned-toolchain", "CARGO_PROFILE_RELEASE_DEBUG_ASSERTIONS=true", "CC_PRODUCT_MCP_CONTRACT_REPOSITORY_ID=707001"}
	ordinary := askChildEnvironmentValues(context.Background(), parent, "ASK_SOFTWARE_PARENT_HANDLE=7")
	if len(ordinary) != 4 {
		t.Fatal("ordinary child inheritance changed")
	}
	ctx := context.WithValue(context.Background(), askChildExactEnvironment{}, true)
	ctx = context.WithValue(ctx, askChildEnvironment{}, map[string]string{"PATH": "owned-toolchain", "CARGO_HOME": "owned-home"})
	exact := strings.Join(askChildEnvironmentValues(ctx, parent, "ASK_SOFTWARE_PARENT_HANDLE=7"), "\n")
	if strings.Contains(exact, "DEBUG_ASSERTIONS") || strings.Contains(exact, "CC_PRODUCT") || !strings.Contains(exact, "ASK_SOFTWARE_PARENT_HANDLE=7") || !strings.Contains(exact, "CARGO_HOME=owned-home") {
		t.Fatal("unproved child input inherited or explicit required input omitted")
	}
}

func TestCargoNeutralContextHasOwnedConfigAndCacheLinkOnly(t *testing.T) {
	if _, e := os.Stat(mp4PublicCargoRegistry); e != nil {
		t.Skip("explicit workstation public registry integration")
	}
	root := physicalSoftwareFixture(t)
	ctx, config, e := prepareMP4CargoContext(context.Background(), softwareBuildPlan{Target: root})
	if e != nil {
		t.Fatal(e)
	}
	settings := ctx.Value(askChildEnvironment{}).(map[string]string)
	if !sameCleanPath(settings["CARGO_HOME"], config.Root) || settings["RUSTFLAGS"] != "" || settings["RUSTC_WRAPPER"] != "D:/CC/Tools/sccache/sccache.exe" {
		t.Fatal("unbound cargo environment")
	}
	link := config.cacheLink
	if e := config.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Lstat(link); !os.IsNotExist(e) {
		t.Fatal("owned publiccache link remained")
	}
	if _, e := os.Stat(mp4PublicCargoRegistry); e != nil {
		t.Fatal("public cache changed")
	}
}

func TestCargoOwnedHomeRejectsLegacyConfigurationUntilRelease(t *testing.T) {
	if _, e := os.Stat(mp4PublicCargoRegistry); e != nil {
		t.Skip("explicit workstation public registry integration")
	}
	root := physicalSoftwareFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, config, e := prepareMP4CargoContext(ctx, softwareBuildPlan{Target: root})
	if e != nil {
		t.Fatal(e)
	}
	defer config.Close()
	legacy := filepath.Join(config.Root, "config")
	if e := os.WriteFile(legacy, []byte("[env]\nSNAPSHOT_HOME_PROBE=\"unbound\"\n"), 0600); e == nil {
		t.Fatal("unbound legacy Cargo configuration admitted while the owned home is active")
	}
	if _, e := os.Stat(config.cacheLink); e != nil {
		t.Fatal("ordinary public cache lookup blocked", e)
	}
	project := filepath.Join(root, "project")
	if os.Mkdir(project, 0700) != nil || os.Mkdir(filepath.Join(project, "src"), 0700) != nil {
		t.Fatal("owned Cargo source directories")
	}
	if os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname=\"owned-sealed-home\"\nversion=\"0.1.0\"\nedition=\"2021\"\n[workspace]\n"), 0600) != nil || os.WriteFile(filepath.Join(project, "src", "main.rs"), []byte("fn main(){assert!(option_env!(\"SNAPSHOT_HOME_PROBE\").is_none());}\n"), 0600) != nil {
		t.Fatal("owned Cargo source")
	}
	output := filepath.Join(root, "target")
	built := askOwnedChildAt(ctx, ctx.Value(mp4CargoDirectory{}).(string), snapshotTestTool(t, "cargo"), "build", "--offline", "--manifest-path", filepath.Join(project, "Cargo.toml"), "--target-dir", output)
	defer clear(built.Raw)
	if built.Exit != 0 || !built.Reaped || built.TimedOut {
		t.Fatal("sealed Cargo home ordinary build failed", built.Exit)
	}
	ran := askOwnedChild(ctx, filepath.Join(output, "debug", "owned-sealed-home.exe"))
	defer clear(ran.Raw)
	if ran.Exit != 0 || !ran.Reaped || ran.TimedOut {
		t.Fatal("legacy Cargo configuration influenced the controlled child", ran.Exit)
	}
	if !config.cargoConfigurationUnchanged() {
		t.Fatal("held Cargo configuration changed during compilation")
	}
	if e := config.Close(); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(legacy, []byte("released"), 0600); e != nil {
		t.Fatal("original owned namespace ACL not restored", e)
	}
	if config.cargoConfigurationUnchanged() {
		t.Fatal("released configuration accepted before acknowledgement")
	}
}

func TestCargoControlledContextIgnoresUnboundAncestorFlagsAndEnv(t *testing.T) {
	if _, e := os.Stat(mp4PublicCargoRegistry); e != nil {
		t.Skip("explicit workstation Cargo integration")
	}
	root := physicalSoftwareFixture(t)
	project := filepath.Join(root, "source")
	if os.Mkdir(project, 0700) != nil {
		t.Fatal("ownedsource")
	}
	if os.Mkdir(filepath.Join(project, "src"), 0700) != nil || os.Mkdir(filepath.Join(root, ".cargo"), 0700) != nil {
		t.Fatal("owneddirs")
	}
	if os.WriteFile(filepath.Join(root, ".cargo", "config.toml"), []byte("[env]\nSNAPSHOT_CONFIG_PROBE=\"unbound\"\n[build]\nrustflags=[\"--cfg\",\"ancestor_fixture\"]\n"), 0600) != nil || os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname=\"owned-cargo-config\"\nversion=\"0.1.0\"\nedition=\"2021\"\n[workspace]\n"), 0600) != nil || os.WriteFile(filepath.Join(project, "src", "main.rs"), []byte("fn main(){assert!(!cfg!(ancestor_fixture));assert!(option_env!(\"SNAPSHOT_CONFIG_PROBE\").is_none());}\n"), 0600) != nil {
		t.Fatal("owned config fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, config, e := prepareMP4CargoContext(ctx, softwareBuildPlan{Target: root})
	if e != nil {
		t.Fatal(e)
	}
	defer config.Close()
	result := askOwnedChildAt(ctx, ctx.Value(mp4CargoDirectory{}).(string), snapshotTestTool(t, "cargo"), "build", "--offline", "--manifest-path", filepath.Join(project, "Cargo.toml"), "--target-dir", filepath.Join(root, "target"))
	defer clear(result.Raw)
	if result.Exit != 0 || !result.Reaped {
		t.Fatal("controlled Cargo build failed", result.Exit)
	}
	ran := askOwnedChild(ctx, filepath.Join(root, "target", "debug", "owned-cargo-config.exe"))
	defer clear(ran.Raw)
	if ran.Exit != 0 || !ran.Reaped {
		t.Fatal("unboundancestorconfiguration consumed", ran.Exit)
	}
}

func TestCargoPurposeExcludesInheritedReleaseLinkerAndProductInputs(t *testing.T) {
	if _, e := os.Stat(mp4PublicCargoRegistry); e != nil {
		t.Skip("explicit workstation Cargo integration")
	}
	t.Setenv("CARGO_PROFILE_RELEASE_DEBUG_ASSERTIONS", "true")
	t.Setenv("CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_LINKER", "unqualified-linker.exe")
	t.Setenv("CC_PRODUCT_MCP_CONTRACT_REPOSITORY_ID", "707001")
	t.Setenv("CC_PRODUCT_MCP_CONTRACT_OWNER_ID", "707002")
	root := physicalSoftwareFixture(t)
	project := filepath.Join(root, "project")
	if os.Mkdir(project, 0700) != nil || os.Mkdir(filepath.Join(project, "src"), 0700) != nil {
		t.Fatal("owned Cargo directories")
	}
	if os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte("[package]\nname=\"owned-cargo-env\"\nversion=\"0.1.0\"\nedition=\"2021\"\n[workspace]\n"), 0600) != nil || os.WriteFile(filepath.Join(project, "src", "main.rs"), []byte("fn main(){assert!(!cfg!(debug_assertions));assert!(option_env!(\"CC_PRODUCT_MCP_CONTRACT_REPOSITORY_ID\").is_none());assert!(option_env!(\"CC_PRODUCT_MCP_CONTRACT_OWNER_ID\").is_none());}\n"), 0600) != nil {
		t.Fatal("owned Cargo source")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, config, e := prepareMP4CargoContext(ctx, softwareBuildPlan{Target: root})
	if e != nil {
		t.Fatal(e)
	}
	defer config.Close()
	output := filepath.Join(root, "target")
	built := askOwnedChildAt(ctx, ctx.Value(mp4CargoDirectory{}).(string), snapshotTestTool(t, "cargo"), "build", "--offline", "--release", "--manifest-path", filepath.Join(project, "Cargo.toml"), "--target-dir", output)
	defer clear(built.Raw)
	if built.Exit != 0 || !built.Reaped || built.TimedOut {
		t.Fatal("inherited unqualified linker reached Cargo", built.Exit)
	}
	ran := askOwnedChild(ctx, filepath.Join(output, "release", "owned-cargo-env.exe"))
	defer clear(ran.Raw)
	if ran.Exit != 0 || !ran.Reaped || ran.TimedOut {
		t.Fatal("inherited release or product-binding input reached compiler", ran.Exit)
	}
}

func TestMP4FinalCleanupMustMatchReadyArtifactBeforeReceipt(t *testing.T) {
	id := softwareIdentity{"1", "2", "3"}
	ok := true
	ready := askArtifactResult{Domain: "owner-question-cargo-output-v1", Source: mp4Source, SHA: strings.Repeat("a", 64), Bytes: 16, OutputIdentity: &id, SnapshotManifestSHA: strings.Repeat("b", 64), ResolvedLockSHA: strings.Repeat("c", 64), ConfigurationSHA: strings.Repeat("d", 64)}
	for _, fault := range []string{"normal", "cleanupfalse", "cleanupmissing", "identity", "source", "bytes", "snapshot", "lock", "config", "reason"} {
		t.Run(fault, func(t *testing.T) {
			final := ready
			final.Reason = "producer_acknowledged"
			final.SnapshotCleanup = &ok
			switch fault {
			case "cleanupfalse":
				no := false
				final.SnapshotCleanup = &no
			case "cleanupmissing":
				final.SnapshotCleanup = nil
			case "identity":
				final.OutputIdentity = &softwareIdentity{"1", "other", "3"}
			case "source":
				final.Source = "other"
			case "bytes":
				final.Bytes++
			case "snapshot":
				final.SnapshotManifestSHA = "other"
			case "lock":
				final.ResolvedLockSHA = "other"
			case "config":
				final.ConfigurationSHA = "other"
			case "reason":
				final.Reason = "source_snapshot_cleanup_failed_preserved"
			}
			raw, _ := json.Marshal(final)
			if softwareBuildCompletionMatches(raw, ready) != (fault == "normal") {
				t.Fatal("completion mismatch accepted", fault)
			}
		})
	}
}
