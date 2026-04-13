# Git Blitz (gb)

[![CI](https://github.com/ntancardoso/gb/actions/workflows/ci.yml/badge.svg)](https://github.com/ntancardoso/gb/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/ntancardoso/gb)](https://github.com/ntancardoso/gb/releases) [![Go version](https://img.shields.io/github/go-mod/go-version/ntancardoso/gb)](go.mod) [![License](https://img.shields.io/github/license/ntancardoso/gb)](LICENSE)

A fast CLI tool for executing git and shell commands across multiple repositories simultaneously.

## Features

- Run any git or shell command across all repos at once
- Switch all repos to the same branch in parallel, falling back to a default if the branch doesn't exist
- Soft reset, hard reset, or rebase all repos to match `<remote>/<branch>`
- List current branches across all repos
- Recursively discovers git repos in subdirectories
- Configurable worker pool for parallel execution
- Exclude or include specific directories and branches
- Worktree management: create, remove, list, and open worktrees across all repos

## Installation

### Linux / macOS

```sh
curl -fsSL https://raw.githubusercontent.com/ntancardoso/gb/main/install.sh | sh
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/ntancardoso/gb/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\Programs\gb` and adds it to your user PATH automatically.

### Uninstall

**Linux / macOS:**
```sh
curl -fsSL https://raw.githubusercontent.com/ntancardoso/gb/main/install.sh | sh -s -- --uninstall
```

**Windows (PowerShell):**
```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/ntancardoso/gb/main/install.ps1))) -Uninstall
```

### Manual Installation

If your environment restricts running remote scripts, download the binary directly from [Releases](https://github.com/ntancardoso/gb/releases).

**Linux / macOS:**
```bash
# Extract and install
tar -xzf gb_<version>_<os>_<arch>.tar.gz
chmod +x gb
sudo mv gb /usr/local/bin/
```

**Windows:**
1. Download and extract `gb_<version>_windows_amd64.zip` from [Releases](https://github.com/ntancardoso/gb/releases)
2. Move `gb.exe` to a folder in your PATH, or add the folder to PATH

### Alternative: Go Install (requires Go)

```bash
go install github.com/ntancardoso/gb/cmd/gb@latest
```

### Build from Source (requires Go)

```bash
git clone https://github.com/ntancardoso/gb.git
cd gb
go build -o gb cmd/gb/main.go

# Linux/macOS
sudo mv gb /usr/local/bin/

# Windows: move gb.exe to a directory in your PATH
```

## Usage

### Basic Commands

```bash
gb [options] <branch_name>
```

**Execute a git command in all repositories:**
```bash
gb -c "status"           # Short form
gb --cmd "fetch origin"  # Long form
gb -c "pull"
```

**Execute a shell command in all repositories:**
```bash
gb -sh "ls -la"          # Short form (Unix/Linux/macOS)
gb -sh "dir"             # Short form (Windows)
gb --shell "mkdir tmp"   # Long form
gb -sh "pwd"             # Print working directory
```

**Switch all repositories to a branch:**
```bash
gb main
gb 15.0
gb feature-branch
gb feature-branch -r upstream  # fetch from upstream if branch not found locally
```
If the branch doesn't exist locally, gb fetches it from the remote (default: `origin`) and creates a local tracking branch.

**List all current branches:**
```bash
gb -l              # Short form
gb --list          # Long form
```

**Get help:**
```bash
gb -h              # Short form
gb --help          # Long form
```

**Show version:**
```bash
gb -v              # Short form
gb --version       # Long form
```

### Sync from Remote

Sync all repos to match a branch on a remote across your entire workspace at once. The default remote is `origin`; use `-r` to target a different one.

All three modes share the same pre-checks: repos are auto-switched to the target branch first (fetching from the remote if needed). Repos are **skipped** (not failed) when the branch doesn't exist on the remote, there is no matching remote, HEAD is detached, or the repo is already up to date.

**Soft reset** — move HEAD to `<remote>/<branch>`, keep working tree and index intact:
```bash
gb -rs main              # Soft reset to origin/main
gb --reset-soft main     # Long form
gb -rs feature/xyz       # Works with any branch name
gb -rs main -r upstream  # Use a different remote
gb -rs upstream/main     # Inline remote prefix (equivalent)
```
Safe for CI/non-interactive use. If a repo has staged changes before the reset, a warning is noted in the log output.

**Hard reset** — discard all local changes and reset to `<remote>/<branch>`:
```bash
gb -rh main              # Short form
gb --reset-hard main     # Long form
gb -rh main -r upstream  # Hard reset to upstream/main
```
> **Destructive.** Requires an interactive terminal. Before executing, gb scans all repos for dirty state and shows a confirmation prompt listing any repos whose changes will be discarded. Repos that are mid-merge, mid-cherry-pick, or mid-revert are automatically skipped.

**Rebase** — rebase local commits onto `<remote>/<branch>`:
```bash
gb -rb develop           # Short form
gb --rebase develop      # Long form
gb -rb develop -r upstream  # Rebase onto upstream/develop
```
> **Requires interactive terminal.** If a conflict occurs in any repo, `git rebase --abort` is run automatically to restore a clean state; the repo is reported as failed.

**Inline remote prefix (reset/rebase only):**

For reset and rebase, instead of `-r <remote>`, you can prefix the branch argument with the remote name. The prefix is validated against the repo's actual remotes, so branch names containing `/` (e.g. `feat/x`) are handled correctly.

| Command | Equivalent to |
|---------|--------------|
| `gb -rs origin/main` | `gb -rs main` |
| `gb -rs upstream/main` | `gb -rs main -r upstream` |
| `gb -rs origin/feat/x` | `gb -rs feat/x` |
| `gb -rs feat/x` | `gb -rs feat/x -r origin` |

**CI / non-interactive use:**
```bash
# Safe in CI — soft reset does not require a terminal
gb -rs main

# These will exit with an error if stdin is not a TTY:
# gb -rh main
# gb -rb main
```

### Worktree Commands

Manage git worktrees across all repos simultaneously.

```bash
gb -wl                               # List all active worktrees
gb -ib develop -wl                   # List worktrees only in repos currently on develop
gb -wc feature/my-task               # Create worktrees branching from master
gb -wc feature/my-task develop       # Create worktrees branching from develop
gb -wr feature/my-task               # Remove worktrees for an exact branch name
gb -wr "feat/AB*"                    # Remove all worktrees whose branch matches feat/AB*
gb -i client-frontend -wr "feat/AB*" # Same, scoped to client-frontend repo only
gb -ib develop -wr "feat/AB*"        # Remove matching worktrees in repos on develop
gb -wo feature/my-task               # Print worktree paths (for scripting)
gb -l -iw                            # Include worktree repos in branch listing
```

The `-i`/`-e` (include/exclude dirs) and `-ib`/`-eb` (include/exclude by current branch) filters all apply to worktree commands. By default, worktree repos are excluded from all operations. Use `-iw` / `--include-worktrees` to include them.

The `-wr` flag supports glob patterns (`*`, `?`, `[...]`) to match multiple branches at once. The main worktree is never removed.

### Advanced Options

```bash
# Use more workers for faster processing
gb -w 50 -c "status"           # Short form
gb --workers 50 -c "status"    # Long form

# Custom page size for progress display
gb -ps 10 -c "status"          # Show 10 repos per page
gb --size 30 -c "status"       # Show 30 repos per page

# Exclude additional directories
gb -e "build,dist,temp" -c "status"           # Short form
gb --excludeDirs "build,dist,temp" -c "status"  # Long form

# Include normally excluded directories
gb -i "vendor,node_modules" -l                # Short form
gb --includeDirs "vendor,node_modules" --list  # Long form

# Glob patterns are supported for -i and -e (*, ?, [...])
gb -i "feat-*" -l                             # Include all directories matching feat-*
gb -e "build-*,dist-*" -c "status"           # Exclude directories matching build-* or dist-*

# Only run in repos on a specific branch
gb -ib main -c "fetch origin"                 # Short form
gb --includeBranches main -c "fetch origin"   # Long form

# Glob patterns are supported for -ib and -eb (*, ?, [...])
gb -ib "release/*" -c "fetch origin"          # Only repos on a release/* branch
gb -eb "feat-*" -c "status"                   # Skip repos on any feat-* branch

# Exclude repos on a specific branch
gb -eb main -c "fetch origin"                 # Short form
gb --excludeBranches main -c "fetch origin"   # Long form

# Combine options (mix short and long forms)
gb -w 10 --includeDirs "custom-vendor" -c "fetch"
gb --workers 10 -i "custom-vendor" main
```

### Full Command Reference

```
Usage: gb [options] <branch_name>

Options:
  -h, --help              Show this help message
  -v, --version           Show version information
  -l, --list              List all branches found in repositories
  -c, --cmd string        Execute a git command in all repositories
  -sh, --shell string     Execute a shell command in all repositories
  -w, --workers int       Number of concurrent workers (default 20)
  -ps, --size int         Number of repos to display per page (default 20)
  -e, --excludeDirs string   Comma-separated list of directories to exclude from execution
  -i, --includeDirs string   Comma-separated list of directories to include in execution (glob patterns supported: *, ?, [...])
  -rs, --reset-soft string   Soft reset all repos to <remote>/<branch>
  -rh, --reset-hard string   Hard reset all repos to <remote>/<branch> (destructive, confirms first)
  -rb, --rebase string       Rebase all repos onto <remote>/<branch> (confirms first)
  -r, --remote string        Remote name to use when fetching (switch, reset, rebase) (default: origin)
  -ib, --includeBranches string
                             Only operate on repos currently on these branches (comma-separated, glob patterns supported)
  -eb, --excludeBranches string
                             Exclude repos currently on these branches (comma-separated, glob patterns supported)
  -iw, --include-worktrees   Include worktree repos in operations (default: excluded)

Worktree Commands:
  -wl, --worktree-list              List all active worktrees across all repos
  -wc, --worktree-create string     Create worktrees for <branch> (optional base as positional arg, default master)
  -wr, --worktree-remove string     Remove worktrees for <branch> across all repos (glob patterns supported: *, ?, [...])
  -wo, --worktree-open string       Print worktree paths for <branch> across all repos

Examples:
  gb main                               Switch all repos to main branch
  gb -l                                 List all current branches
  gb -w 50 -l                           Fast branch listing with 50 workers
  gb --workers 5 main                   Switch with 5 concurrent workers
  gb -e "build,temp" -l                 List branches, excluding build and temp directories
  gb -i "vendor,dist" 15.0             Include normally excluded directories
  gb -c "status"                        Execute 'git status' in all repositories
  gb --cmd "fetch origin"               Execute 'git fetch origin' in all repositories
  gb -sh "ls -la"                       Execute 'ls -la' shell command in all repositories
  gb --shell "mkdir tmp"                Execute 'mkdir tmp' shell command in all repositories
  gb -rs main                           Soft reset all repos to origin/main
  gb -rs main -r upstream               Soft reset all repos to upstream/main
  gb -rs upstream/main                  Soft reset all repos to upstream/main (inline remote)
  gb -rh feature/xyz                    Hard reset all repos to origin/feature/xyz (with confirmation)
  gb -rb develop                        Rebase all repos onto origin/develop (with confirmation)
  gb -ib main -l                        List branches, only repos currently on main
  gb -eb main -c "fetch origin"         Fetch in all repos except those on main
  gb -l -iw                             List branches including worktree repos
  gb -wl                                List all worktrees across repos
  gb -ib develop -wl                    List worktrees only in repos currently on develop
  gb -wc feature/my-task                Create worktrees for feature/my-task (base: master)
  gb -wc feature/my-task main           Create worktrees branching from main
  gb -wo feature/my-task                Print worktree paths for feature/my-task
  gb -wr feature/my-task                Remove worktrees for feature/my-task
  gb -wr "feat/AB*"                     Remove all worktrees whose branch matches feat/AB*
  gb -i client-frontend -wr "feat/AB*"  Remove matching worktrees in client-frontend only
  gb -ib develop -wr "feat/AB*"         Remove matching worktrees in repos on develop
```

## Default Excluded Directories

By default, gb excludes these directories to improve performance:
- `vendor`, `node_modules`, `.vscode`, `.idea`
- `build`, `dist`, `out`, `target`, `bin`, `obj`
- `.next`, `coverage`, `.nyc_output`
- `__pycache__`, `.pytest_cache`, `.tox`
- `.venv`, `venv`, `.env`, `env`

Use `-i` / `--includeDirs` to include specific directories or `-e` / `--excludeDirs` to add directories to the exclude list.

## Example: Odoo Development Workflow

For Odoo developers managing multiple OCA modules:

```
/odoo-projects/
├── odoo/                 # Core Odoo
├── design-themes/        # Themes
├── OCA/
│   ├── project/          # OCA project module
│   ├── survey/           # OCA survey module
│   └── ...               # Other OCA modules
└── custom/               # Custom modules
```

1. **Navigate to your Odoo projects folder:**
```bash
cd ~/odoo-projects
```

2. **Check current branch status:**
```bash
gb -l
```

3. **Synchronize all repositories to version 15.0:**
```bash
gb 15.0
```

4. **Fetch updates from all remotes:**
```bash
gb -c "fetch origin"
```

5. **Sync all repos to match origin/15.0 (discard local changes):**
```bash
gb -rh 15.0
```
Or keep local changes staged instead:
```bash
gb -rs 15.0
```
Or sync from a different remote:
```bash
gb -rs 15.0 -r upstream
```

6. **Check status across all repos:**
```bash
gb -c "status"
```

## Error Handling

- **Branch not found**: Reports repositories where the target branch doesn't exist
- **Switch failures**: Shows detailed error messages for failed operations
- **Permission issues**: Handles repository access problems gracefully
- **Network issues**: Manages remote fetch failures appropriately
- **Command execution failures**: Shows detailed error messages with log review option
- **Sync: branch not on remote**: Repo is skipped (not failed); counted separately in summary
- **Sync: dirty state warning (soft reset)**: Repos with staged changes before a soft reset log a warning in the output; the reset still proceeds
- **Sync: hard reset with local changes**: Pre-flight scan lists all affected repos before the confirmation prompt; user can abort cleanly
- **Sync: rebase conflict**: `git rebase --abort` is run automatically; repo is reported as failed with clean state restored
- **Sync: non-interactive terminal**: `-rh` and `-rb` exit with an error if stdin is not a TTY; use `-rs` for CI pipelines
- **Sync: mid-operation repo**: Repos in the middle of a merge, cherry-pick, or revert are skipped by hard reset to avoid silent data loss

## Requirements

- **Git**: Must be installed and accessible in PATH
- **Go 1.19+**: For building from source
- **File System**: Read access to repository directories

## Platform Support

- **Windows**: Full support including WSL symlinks and NTFS junctions
- **Linux**: Native support with symlink resolution
- **macOS**: Native support with symlink resolution

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature-branch`)
3. Run tests: `go test ./...`
4. Commit your changes
5. Push to the branch
6. Create a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
