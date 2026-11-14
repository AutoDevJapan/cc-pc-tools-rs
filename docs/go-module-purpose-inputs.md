# Guard build: Go module input validator draft

This is an unconnected candidate for reviewing one guard-build input contract.
It does not authorize, open, copy, download, extract, or compile any actual
module, and it does not change the ordinary path guard. No dispatcher or build
producer calls this code. Matching its input fields is **not** proof of the
producer's identity or permission to read a protected path.

The intended build concerns PR #714, head
`4559c51bd1769449c8e8d5654206c03ee184fc1f`, merged at
`13871a992a7aeba888a1779b18d88a9e781f2b03`. The source owner supplied the
module identity `gopkg.in/yaml.v3@v3.0.1`, its module and go.mod h1 values,
and the main module's `go.mod`/`go.sum` digests. This draft's author could not
independently read those two files because the fresh path classification was
unverified. The supplied values are not embedded as independently verified
production pins. The existing module-cache object was classified protected;
its contents and digest were not read and must not be obtained through an
alternative path, download, or copy to bypass that result.

`validateGoModulePurpose` is only a pure byte-level candidate. Its caller
supplies ZIP bytes, the separate module `go.mod` bytes, and a proof structure.
It takes private copies, requires the fixed module/version and expected
producer label, compares exact buffer digests, verifies the Go module and
`go.mod` h1 hashes independently, and checks the embedded root `go.mod` bytes
match the supplied bytes. It rejects unsafe paths, collisions, links, special
entries, nested modules, and size/entry excesses. Its ASCII-only paths and
rejection of explicit directories are stricter than the general Go module ZIP
format; compatibility with the actual module has not been established.

The producer label and caller-supplied hashes are not an authority boundary.
A future integration must independently establish the original producer and
the fixed purpose, bind its held `go.mod`/`go.sum` and complete selected source
set to the reviewed merge, and admit an input only after its own normal path
qualification. It must compare the actual h1 values with that qualified
`go.sum`, retain and consume the same verified buffers, reject any unknown or
changed input or producer, and leave the existing owner protections intact.
The public-module input source and this trusted binding do not yet exist.
Neither a hash supplied by the caller nor this validator changes that fact.

Go's h1 represents sorted names and file hashes, not the SHA-256 of the ZIP
container. The separate `go.mod` entry has its own h1. See the official
[module authentication reference](https://go.dev/ref/mod#authenticating) and
[dirhash definition](https://pkg.go.dev/golang.org/x/mod/sumdb/dirhash#Hash1).
`go mod verify` is not a substitute for source permission or the missing
purpose-bound producer.

Synthetic tests are drafted for the hash distinction, copied buffers, wrong
producer label/identity/version/sums, malformed ZIPs, path and size boundaries.
They have not yet been compiled or executed. The Go compiler and formatter were
queried before their fresh guard results were checked; later results classified
both as protected. That execution order is retained in the task record and is
not a successful qualification. No formal guard build or installation occurred.
