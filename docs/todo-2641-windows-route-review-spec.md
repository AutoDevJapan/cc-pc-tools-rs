# GoDD MCP αのWindows実行先契約の追加差分を確認する

元2641の有限read-only別モデルレビューです。実装担当はgpt-6.1-solで、別モデルgpt-6-lunaを指定します。Rustテスト差分と実CI workflowの2ファイルを照合するため、単一ファイルレビューではありません。新しい実装を担当せず、供給本文を読んで最終応答だけを返してください。保存・ToDo更新・CI起動・シェル/通信・コード変更は不要です。原応答は実装側がjobのlast_message.mdから保存します。

reviewed_base: 085b5b007cab4ebd57d6690faf6df95bb40453ef
reviewed_head: 0fda87c591ae5e6fd256508d94376c38e16b2f4e
reviewed_tree: 0bc5ff599c5105474f88c6284e984b55583c5510
原repo1154066087、owner AutoDevJapan/private/admin/nonfork、PR3613/open/draft/同head+base repo/原refは親がAPI照合済み。元の同refへforce=falseで正常更新しました。変更は次のRust source 1fileだけです。原ci.yml blob64636a181386f3212d0190be11ffe9afa9903644は不変です。

## 仕様と問題
原run39147155349 attempt4のRust8失敗は、原logと固定blob73904294f03d8ee9bf901570649370c64cb03b23のRAM照合で、全てpreserves_jobのbaseline失敗と元2737が確認しました。同判定はwindows-native-checkのruns-on literal windows-2025だけを許す一方、原workflowは既に手動実行/原branch/明示lease値/専用Windows job+run_idで束縛した式へ変わっています。SDK失敗ではなくsource契約不整合です。
仕様は、元Windows hosted literalと、このレビュー済みの厳密な現在の式だけを許し、元13Cargo工程・SDK/toolchain・profile・2ビルド並列・4テスト並列・外部資源直列・20分・失敗伝播・cache/条件を維持することです。式を実workflowから生成して期待値と同一にするtautologyや、contains/self-hosted/任意式による緩和をしません。

## 実確認と未確認
親はrustfmt edition2021 check成功。実candidateから追加const/helper/回帰2testをそのまま抽出したstd-only Rust testに、固定workflowから取得したWindows scalarの正例1件を加えてrustc --test -D warningsで確認しました。旧literal述語ではexit101（2failed/1passed）、新述語はexit0/3passedです。これは全serde_yaml integration検査ではなく、元のRust8件/Windows13工程/公式CIの再実行成功とはしません。cargo資源がない親は重いCargoを実行せず、原2737へ新候補を配送して関連検査/正規CIを続けます。終了時source_metadataの根因は別2737/2922工程の未確定として保持し、推測で合格にしません。

