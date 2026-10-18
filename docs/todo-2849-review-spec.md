# Independent read-only review request: ToDo 2849 guard change

Review the exact source diff `c9f0b26df6014c5473e110996b58cda18dff151e..0d48ef511829cf3100e490dc3820c9554d106ca1` in AutoDevJapan/cc-pc-tools-rs. Focus on `repair-guard/current_windows.go` and `repair-guard/current_windows_test.go`; read the current project AGENTS.md and the changed current-inheritance notes for context.

Check whether the new finite product declaration can admit any path, project, task, spec, repository, owner or Git common directory outside the explicitly declared GoDD-AO/2839 and GoDD-13a13e1/2850 scopes. Check that registration root and immutable saved spec SHA are bound, original denials remain authoritative, existing 2839 behavior is preserved, and the regression test would catch a wrong task/repo/root/spec or sibling worktree. Also check for regressions, weak assertions and missing security-relevant cases in this narrow diff.

This is read-only. Do not edit, run commands, create commits, push, comment on GitHub, merge, change ToDo state other than returning this review through the assigned subwork, or access unrelated repositories/resources. Do not expose credential-like values or any protected client/project names. Report actionable findings only with severity, file and line, or state `Must 0; Suggestion 0; Nit 0` if none. Distinguish confirmed defects from optional improvements.
