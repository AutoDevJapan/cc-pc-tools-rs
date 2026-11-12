# Recovery metadata from the job manager

Normal `status --job-id ID --json` keeps its existing fixed metadata output.
An exact `--purpose` may request one of the two assigned recovery projections:

- `karaoke-1157-locators`: the stored absolute working directory, an existing
  result locator, and the original question identifier only when that original
  attribute is explicitly stored. A later question route is not evidence of
  original status. Missing or unverified values remain `null`.
- `fourbros-1245-launch-failure`: a fixed failure category for the original
  failed launch, after validating its job/session/launch binding. It returns no
  command, diagnostic text, credentials, or source bodies. An absent or
  unrecognized diagnostic returns `unknown`.
  Fixed diagnostic tags, a bounded OS error number and the launcher kind can
  narrow that unknown result without returning diagnostic text or a path.

These purposes accept only their compiled original job identifiers and JSON
output. They do not enumerate other jobs, change records, grant data access,
restart a job or send a question. Reads are bounded, duplicate JSON keys are
rejected, and the launch binding is checked before opening its diagnostic.
The diagnostic category is evidence for investigation, not permission to retry.
