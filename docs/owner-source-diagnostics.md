# Source classification diagnostics

`scrub391 --guard-path <absolute-path> --classification-diagnostic` runs the
ordinary classification and adds fixed timing and identity-validity metadata.
The normal guard output remains unchanged when the option is omitted.
An explicit `--classification-diagnostic=false` also selects ordinary output.

The diagnostic reports registration reads, local Git calls, identity checks,
rules and the final registration snapshot check in milliseconds. Git, identity,
plain-path and membership timings are contained within the rules timing;
they must not be added together as independent elapsed periods. A fixed
`deadline_stage` identifies a measured phase which observed cancellation.
The option neither extends the 750 ms classification budget nor converts an
unknown classification into access.
`stage` matches the final returned classification basis, including any existing
independently qualified retirement receipt. `policy_stage` preserves the policy
return before that final retirement adjustment. Earlier operational or
conversation receipt fallbacks may already be included in that policy return.
The diagnostic neither creates a receipt nor changes whether one qualifies.

For one normally decoded and validated repository identity, the output includes
its checked time, expiry, observation time and current validity. It contains no
repository names, registry contents, error bodies, tokens or headers. Multiple
different identities suppress the single-identity timestamps. Missing or
invalid records do not acquire fabricated timestamps.

An identity expires 300 seconds after its checked time. A successful refresh
which reports `attempted: false` may only have reused a still-valid identity;
it does not renew that time. An expired identity and a classification deadline
are different failures. The diagnostic performs no extra network lookup or
cache write. Existing finite background refresh behavior remains unchanged.

Physical resolution has its own call count, elapsed milliseconds and fixed
state (`same`, `mismatch`, `not_found` or `unavailable`). This precheck is
outside the classification context budget. Identity phase context state
records cancellation observed before the call, during the call, or different
observations across multiple calls. It does not identify the cause of a slow
lookup or expose paths, repository names or error text.

## Refresh cooldown cause

`scrub391 --refresh-owner-source <absolute-path> --diagnostic` reports the stage
`cooldown_no_valid_identity`, without a network lookup, while a recent failed or
unfinished refresh keeps the repository in its 300 second cooldown. When a cause
is known, that output also carries a `cooldown` object with fixed fields only:

- `kind`: `fetch_failed`, `helper_unfinished`, `helper_start_failed` or
  `unknown`. `fetch_failed` covers every failure after a fetch attempt began, so
  a fetched identity that could not be stored or bound is `fetch_failed` too;
  `stage` tells which.
- `stage`: the identity stage of the failed attempt.
- `cli_exit`: 0 to 255. An exit status outside that range is not reported, so a
  crash status and a CLI that never ran look alike.
- `http`: 401 or 403 when the CLI reported that status. Nothing finer is
  reported; a 403 can be a rate limit or a missing permission.
- `age_seconds`: seconds since the failure.

`unknown` means that no usable cause was stored: none at all, for example by an
earlier guard version, or a record that is not a valid sealed cause for this
repository, catalogue and failure second.

One refresh run can check several repository identities, so the object follows a
fixed rule. It explains the failure, therefore it is built only from the
identities that failed because they were cooling and had no valid cached
identity. An identity that was still served from a valid cached identity does
not contribute, even while its own failure marker applies. Contributing
identities that share the same `kind`, `stage`, `cli_exit` and `http` report
that cause with the `age_seconds` of the most recent failure among them. If they
differ in any of those four fields, the object is omitted, because one cause
cannot name them all. The object is shown only when the final stage is
`cooldown_no_valid_identity`, or when `identity_failure` is that stage in a
`partial_identity_failure`; other stages such as `multiple_identity_failures` or
`deadline_exceeded` never carry it.

The cause is stored in a sealed record next to the failure marker, sealed for
the current Windows user like the marker. The reported object never contains a
path, repository name, token or error text. The sealed record itself holds the
repository, the catalogue hash and the failure second, as the marker does. They
only tie a cause to its marker and are never reported. A cause belongs to a
marker when those three agree, so two failures of one repository within the same
second can show the cause of the other. Values outside the fixed vocabulary are
dropped when the record is read, and the object is absent when no cooldown
applies. The record is not deleted; there is at most one per repository and
catalogue, and it is read only while the marker applies. The cause does not
change the cooldown length, the marker format, any classification or any access
decision.