## 追加差分（全1file）
```diff
@@ -3,6 +3,15 @@ use serde_yaml::Value;

 const CI: &str = include_str!("../../../../.github/workflows/ci.yml");
 const PROFILE_SOURCE: &str = include_str!("../../../../.github/scripts/windows-ci-profile.rs");
+// Only the reviewed, branch-scoped manual route may replace the hosted Windows runner.
+// Keep this independent of CI: deriving the expected value from CI would admit drift.
+const PINNED_WINDOWS_RUNNER_ROUTE: &str = concat!(
+    "${{ vars.CC_PRODUCT_2737_CI == 'todo2737-0b2414b10418d265c7823a4f87bb5881' ",
+    "&& github.event_name == 'workflow_dispatch' ",
+    "&& github.ref == 'refs/heads/codex/todo2641-http-response-pseudo-20261015' ",
+    "&& format('cc-product-2737-1154066087-windows-windows-native-check-{0}', github.run_id) ",
+    "|| 'windows-2025' }}",
+);
 const WINDOWS_IF: &str = "needs.verify.outputs.windows != 'false'";
 const JOB_IF: &str = "needs.verify.outputs.expensive != 'false' && (needs.verify.outputs.windows != 'false' || needs.verify.outputs.windows_ts != 'false') && needs.verify.outputs.tree_reuse != 'true'";
 const KOFFI_IF: &str = "needs.verify.outputs.windows_ts != 'false'";
@@ -210,6 +219,50 @@ fn native_test_threads_have_one_owner(value: &Value) -> bool {
     count(value) == Some(1)
 }

+fn preserves_windows_runner_route(route: Option<&str>) -> bool {
+    matches!(
+        route,
+        Some("windows-2025") | Some(PINNED_WINDOWS_RUNNER_ROUTE)
+    )
+}
+
+#[test]
+fn windows_runner_route_accepts_hosted_and_only_the_reviewed_manual_route() {
+    for route in ["windows-2025", PINNED_WINDOWS_RUNNER_ROUTE] {
+        assert!(preserves_windows_runner_route(Some(route)));
+    }
+}
+
+#[test]
+fn windows_runner_route_rejects_wrong_os_unbound_and_weakened_routes() {
+    assert!(!preserves_windows_runner_route(None));
+    for route in ["", "ubuntu-latest", "windows-latest", "self-hosted"] {
+        assert!(!preserves_windows_runner_route(Some(route)));
+    }
+    for route in [
+        PINNED_WINDOWS_RUNNER_ROUTE.replace("workflow_dispatch", "pull_request"),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace("github.event_name == 'workflow_dispatch' && ", ""),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace(
+            "github.ref == 'refs/heads/codex/todo2641-http-response-pseudo-20261015' && ",
+            "",
+        ),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace("0b2414b10418d265c7823a4f87bb5881", "unbound"),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace("windows-windows-native-check", "linux-verify"),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace("github.run_id", "github.run_number"),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace(" || 'windows-2025'", ""),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace("'windows-2025'", "'ubuntu-latest'"),
+        PINNED_WINDOWS_RUNNER_ROUTE.replace(
+            "refs/heads/codex/todo2641-http-response-pseudo-20261015",
+            "refs/heads/main",
+        ),
+    ] {
+        assert_ne!(
+            route, PINNED_WINDOWS_RUNNER_ROUTE,
+            "mutation must change the route"
+        );
+        assert!(!preserves_windows_runner_route(Some(&route)));
+    }
+}
 fn preserves_job(document: &Value) -> bool {
     let job = windows_job(document);
     let Some(steps) = job["steps"].as_sequence() else {
@@ -240,7 +293,7 @@ fn preserves_job(document: &Value) -> bool {
         && job.as_mapping().is_some_and(|mapping| mapping.len() == 7)
         && job["name"].as_str() == Some("Windows native check")
         && job["needs"].as_str() == Some("verify")
-        && job["runs-on"].as_str() == Some("windows-2025")
+        && preserves_windows_runner_route(job["runs-on"].as_str())
         && job["timeout-minutes"].as_i64() == Some(20)
         && job["if"].as_str() == Some(JOB_IF)
         && steps
```

