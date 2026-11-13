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
