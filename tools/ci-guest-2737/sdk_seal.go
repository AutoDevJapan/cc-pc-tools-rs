package main

import "errors"

// The fixed installer already selects Rust 1.97.1 x86_64-unknown-linux-gnu.
// These two component hashes were extracted from the official rustc archive,
// SHA256 9819d0a32d56bd339585319c80260e332779f5541fd66838ab7e016d6c814819,
// checked against its static.rust-lang.org .sha256 on 2026-10-19. They cover
// these artifacts only, not the rest of the OS/toolchain or product acceptance.
const sdkCompilerSHA = "d3a664c970a9fd8361b64194861bebc1ae37b9054e5ee3400dc1c9e691797eea"
const sdkDriverSHA = "b0726217c199be52842c1318a340f0fc68a2c2ca0e3fa4886191dbf6ee192a58"
const sdkDriverName = "librustc_driver-832cf6cfb1386559.so"
const sdkDriverBytes = 157802496
const sdkVersion = "rustc 1.97.1 (8bab26f4f 2026-07-14)"

func validatePublicSDKSeal(r sdkResult) error {
	if r.Failure != "" || !r.DiskSynced || r.ProductSource || r.RunnerRegistered || len(r.RustProbe) != 3 || len(r.RustDriver) != 1 {
		return errors.New("public SDK persistence/runtime receipt rejected")
	}
	for i, name := range []string{"compiler", "verbose", "cachekey"} {
		p := r.RustProbe[i]
		if p.Name != name || p.Exit != 0 || p.Signal != 0 || p.TimedOut || p.FailureCode != "" {
			return errors.New("public SDK Rust runtime rejected")
		}
	}
	if r.RustProbe[0].CompilerSHA256 != sdkCompilerSHA || r.RustProbe[1].Version != sdkVersion || r.RustProbe[1].CommitHash != "8bab26f4f68e0e26f0bb7960be334d5b520ea452" || r.RustProbe[1].CommitDate != "2026-07-14" || r.RustProbe[2].CacheKey != "202607148bab" {
		return errors.New("public SDK Rust component rejected")
	}
	d := r.RustDriver[0]
	if d.Name != sdkDriverName || d.Bytes != sdkDriverBytes || d.SHA256 != sdkDriverSHA || !d.ReadComplete {
		return errors.New("public SDK Rust driver rejected")
	}
	return nil
}
