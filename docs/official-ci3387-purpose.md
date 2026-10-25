# Fixed official Windows CI software preparation

This is the ordinary implementation task 3894, following the finite synthetic
check 3893 (candidate commit `c252cf51a13a451dda54c1063d8c3a6bac1a3ef4`).
The candidate is input to implementation, not production acceptance.

Only the official `actions/runner` v2.338.0 Windows x64 archive is supported.
The fixed repository, PR, head, tree, run, attempt, job, labels, asset ID, size,
URL and SHA256 are compiled purpose constraints. There are no caller overrides.
The existing CI provider 3888 retains registration, credentials, execution and
cleanup of its runner. This tool prepares public software only.

## Trust and lifecycle

The normal registered source and current scheduler assignment establish the
implementation actor. The actual GitHub owner/repository identity, original
parent and provider lineage, still-current PR/run/job and official release/asset
are independently checked. A fresh safe-delete `create` is called by this
constructor; a caller cannot supply an old root, receipt, URL or digest.
The original issuer output, its exact bytes, creation-time physical identity,
source proof and fixed purpose are authenticated in a dedicated creation ticket.
This is one finite purpose journal, not a general path registry.

The downloaded buffer is size/hash checked and that same buffer is parsed.
Every output is created exclusively below the fresh owned anchor. Reparse
points, hardlinks, ADS, unsafe archive names and unknown existing entries fail.
Receipt transitions authenticate the same bytes that are decoded, compare the
whole prior record under an exclusive native lock, and preserve partial states.
Preparation and verification never start or configure software, request a token,
or return credentials. Verification returns metadata. The one-shot byte consumer
returns only the authenticated public file snapshots programmatically; its
stdout must not be displayed to an AI or terminal as a bulk SDK dump.

General `--guard-path`, current denials, original registry recovery, unknown
data, old SDKs and existing retirement receipts are unchanged. All purpose
responses retain `access_allowed:false`, `original_registry_recovered:false`
and `launch_performed:false`.

## Acceptance cases

| Given | When | Then |
| --- | --- | --- |
| Legacy guard | New preparation flag | Red: unsupported; no input opened |
| Registered ordinary implementation actor and exact current CI | Prepare | One normal new owned root and authenticated complete receipt; same official bytes installed |
| Different owner/repository/head/tree/run/attempt/job/labels/asset/hash | Prepare/verify | Deny before creation or consumption |
| Caller-supplied root, repeated/mixed/unknown flags | CLI | One bounded machine JSON refusal; no side effects |
| Modified/copied ticket, pending receipt, replaced physical object, unknown sibling or altered public bytes | Verify/consume | Deny; no general access grant |
| Complete receipt, exact original CI and registered provider actor | Consume | One callback sees the same verified public bytes; concurrent/repeated consumers fail |
| Interrupted writes or callback failure | Restart | Preserve explicit pending/consuming/failed state; do not adopt or replay |
| Unrelated original source/path | Existing guard | Existing classification remains unchanged |

## Required evidence

Record meaningful legacy Red, candidate and adapter Green, CLI boundaries,
Windows physical identity and receipt tampering tests, Go race/vet and affected
existing guard callers. No Cargo/GPU/Civ6 allocation is used. A separate actual
model with max effort reviews only this change through a normal typed review
ToDo. Necessary same-head formal CI, merge, source/binary/hash/ACL comparison and
normal distribution precede the single real fixed SDK preparation acceptance.
That original receipt and exact entry point are returned to 3387 and 3888;
neither the private PR nor the original whole task is completed here.