## 変更後の共通baseline（全体）
```rust
fn preserves_job(document: &Value) -> bool {
    let job = windows_job(document);
    let Some(steps) = job["steps"].as_sequence() else {
        return false;
    };
    let Some(jobs) = document["jobs"].as_mapping() else {
        return false;
    };
    let Some(koffi) = step(job, STEP_NAMES[23]) else {
        return false;
    };
    let Some(toolchain) = step(job, "Set up Rust toolchain") else {
        return false;
    };
    let Some(install) = step(job, STEP_NAMES[22]) else {
        return false;
    };
    jobs.len() == 4
        && document["defaults"].is_null()
        && [
            "verify",
            "windows-native-check",
            "pr-tree-receipt",
            "ci-gate",
        ]
        .iter()
        .all(|name| jobs.contains_key(Value::String((*name).into())))
        && job.as_mapping().is_some_and(|mapping| mapping.len() == 7)
        && job["name"].as_str() == Some("Windows native check")
        && job["needs"].as_str() == Some("verify")
        && preserves_windows_runner_route(job["runs-on"].as_str())
        && job["timeout-minutes"].as_i64() == Some(20)
        && job["if"].as_str() == Some(JOB_IF)
        && steps
            .iter()
            .map(|s| s["name"].as_str())
            .eq(STEP_NAMES.map(Some))
        && preserves_environment(job)
        && preserves_cargo_steps(job)
        && native_test_threads_have_one_owner(document)
        && preserves_dependency_cache(job)
        && document.get("cache-mode").is_none()
        && jobs.values().all(|job| job.get("cache-mode").is_none())
        && step(job, STEP_NAMES[5]).is_some_and(|s| s["id"].as_str() == Some("windows-ci-profile"))
        && [
            (STEP_NAMES[4], PROFILE_BUILD),
            (STEP_NAMES[5], PROFILE_INSTALL),
        ]
        .iter()
        .all(|(name, command)| {
            step(job, name).is_some_and(|s| {
                s.as_mapping()
                    .is_some_and(|m| m.len() == if *name == STEP_NAMES[5] { 5 } else { 4 })
                    && s["if"].as_str() == Some(WINDOWS_IF)
                    && s["working-directory"].as_str() == Some("${{ github.workspace }}")
                    && s["run"].as_str() == Some(*command)
            })
        })
        && flags_stay_in_windows_env(document, &mut Vec::new())
        && toolchain["if"].as_str() == Some(WINDOWS_IF)
        && toolchain["with"]["toolchain"].as_str() == Some("1.97.1")
        && toolchain["with"]["components"].as_str() == Some("clippy")
        && koffi["if"].as_str() == Some(KOFFI_IF)
        && koffi["run"].as_str() == Some("pnpm exec vitest run tests/os-file-lock.test.ts")
        && koffi["working-directory"].as_str() == Some("godd-mcp-alpha")
        && koffi.as_mapping().is_some_and(|mapping| mapping.len() == 4)
        && install["if"].as_str() == Some(KOFFI_IF)
        && install["run"].as_str() == Some("pnpm install --frozen-lockfile")
}
```

