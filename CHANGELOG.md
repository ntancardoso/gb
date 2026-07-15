# Changelog

## v0.2.7 — 2026-07-15

### Added
- Page navigation (↑/↓/PgUp/PgDn) stays live while the "View detailed logs? (y/N)" prompt is shown; y/n/Enter/Esc answer the prompt. Falls back to the plain prompt on non-TTY output or piped stdin.
- The hard-reset confirmation prompt now also warns about repos whose HEAD is ahead of the reset target (those commits would be discarded) and repos whose status check failed, not just dirty working trees.
- Stale `gb-logs-*` temp directories older than 7 days are purged on startup; the current run's logs are kept.

### Fixed
- `gb -dv` and `gb -tr` now exit non-zero when any repo fails, matching every other operation.
- Conflicting operation flags (e.g. `-rs` with `-rh`, `-c` with `-l`) are rejected instead of silently running only one.
- `gb -dv` with a stray positional argument errors instead of silently ignoring it.
- `-tr`/`--track` no longer consumes a following positional argument during flag reordering.
- In-flight git subprocesses now honor context cancellation (`exec.CommandContext`) in switch/reset/rebase/diverge/track, so a parent-driven cancel or SIGINT in non-interactive runs stops them promptly. While the interactive progress UI is active, Ctrl+C stops the display but the current git command still completes.
- Worktree removal no longer attempts to delete the workspace parent directories.
- `.env` copied into a new worktree keeps the source file's permissions.
- Installer scripts fail hard when the release checksum entry or sha256 tool is missing instead of skipping verification.

### Security
- Branch/ref/remote/pattern arguments starting with `-` are rejected, preventing option injection into git (e.g. `-rb=--exec=<cmd>`).
- `fetch`/`ls-remote` run with `protocol.ext.allow=never`; `git status` preflight runs with `core.fsmonitor=false`, so a scanned repo's config can't execute commands through those paths.
- Release workflow no longer interpolates the dispatch tag input directly into shell commands; CI workflow runs with read-only permissions.

## v0.2.6 — 2026-06-09

### Fixed
- `-rs`/`-rh`/`-rb` no longer doubled the remote prefix: `gb -rh origin/main` showed and reasoned about `origin/origin/main` because the confirmation prompt, summary, and logs were built from the hardcoded default remote plus the raw argument.

### Changed
- Reset and rebase now follow ordinary `git reset` semantics instead of always forcing a remote:
  - `gb -rh main` resets to the **local** `main` (no fetch).
  - `gb -rh origin/main` (inline prefix, where `origin` is a real remote), `gb -rh origin main` (two-token form), and `gb -rh main -r upstream` (`-r` flag) reset to the named remote-tracking branch.
  - A branch argument whose prefix isn't a real remote (e.g. `feat/x`) is treated as a local branch.
  - In local mode, if the branch is missing locally but exists on the default remote, gb falls back to `<remote>/<branch>` automatically; it's skipped only when the branch exists in neither place.
- Added the two-token `gb -rs|-rh|-rb <remote> <branch>` form.
- Stopped hardcoding `origin` as the reset/rebase target remote; `-r` now forces remote mode explicitly.
- The destructive confirmation prompt and summary for a bare-branch (local) reset now disclose the `<remote>/<branch>` fallback used where the branch isn't a local branch, instead of showing only the local ref.
- Ambiguous reset/rebase invocations now fail fast with a clear error instead of silently resolving to a surprising target: surplus positional arguments, combining the two-token form or an inline prefix with `-r`, and a non-bare remote token in the two-token form.
- The local→remote fallback no longer repeats the `git ls-remote` branch probe before fetching.
