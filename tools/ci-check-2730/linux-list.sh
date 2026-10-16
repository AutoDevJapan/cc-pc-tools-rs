#!/bin/sh
set -eu
export CARGO_HOME=/work/cargo RUSTUP_HOME=/work/rustup RUSTUP_TOOLCHAIN=1.95.0
export PATH=/work/cargo/bin:/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
export RUSTC_WRAPPER=/work/sccache SCCACHE_DIR=/work/cache SCCACHE_CACHE_SIZE=30G
cargo nextest list --locked --manifest-path tools/Cargo.toml --workspace --message-format json > /work/evidence/list-default.json 2>/work/evidence/list-default.stderr.log
cargo nextest list --locked --manifest-path tools/Cargo.toml --workspace --all-targets --message-format json > /work/evidence/list-all-targets.json 2>/work/evidence/list-all-targets.stderr.log