## 元workflowのWindows job（変更なし・参照対象）
```yaml
  windows-native-check:
    name: "Windows native check"
    needs: verify
    if: needs.verify.outputs.expensive != 'false' && (needs.verify.outputs.windows != 'false' || needs.verify.outputs.windows_ts != 'false') && needs.verify.outputs.tree_reuse != 'true'
    runs-on: ${{ vars.CC_PRODUCT_2737_CI == 'todo2737-0b2414b10418d265c7823a4f87bb5881' && github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/codex/todo2641-http-response-pseudo-20261015' && format('cc-product-2737-1154066087-windows-windows-native-check-{0}', github.run_id) || 'windows-2025' }}
    timeout-minutes: 20
    # One job-local output and matching profiles let the independent crates
    # reuse compatible dependency artifacts without dropping any checks.
    env:
      CARGO_TARGET_DIR: ${{ github.workspace }}/../godd-windows-target
      CARGO_BUILD_JOBS: "2"
      CARGO_PROFILE_DEV_DEBUG: "0"
      CARGO_PROFILE_TEST_DEBUG: "0"
      CARGO_INCREMENTAL: "0"
      RUSTFLAGS: "-C linker=rust-lld -C linker-flavor=lld-link -C link-arg=/threads:2"
      RUSTDOCFLAGS: "-C linker=rust-lld -C linker-flavor=lld-link -C link-arg=/threads:2"
    steps:
      - name: Checkout tested tree
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
        with:
          ref: ${{ inputs.release_source_sha || '' }}
          persist-credentials: false
      - name: Set up Node.js
        uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020
        with:
          node-version: ${{ env.NODE_VERSION }}
      - name: Verify published Rust toolchain and workflow contract
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: node godd-mcp-alpha/scripts/verify-rust-toolchain-availability.js --check-network
      - name: Set up Rust toolchain
        if: needs.verify.outputs.windows != 'false'
        uses: dtolnay/rust-toolchain@7e38f4b43b4db5c8dd498af069a4f6196df1d067
        with:
          toolchain: 1.97.1
          components: clippy
      # Optimize hash/signature/key derivation plus the syn 2 AST/token libraries.
      # Root/test fixtures, proc-macro crates and syn 3 keep their profiles.
      # The original nine commands keep
      # their arguments/order, with four installer/release-stage checks appended.
      - name: Build Windows CI dependency profile writer
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: rustc --edition=2021 .github/scripts/windows-ci-profile.rs -D warnings -C linker=rust-lld -C linker-flavor=lld-link -C link-arg=/threads:2 -o "${{ runner.temp }}/godd-windows-ci-profile.exe"
      - name: Install Windows CI dependency profile
        id: windows-ci-profile
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: "& '${{ runner.temp }}/godd-windows-ci-profile.exe'"
      # Each positive glob selects leaf files or individual dependency dirs,
      # never their parent directory (tar would otherwise include exclusions).
      # This is dependency data only; every check still executes on hit or miss.
      - name: Restore Windows Cargo dependencies
        id: windows-dependencies
        if: needs.verify.outputs.windows != 'false'
        continue-on-error: true
        uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9
        with:
          path: |
            ~/.cargo/registry/index
            ~/.cargo/registry/cache
            ~/.cargo/registry/src
            ${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*godd*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*.exe
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*.pdb
            ${{ steps.windows-ci-profile.outputs.cache_target }}/debug/.fingerprint/*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/.fingerprint/godd-*
            ${{ steps.windows-ci-profile.outputs.cache_target }}/debug/build/*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/build/godd-*
          key: windows-dependencies-v1-${{ runner.arch }}-rust-1.97.1-${{ hashFiles('godd-mcp-alpha/native/Cargo.toml', 'godd-mcp-alpha/native/Cargo.lock', '.github/scripts/windows-ci-profile.rs') }}
          # A profile-only edit must not discard every unchanged dependency.
          # Cargo still checks the current source/profile/features before reuse;
          # a prefix restore is not an exact hit or any gate admission.
          restore-keys: windows-dependencies-v1-${{ runner.arch }}-rust-1.97.1-
      # `godd-native-release.yml`のWindows matrix targetと同じTest stepを流用する。
      - name: Rust native lint (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: godd-mcp-alpha/native
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" clippy --locked --all-targets --all-features -- -D warnings
      - name: Rust native tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: godd-mcp-alpha/native
        # Same fixture feature set as the helper check; plain `test` retains doctests.
        env:
          CARGO_PROFILE_TEST_DEBUG: "0"
          # Bound native libtest scheduling independently of the two Cargo jobs.
          # Other workspaces retain their explicit serial external-resource tests.
          RUST_TEST_THREADS: "4"
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --locked --all-features
      # Save before the later workspaces: no post-cancel behaviour is assumed.
      # A failed cold attempt may seed a warm attempt, but is never a CI success.
      # GitHub ref scopes and the existing 10GB eviction limit stay unchanged.
      - name: Save completed Windows Cargo dependencies
        if: needs.verify.outputs.windows != 'false' && steps.windows-dependencies.outcome == 'success' && steps.windows-dependencies.outputs.cache-hit != 'true' && inputs.release_source_sha == '' && (github.event_name == 'push' || github.event_name == 'pull_request')
        continue-on-error: true
        uses: actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9
        with:
          path: |
            ~/.cargo/registry/index
            ~/.cargo/registry/cache
            ~/.cargo/registry/src
            ${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*godd*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*.exe
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/deps/*.pdb
            ${{ steps.windows-ci-profile.outputs.cache_target }}/debug/.fingerprint/*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/.fingerprint/godd-*
            ${{ steps.windows-ci-profile.outputs.cache_target }}/debug/build/*
            !${{ steps.windows-ci-profile.outputs.cache_target }}/debug/build/godd-*
          key: ${{ steps.windows-dependencies.outputs.cache-primary-key }}
      - name: Rust native credential helper tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: godd-mcp-alpha/native
        env:
          CARGO_PROFILE_TEST_DEBUG: "0"
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --locked --features test-fixtures --test credential_helper_contract
      - name: Rust common entry runtime lint (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" clippy --manifest-path godd-mcp-alpha/common-entry-runtime/Cargo.toml --locked --all-targets --jobs 1 -- -D warnings
      - name: Rust common entry runtime tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --manifest-path godd-mcp-alpha/common-entry-runtime/Cargo.toml --locked --jobs 1 -- --test-threads=1
      - name: Rust public entry lint (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" clippy --manifest-path godd-mcp-alpha/public-entry/Cargo.toml --locked --all-targets --jobs 1 -- -D warnings
      - name: Rust public entry tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --manifest-path godd-mcp-alpha/public-entry/Cargo.toml --locked --jobs 1
      - name: Rust public entry quality lint (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" clippy --manifest-path tools/public-entry-quality/Cargo.toml --locked --all-targets --jobs 1 -- -D warnings
      - name: Rust public entry quality tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --manifest-path tools/public-entry-quality/Cargo.toml --locked --jobs 1 -- --test-threads=1
      - name: Rust common entry installer lint (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" clippy --manifest-path tools/common-entry-installer/Cargo.toml --locked --all-targets --jobs 1 -- -D warnings
      - name: Rust common entry installer tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --manifest-path tools/common-entry-installer/Cargo.toml --locked --jobs 1 -- --test-threads=1
      - name: Rust release stage lint (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" clippy --manifest-path tools/release-stage/Cargo.toml --locked --all-targets --jobs 1 -- -D warnings
      - name: Rust release stage tests (Windows cfg)
        if: needs.verify.outputs.windows != 'false'
        working-directory: ${{ github.workspace }}
        run: cargo --config "${{ runner.temp }}/godd-windows-ci-profile.toml" test --manifest-path tools/release-stage/Cargo.toml --locked --jobs 1 -- --test-threads=1
      - name: Set up pnpm
        if: needs.verify.outputs.windows_ts != 'false'
        uses: pnpm/action-setup@ea17c68df8912ef543352723c149a84f56e3d413
      - name: Install dependencies (Windows koffi bindings)
        if: needs.verify.outputs.windows_ts != 'false'
        working-directory: godd-mcp-alpha
        run: pnpm install --frozen-lockfile
      - name: Windows koffi FFI binding tests (os-file-lock, #2646)
        if: needs.verify.outputs.windows_ts != 'false'
        working-directory: godd-mcp-alpha
        run: pnpm exec vitest run tests/os-file-lock.test.ts

  # receipt の発行可否（`receipt_eligible`）と、receipt に刻む gate 実行結果
  # （rust / docker / native）はすべて `verify` の単一判定を根拠にする。
  # `verify` からは「required step が成功したか」だけを受け取る（Issue #1641）。

```

## 返答
reviewed_base/head、complete_diff_read、実際に確認した追加差分と参照範囲、Must/Suggestion/Nitを日本語で返してください。固定式で信頼境界を保つか、異常系が式を実際に変えているか、元13工程・SDK・失敗伝播の意味が維持されるか、Rustの型/パターンに問題がないかを見てください。実行0なら明記してください。未供給の原log/SARIFや旧HTTP修正・解析37件を読んだとしないでください。顧客名・秘密・除外一覧を出さず、追加費用/APIキーAI/GPU/本人PC操作/Terminal/UAC/直接子Codexを行わないでください。読み取り専用で保存できないことだけを理由に再実行しないでください。