# Fixed Rust toolchain inputs

The purpose-specific matcher covers Rust `1.95.0-x86_64-pc-windows-msvc`, the fixed install alias, and the retained `cargo.exe` and `rustc.exe` buffers. It does not change the general path guard, registry, ACLs, installation, or execution permissions.

The fixed release manifest is `https://static.rust-lang.org/dist/channel-rust-1.95.0.toml`, dated `2026-04-16`, with SHA256 `821ff14e4c4a1cbe1e8915f35aff0a3fbbdf8d293ad48ab8f31e3b0440c581f9`. Its component set is cargo, rustc and rust-std for the one target. Archive hashes and extracted executable hashes are distinct. Verifying an executable does not verify the complete DLL, linker or standard-library execution closure.

`rustToolchainOriginProvider.WithHeld` supplies one synchronous callback while retaining the alias, parents, identities, ACL observations and buffers. The consumer checks the fixed purpose, calls `Recheck` before and after using the same buffers, and checks the complete volume/file ID/creation-time identities. The provider reports cancellation, recheck and close failures. A JSON receipt, producer label, arbitrary path or caller hash cannot construct the normal provider.

The shared provider source implements fixed HTTPS metadata intake, bounded gzip/tar validation and a native install-alias adapter. It is not connected to the normal consumer factory. Its current-object purpose qualification and ACL policy still require independent acceptance before the installed body is opened through that route. Public metadata alone does not authorize that read. No canonical-path substitute is an access workaround. Historical installation details and signature verification remain separate, unknown facts unless independently obtained; no historical receipt is fabricated.

The pure matcher checks consistency and never issues a grant. Synthetic matcher, provider and consumer tests establish their own limited contracts. Hosted Go compilation does not establish current local qualification or compiler success. Source qualification, native acceptance, purpose-specific runtime connection, a formal product build and installed readback remain separate steps.

The [release layout](https://forge.rust-lang.org/infra/channel-layout.html) describes manifest checksums and detached signatures. Current official-payload equivalence, signature verification and historical installation provenance must not be conflated. See [the fixed Unslop consumer](unslop-toolchain-purpose.md) for consumption and failure behavior.
