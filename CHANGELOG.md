# Changelog

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
