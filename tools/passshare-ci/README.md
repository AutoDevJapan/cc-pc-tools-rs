# PassShare workflow comparison

This internal Go helper generates only `quality-gate.yml` and `mobile-build.yml`.
The original fixtures are the public API bytes at PassShare main
`c67bfb864acd51af0aafe972b2ea09e53d187ed7`. They are not a moving main checkout.
The desktop workflow is owned by the separate ToDo2994 implementation.

```text
go run . testdata/before <candidate-output>
TEST_SCRATCH=<safe-delete-owned-root> go test ./...
go vet ./...
```

The helper decodes YAML and compares unrelated jobs structurally, preserving
their commands, environments, dependencies and configuration except for the
explicit preflight dependency and the narrowly checked database-test repair.
The latter resolves the original Compose command rather than copying its test
list, and serializes Node test files to prevent concurrent migration deadlocks.
Tests run the emitted Bash selector against real
Git commits, including cross-client renames, deletions, multi-commit pushes,
lockfiles, shared inputs, consumed documents and failed comparisons.
The actual aggregate Bash/jq command is tested against failures and unexpected
skips; the database command is tested for full list preservation and rejection
of an unsupported Compose command format.
The owning caller reclaims the test scratch root using the safe-delete CLI.

The reusable mobile workflow retains the broader original mobile acceptance;
the narrower duplicate smoke filter is not substituted for full tests/Clippy.
The aggregate requires every unrelated job to succeed and accepts a skipped
client only when preflight explicitly marked that client unaffected.
No runner registrations, budget writes, deployment changes or API credentials
are part of this helper.
