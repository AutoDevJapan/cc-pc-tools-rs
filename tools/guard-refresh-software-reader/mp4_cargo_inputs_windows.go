//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const mp4CargoConfiguration = "[build]\nrustc = \"C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe\"\nrustc-wrapper = \"D:/CC/Tools/sccache/sccache.exe\"\nrustc-workspace-wrapper = \"\"\nrustflags = []\n"
const mp4PublicCargoRegistry = "C:/Users/makhmeto/.cargo/registry"

var errSnapshotCleanup = errors.New("compiler_snapshot_cleanup_failed_preserved")

func mp4CompilerEnvironment(home string) map[string]string {
	settings := map[string]string{"CARGO_HOME": home, "CARGO_NET_OFFLINE": "true", "RUSTC": "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe", "RUSTC_WRAPPER": "D:/CC/Tools/sccache/sccache.exe", "RUSTC_WORKSPACE_WRAPPER": "", "CARGO_ENCODED_RUSTFLAGS": "", "RUSTFLAGS": "", "CODEX_JOBS_REAL_CARGO": "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe", "SCCACHE_DIR": "D:/CC/BuildCache/sccache", "SCCACHE_CACHE_SIZE": "30G", "SCCACHE_SERVER_PORT": "42259"}
	// Preserve the existing Windows/MSVC toolchain search inputs deliberately;
	// Cargo/Rust profile, linker overrides, credentials and product-binding
	// variables are not inherited. These selected values join the config digest.
	for _, key := range []string{"SystemRoot", "WINDIR", "COMSPEC", "SystemDrive", "PATH", "PATHEXT", "TEMP", "TMP", "LOCALAPPDATA", "APPDATA", "USERPROFILE", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "INCLUDE", "LIB", "LIBPATH", "VSINSTALLDIR", "VCINSTALLDIR", "VCToolsInstallDir", "WindowsSdkDir", "WindowsSDKVersion", "UniversalCRTSdkDir", "UCRTVersion"} {
		if value, ok := os.LookupEnv(key); ok {
			settings[key] = value
		}
	}
	return settings
}

func mp4ConfigurationDigest(ctx context.Context) string {
	settings, _ := ctx.Value(askChildEnvironment{}).(map[string]string)
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	raw := []byte(mp4CargoConfiguration)
	defer clear(raw)
	for _, key := range keys {
		raw = append(raw, key...)
		raw = append(raw, 0)
		raw = append(raw, settings[key]...)
		raw = append(raw, 0)
	}
	return digest(raw)
}

func (s *softwareBuildSnapshot) cargoConfigurationUnchanged() bool {
	if s == nil || len(s.files) != 1 || !s.cargoDirectoryUnchanged() {
		return false
	}
	entries, e := os.ReadDir(s.Root)
	want := 3
	if s.borrowedCargoDrive {
		want = 2
	}
	if s.cacheLink == "" {
		want--
	}
	if e != nil || len(entries) != want {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != "config.toml" && entry.Name() != "registry" && entry.Name() != "cwd" {
			return false
		}
	}
	if s.cacheLink != "" {
		link, e := os.Readlink(s.cacheLink)
		if e != nil || !sameCleanPath(link, mp4PublicCargoRegistry) {
			return false
		}
	}
	f := s.files[0]
	held, e1 := f.Stat()
	named, e2 := os.Stat(filepath.Join(s.Root, "config.toml"))
	if e1 != nil || e2 != nil || !os.SameFile(held, named) {
		return false
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return false
	}
	raw, e := io.ReadAll(io.LimitReader(f, int64(len(mp4CargoConfiguration)+1)))
	defer clear(raw)
	return e == nil && string(raw) == mp4CargoConfiguration
}

// Only the normal public package registry cache is shared. No existing Cargo
// config, bin directory or credentials are read or copied. Cargo config
// discovery starts at the new sealed operation-owned drive root.
func prepareMP4CargoContext(ctx context.Context, build softwareBuildPlan) (result context.Context, snapshot *softwareBuildSnapshot, err error) {
	home := filepath.Join(build.Target, "cargo-home")
	if os.Mkdir(home, 0700) != nil {
		return ctx, nil, errUpdateObject
	}
	registry := filepath.Join(home, "registry")
	sharedCache := build.Source != mp4Source
	if sharedCache {
		if e := os.Symlink(mp4PublicCargoRegistry, registry); e != nil {
			return ctx, nil, e
		}
	}
	config := &softwareBuildSnapshot{Root: home, paths: map[string]bool{}}
	if sharedCache {
		config.cacheLink = registry
	}
	ok := false
	defer func() {
		if !ok {
			if cleanup := config.Close(); cleanup != nil {
				err = errors.Join(err, errSnapshotCleanup, cleanup)
			}
		}
	}()
	if e := snapshotDirectories(home, home, config); e != nil {
		return ctx, nil, e
	}
	if e := config.create(filepath.Join(home, "config.toml"), []byte(mp4CargoConfiguration)); e != nil {
		return ctx, nil, e
	}
	if parent := borrowedCargoDirectory(ctx); parent != nil {
		if !parent.cargoDirectoryUnchanged() {
			return ctx, nil, errUpdateObject
		}
		config.cargoDrive, config.cargoDriveTarget, config.borrowedCargoDrive = parent.cargoDrive, parent.cargoDriveTarget, true
	} else if e := config.bindCargoDirectory(); e != nil {
		return ctx, nil, e
	}
	if e := config.sealDirectories(); e != nil {
		return ctx, nil, e
	}
	if !config.cargoConfigurationUnchanged() {
		return ctx, nil, errUpdateObject
	}
	ctx = context.WithValue(ctx, askChildEnvironment{}, mp4CompilerEnvironment(home))
	ctx = context.WithValue(ctx, mp4CargoDirectory{}, config.cargoDrive+"\\")
	ok = true
	return context.WithValue(ctx, askChildExactEnvironment{}, true), config, nil
}
