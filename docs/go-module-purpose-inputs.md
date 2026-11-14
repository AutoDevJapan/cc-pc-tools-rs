# Go module input validation

The guard build depends on `gopkg.in/yaml.v3@v3.0.1`. This pure validator checks supplied module ZIP and `go.mod` buffers; it does not open a local cache, download a module, grant source access, or start a compiler. The normal build provider is not connected.

The module h1 and the separate `go.mod` h1 are checked independently. The ZIP container SHA is a different value. Paths, duplicate and case-colliding entries, Windows aliases, links, nested modules and finite size limits are checked before returning private copies of the verified buffers. The fixed-purpose path subset is deliberately stricter than a general Go module ZIP reader; actual-module compatibility still requires acceptance.

A producer label or a caller-supplied hash is not authentication. A connected provider must bind the qualified source module and `go.sum` to the reviewed source, authenticate its public input, retain the verified bytes through consumption, and preserve existing path protections. This validator cannot turn an unqualified cache into an allowed input.

The accompanying tests use synthetic public bytes. Source tests and hosted compilation do not prove that a local toolchain or module cache is qualified, nor that the installed guard was updated. See the [Go module authentication reference](https://go.dev/ref/mod#authenticating) and [dirhash definition](https://pkg.go.dev/golang.org/x/mod/sumdb/dirhash#Hash1).
