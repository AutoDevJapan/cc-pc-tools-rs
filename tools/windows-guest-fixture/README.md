# Windows Job Object lifecycle fixture

This finite, credential-free fixture creates one uniquely named Windows Job
Object and two real child processes. Both children are created suspended,
assigned to that job before they can run, and then resumed. The `normal` case
observes both live, waits for one child to return normally, and verifies that
closing the owned job kills the remaining child. The `cancel` case cancels a
finite context while both children are alive, closes the same owned job, and
verifies both process handles have exited. Both cases also verify that the
named Job Object no longer exists.

The nonce used for the temporary kernel object name is generated in memory,
cleared after object creation, and represented in evidence only by SHA-256.
No credentials, source checkout, network, GPU, profile file, or persistent
registration is used. The helper itself runs on the Windows host; the fixture
does not provision a Hyper-V VM or RAM disk and does not execute the GoDD
pipeline.

```powershell
go test ./...
go run . normal
go run . cancel
```

The results prove cleanup of this disposable Windows process namespace only.
They do not prove cleanup after host shutdown, logout, or reboot, and do not
establish that the original Windows CI workflow succeeds.
