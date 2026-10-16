#!/bin/sh
set -eu
export CARGO_HOME=/work/cargo RUSTUP_HOME=/work/rustup RUSTUP_TOOLCHAIN=1.95.0
export PATH=/work/cargo/bin:/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
export RUSTC_WRAPPER=/work/sccache SCCACHE_DIR=/work/cache SCCACHE_CACHE_SIZE=30G
# The captured default/all-targets inventory identifies exactly this one extra
# example binary (15 tests). Do not repeat the 1835 completed workspace tests.
set +e
cargo nextest run --locked --manifest-path tools/Cargo.toml -p civ6-autotest-rs --example mod_selection_rollback > /work/evidence/extra-example.log 2>&1
code=$?
set -e
bytes=$(wc -c < /work/evidence/extra-example.log)
hash=$(sha256sum /work/evidence/extra-example.log | cut -d' ' -f1)
printf 'extra-example exit=%s bytes=%s sha256=%s\n' "$code" "$bytes" "$hash" >> /work/evidence/checks.txt
exit "$code"
