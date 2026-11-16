# Civilization VI artifact observation and preflight

This component validates software build material without starting an executable, loading a DLL, attaching to a game, or changing an installation. A matching JSON record or hash is data; it does not create a source, tool, or target permission.

## Source and selected artifacts

The current product material is from [ec98942d](https://github.com/AutoDevJapan/civ6-banseiki/commit/ec98942d0bb28f7c63e38c7ee3cfd75bc717d254), tree `7acbe7525928f67711c34658de9368455e13f2df`, in repository `AutoDevJapan/civ6-banseiki` (ID `1392152644`). Its [Rust and Lua workflow run](https://github.com/AutoDevJapan/civ6-banseiki/actions/runs/43824667794) and Windows job `133155508662` published artifact `14208090178`, attempt 1. This is the product revision, not the software-reader revision.

| Role | Cargo package | Target | Windows output |
| --- | --- | --- | --- |
| autotest | civ6-autotest-rs | civ6-autotest | civ6-autotest.exe |
| diagnostics | civ6-autotest-rs | civ6-startup-diagnostics | civ6-startup-diagnostics.exe |
| scene | civ6-autotest-rs | civ6-bootstrap-scene | civ6-bootstrap-scene.exe |
| bridge | civ6-native-bridge-rs | civ6_native_bridge_rs | civ6_native_bridge_rs.dll |
| bootstrap | civ6-bootstrap-rs | civ6_bootstrap_rs | civ6_bootstrap_rs.dll |

The ZIP has these five software files plus `manifest.json` and `cargo-artifacts.json`. The provider independently compares the artifact's run, attempt, job, source, workflow blob, server digest, manifest and selected Cargo records. The manifest's ordered source inventory and separate before/after input projections must agree with the fixed Git namespace. The five outputs are inspected as Windows x64 PE images with the required executable/DLL roles and typed export metadata.

The original source constants `6a77a4ae7b9368a45a17ba0a5ac2ae72529355a1` and `c52c78ebce0b84891bca5df78cbe7fe279f87282` remain historical comparison fixtures. They are not aliases for the current product revision or an admitted producer.

## Historical capture events

The current producer emits `civ6-native-capture/v3` records with `evidence_kind: historical_capture_snapshot`. Each record must contain all 23 exact keys. Duplicate, unknown and case-folded aliases are rejected.

The origin record likewise requires all 14 exact non-null keys, including case-sensitive names.

The phase must be a non-null, nonempty string before dispatch. Valid nonselected phases, including git, rustup, metadata and generator, remain outside the bins/dlls success predicate. Malformed extra records are rejected even when a complete valid bins/dlls pair appeared earlier.

Selected bins and dlls records each require a known native exit 0, complete collection, completed capture reason, returned Start, observed root exit, saved result receipt facts, owned hashed streams, no primary exception, and zero secondary failures. Empty stderr is valid only with the empty-byte digest. The two stream digests must match the corresponding manifest command records. Legacy v2 records are not silently upgraded.

An event records what was true at its emission. It is not a current receipt or an issuer. Completed same-generation producer/job checks, source verification, the current archive's bytes and the consumer's held-object checks remain separate requirements. A later cleanup or publication failure is not repaired by accepting an earlier historical event.

## Held objects and cleanup

The observation implementation can create an operation-owned child stage with the five outputs using the original CreateNew handles. It retains the parent namespace, file identities and ACL observations while inspecting the same bytes before and after consumption. It checks both file-backed function exports and bounded virtual data exports, which may reside in a zero-filled image section.

The caller owns the returned prepared/stage object even on an error. Cancellation, uncertain creation, foreign entries, failed disposition and unconfirmed closes retain the corresponding owner for reconciliation. `Cleanup` reconciles and removes only the exact objects created by this operation. `Close` releases those handles only after that cleanup is confirmed; an uncertain or foreign namespace remains held. Directory inventory is read from the original held directory handle, so the owner keeps DELETE access without a conflicting path reopen. Releasing this operation's handles does not prove that every external native handle has drained.

## Normal connection

The published reader base supplies a normal Windows Credential Manager-backed Guard transport. Its request policy is specific to the Guard purpose. It is not automatically a Civilization artifact issuer. Its types and cleanup methods must be preserved when integrating this component.

The Civilization-specific normal factory still reports an unconnected purpose. A complete connection requires:

1. An actual authenticated owner for this fixed repository, reviewed producer and task purpose.
2. Qualification of the current reader/tool image and the selected product source and inputs.
3. A fixed operation-owned stage parent factory and ownership through acquisition, current-byte verification, consumption and cleanup.
4. A consumer that borrows only those held bytes within that generation and rechecks them before and after use.

Caller-selected paths, tokens, IDs, JSON, hashes, generic HTTP clients or boolean flags cannot replace those issuers. `Session::start_bootstrap` starts a native controller and is not an offline import or read-only preflight.

## Validation and remaining acceptance

The tests include the two original v3 bins/dlls log records, fixed manifest-stream associations, missing/null/type/case-alias and duplicate fields, cancellation, cleanup and primary/secondary failures, legitimate nonselected phases, and malformed extra phases appended to an otherwise valid pair. These are source tests until they have actually run against the integrated reader revision. Product workflow success does not compile or qualify this reader.

The product's same-revision CI and material inspection do not establish all native producer shutdown, OS and remote receive-generation closure, or a complete Session birth window. The normal product acceptance still requires 22 leaders, same-process real save/load cycles and save comparisons, both AI payment paths, unpaid-placement refusal, partial-payment multiplayer save/rejoin, the original full scenario and six save pairs, and formal spy neutralization followed by saved recovery without changing RNG results.

For AI-operated entry points, Codex and Claude Code are both minimum supported hosts. Each must reach the same normal preflight purpose and representative result through its actual configured entry and authentication. Source, registration or a result from one host is not proof of both-host use. This host requirement does not change product inference-model requirements, execution permissions or target ownership.
