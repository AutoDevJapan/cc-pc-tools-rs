# Hand off one completed writer result

`codex_jobs.exe handoff-finished-work` removes one recorded automatic writer edge from one queued, never-started worker after the completed result has been delivered to the parent task. It does not start a job, complete a ToDo, or change execution permissions. Normal queue admission checks every remaining dependency, live writer, resource limit, and stop condition.

Preview and apply use the same queue, job-state, and ToDo locks:

```text
codex_jobs.exe handoff-finished-work --request-file request.json
codex_jobs.exe handoff-finished-work --request-file request.json --apply
```

The request must identify the exact candidate job, its result-waiting parent, and the finished writer. It pins the candidate, parent, and writer specifications plus the candidate's original prompt by SHA-256. It also identifies the delivered writer source commit and the commit that changed the writer module.

The command checks that:

- The parent ToDo still waits for the candidate, and the candidate has no manual or owner stop, ToDo prerequisite, or separate subwork relation.
- The candidate is still the exact queued job with zero steps and only one automatic wait, whose saved evidence names the selected writer.
- The parent job and selected writer completed successfully. Every job in the writer ToDo's recorded history is terminal and its saved PID and creation-time identities are no longer live.
- The writer ToDo remains in `wait` on at least one unresolved prerequisite unrelated to the candidate, and has no owner stop.
- The parent note contains the delivery marker and both pinned source commits. The writer checkout is still at the delivered commit and has no uncommitted changes.
- The writer commit changes files only inside the declared writer module. The writer and candidate module scopes are separate and each is named in its original specification.
- The registered jobs still refer to the same repository, Git common directory, and PR while using separate worktrees and branches.

Apply stores the original wait, evidence, delivery receipt, and request in the candidate job's history. It clears only that job's automatic wait and queue diagnostic. The following normal admission is responsible for the actual start; if another live writer or any other prerequisite remains, the candidate stays queued.

This command is for a delivered, successful implementation whose ToDo remains open for a separate unfinished item. It does not replace review, test, CI, merge, or cleanup requirements.
