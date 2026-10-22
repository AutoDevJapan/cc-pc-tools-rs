# X bookmarks daily launcher (Windows)

Recovered from the original completed source-read record after the runtime executable,
source and prompt disappeared. The recovered source rebuilt with Go 1.27.0,
`-buildvcs=false -trimpath`, to the independently recorded original executable SHA-256
`b94566f460864d2872c57d59afe17653bf7422613ad928dcdbfde06223843c15`.
The recovered main.go SHA-256 was
`4a2af6a02c578e02ca94d5ccd7a5e67fb8a149e229ef59950e54c7f74a43274c`.

The runtime home remains `D:/CC/Tools/x-bookmarks`. Keep `done.txt`, daily runs and
existing ToDo/worker relationships. Do not replace missing historical records with
blank completion ledgers.

Build on Windows with `go test ./...`, `go vet ./...` and
`go build -buildvcs=false -trimpath -ldflags=-H=windowsgui -o x-bookmarks-daily.exe .`.
The GUI subsystem and existing CREATE_NO_WINDOW child execution prevent console windows.

`--dry-run` does not launch or write. `--probe` checks a real hidden child process.
`--adopt-manual-todo ID` binds today's real manually performed work to the existing ToDo,
using `runs/YYYY-MM-DD.json` with `mode=dedicated-manual`. It requires matching date/ToDo
and at least one item with both done registration and confirmed bookmark removal.
It does not invent a job ID or claim that a partial batch is complete. Subsequent normal
invocations report the actual partial count and avoid starting a second worker.

New daily ToDos retain their prompt through `--spec`; job launches carry the actual
ToDo, project and Japanese title. Nonzero child failures remain nonzero launcher results.
The task's zero result only establishes successful launcher handling; daily item completion
must still be read from the canonical run and verified separately.

The worker prompt distinguishes an unverified asset classification from a confirmed
client resource and a concrete execution-tool refusal. An unverified classification
alone must not park every explicitly requested Skill or X operational-file operation
behind registry restoration. The worker presents the original scope and provenance
to the normal operation, retains actual protections, and holds only a refused operation.
It never disables guards, copies protected originals, substitutes completion ledgers,
or treats a new daily launch/status question as permission to repeat a rejected action.
An existing worker's real job/run evidence is checked before any overlapping processing.
These instructions do not establish that the next worker has executed or completed them.

Manual adoption refuses to overwrite a recorded job, another ToDo or corrupt state.
If linking an accepted worker to its ToDo fails, the next invocation retries that link
without launching a new worker. Child commands use a three-minute timeout, bounded
pipe drain and separate stdout receipt parsing; stderr warnings cannot become job IDs.
