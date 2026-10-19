# ToDo 3029 build receipt

Owner: shared ToDo 3029 (`cc-pc-tools-rs`), job `20261020-033638-todo-3029`.
Retain the artifacts under `tools/codex-progress-tui/.ci-work/todo3029/`; this
directory was created by this job and contains the pinned source checkout,
dedicated Cargo target, official npm tarball, extracted metadata, and output
binaries. Do not remove it while ToDo 3021 may need the binaries.

## Source and build

- Fixed source commit in this repository: `7c1fe857547eb7e6850361752a26d9dfe868528e`.
- Upstream: `https://github.com/openai/codex`, tag `rust-v0.163.0`, commit
  `83086188ae0614bd882818ff1aa23f3194962012`, annotated tag object
  `129b8ec7ee6f4511557a4856279f529025c25af4`.
- Applied patch SHA256:
  `21dd906d4ceace003835e8b0a59517e9a35187df7c702ddda75e4a9c1b3f96b7`.
- Patched source stamp supplied as `STABLE_GIT_COMMIT`:
  `e5578209650641348c778829c8a0b83eb6753dc1`.
- Toolchain: rustc/cargo `1.95.0`, host `x86_64-pc-windows-msvc`.
- Dedicated target: `.ci-work/todo3029/source/codex-rs/target`.
- Built with `D:/CC/Tools/codex-jobs/bin/cargo.cmd build --locked --release
  -p codex-cli --bin codex`, using release profile `lto=false`, `debug=0`,
  `strip=symbols`, `codegen-units=16`, and `RUST_MIN_STACK=8388608`.
- `codex.exe --version` reports `codex-cli 0.163.0`.

## Retained artifacts and checksums

All paths below are relative to `tools/codex-progress-tui/`.

- Patched Windows CLI: `.ci-work/todo3029/delivery/codex.exe`, SHA256
  `618bf6dcd243463d9058f0ce68b9e43ba2a2b2ac75c715f87ad40d06e453433f`.
- `source.json` expected tested CLI SHA256:
  `bcebff4cf935c2bd728bbed130c3c21a329ac58727677326d6a1556124b46969`.
  **Mismatch.** The binary is version 0.163.0 and was built from the pinned tag
  plus the recorded patch and commit stamp, but this build did not reproduce
  the previously tested byte hash. The first attempt omitted the commit stamp;
  the final build supplied it, but the remaining byte-level cause is not
  proven. `llvm-readobj` reports different PE COFF `TimeDateStamp` values for
  the two local builds (`2026-10-19 19:03:34` and `2026-10-19 19:09:26` UTC),
  so build-time variation can affect the byte hash; whether it fully explains
  the difference from the prior tested hash is unknown. No hash has been
  substituted or claimed as the tested hash.
- First, unstamped build retained separately as
  `.ci-work/todo3029/delivery/codex-build-unverified-ed0a81d0.exe` (SHA256
  `ed0a81d0c137105a0913bdc3b210e8e0f09684ac130743ba65652aa319886cd8`). It is
  not the candidate patched binary.
- Official source package: `.ci-work/todo3029/delivery/openai-codex-0.163.0-win32-x64.tgz`;
  npm integrity `sha512-QQGQ/q9dCJ5C5jqhgFrwycpWG3szIyFNThMiKl6hYtu6N/eM3Qd7StesgnCFAfzVED8IkE3N3oH71szzcludqw==`,
  tarball SHA512
  `410190feaf5d089e42e63aa1805af0c9ca561b7b3323214d4e13222a5ea162dbba37f78cdd077b4ad7ac82708501fcd5103f08904dcdde81fbd6ccf3725b9dab`.
- Official npm `codex.exe` SHA256:
  `2692c8c7bb2cf8b0aefe638741fc90236f4b824c59578ce84994f9de58200993` —
  matches `source.json`.
- Official `codex-code-mode-host.exe` retained at
  `.ci-work/todo3029/delivery/codex-code-mode-host.exe`, SHA256
  `07f3869f0849cd54b31cec1f86ec796f004501bd3a215135c1facc883a2b9988` —
  matches `source.json`.

The remaining issue is the patched CLI byte-hash mismatch. Preserve the
recorded candidate and source for ToDo 3021's deployment and runtime acceptance;
do not rewrite `source.json` or the tested hash to hide the mismatch. No tests,
Clippy, CI, or review were repeated; prior success records from ToDo 3023/3028
remain the authority for those steps.
