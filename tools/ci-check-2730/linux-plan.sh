#!/bin/sh
set -eu
mkdir -p /work/evidence /work/home /work/rustup /work/cache
export CARGO_HOME=/work/cargo RUSTUP_HOME=/work/rustup RUSTUP_TOOLCHAIN=1.95.0
export PATH=/work/cargo/bin:/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
export SCCACHE_DIR=/work/cache SCCACHE_CACHE_SIZE=30G
check() {
  name="$1"; shift
  set +e
  "$@" > "/work/evidence/$name.log" 2>&1
  code=$?
  set -e
  bytes=$(wc -c < "/work/evidence/$name.log")
  hash=$(sha256sum "/work/evidence/$name.log" | cut -d' ' -f1)
  printf '%s exit=%s bytes=%s sha256=%s\n' "$name" "$code" "$bytes" "$hash" >> /work/evidence/checks.txt
  test "$code" -eq 0
}
check sdk rustup toolchain install 1.95.0 --no-self-update --profile minimal --component clippy --component rustfmt
export RUSTC_WRAPPER=/work/sccache
check sdk-version rustc -vV
check sdk-cargo cargo -V
check head git rev-parse HEAD 'HEAD^{tree}'
check regression cargo nextest run --locked --manifest-path tools/Cargo.toml -p civ6-autotest-rs --test driver_safety -E 'test(test_run_creates_missing_output_parents_but_rejects_existing_run)'
check driver-safety cargo nextest run --locked --manifest-path tools/Cargo.toml -p civ6-autotest-rs --test driver_safety
check changed-crate cargo nextest run --locked --manifest-path tools/Cargo.toml -p civ6-autotest-rs --all-targets
check workspace cargo nextest run --locked --manifest-path tools/Cargo.toml --workspace --all-targets --no-fail-fast
check doctest cargo test --locked --manifest-path tools/Cargo.toml --workspace --doc
check clippy cargo clippy --locked --manifest-path tools/Cargo.toml --workspace --all-targets -- -D warnings
check fmt cargo fmt --manifest-path tools/Cargo.toml --all -- --check
check sccache /work/sccache --show-stats
