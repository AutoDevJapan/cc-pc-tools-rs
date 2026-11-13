//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
)

const mp4CargoNeutralCWD = "D:/CC/Tools/codex-jobs"
const mp4CargoConfiguration = "[build]\nrustc = \"C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe\"\nrustc-wrapper = \"D:/CC/Tools/sccache/sccache.exe\"\nrustc-workspace-wrapper = \"\"\nrustflags = []\n"
const mp4PublicCargoRegistry = "C:/Users/makhmeto/.cargo/registry"

func cargoNeutralConfigurationAbsent(cwd string) bool {
	for parent := filepath.Clean(cwd); ; parent = filepath.Dir(parent) {
		for _, leaf := range []string{"config", "config.toml"} {
			_, e := os.Lstat(filepath.Join(parent, ".cargo", leaf))
			if !os.IsNotExist(e) {
				return false
			}
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	return true
}

// Only the normal public package registry cache is shared. No existing Cargo
// config, bin directory or credentials are read or copied. Cargo config
// discovery starts at the separately admitted neutral software cwd.
func prepareMP4CargoContext(ctx context.Context, build softwareBuildPlan) (context.Context, *softwareBuildSnapshot, error) {
	if !cargoNeutralConfigurationAbsent(mp4CargoNeutralCWD) {
		return ctx, nil, errUpdateObject
	}
	home := filepath.Join(build.Target, "cargo-home")
	if os.Mkdir(home, 0700) != nil {
		return ctx, nil, errUpdateObject
	}
	registry := filepath.Join(home, "registry")
	if e := os.Symlink(mp4PublicCargoRegistry, registry); e != nil {
		return ctx, nil, e
	}
	config := &softwareBuildSnapshot{Root: home, paths: map[string]bool{}}
	config.cacheLink = registry
	if e := snapshotDirectories(home, home, config); e != nil {
		config.Close()
		return ctx, nil, e
	}
	if e := config.create(filepath.Join(home, "config.toml"), []byte(mp4CargoConfiguration)); e != nil {
		config.Close()
		return ctx, nil, e
	}
	settings := map[string]string{"CARGO_HOME": home, "CARGO_NET_OFFLINE": "true", "RUSTC": "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe", "RUSTC_WRAPPER": "D:/CC/Tools/sccache/sccache.exe", "RUSTC_WORKSPACE_WRAPPER": "", "CARGO_ENCODED_RUSTFLAGS": "", "RUSTFLAGS": ""}
	return context.WithValue(ctx, askChildEnvironment{}, settings), config, nil
}
