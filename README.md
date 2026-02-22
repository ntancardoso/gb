# Git Blitz (gb)

A fast CLI tool for executing git and shell commands across multiple repositories simultaneously.

## Use Cases

- **Multi-Repo Command Execution** - Run any git or shell command across all repositories at once
- **Branch Synchronization** - Switch all repos to the same branch in parallel
- **Origin Sync** - Reset or rebase all repos to match `origin/<branch>`, like a bulk "sync branch" button
- **Status Overview** - List current branches across all repositories
- **Version Management** - Keep related projects (like Odoo/OCA modules) in sync

## Features

- **Git Command Execution**: Execute any git command across all repositories
- **Shell Command Execution**: Execute any shell command across all repositories
- **Bulk Branch Switching**: Switch all repos to a target branch with smart fallbacks
- **Origin Sync (soft reset)**: Move all repos' HEAD to `origin/<branch>` without touching working tree
- **Origin Sync (hard reset)**: Discard local changes and reset all repos to `origin/<branch>`
- **Origin Sync (rebase)**: Rebase all repos onto `origin/<branch>` with automatic conflict abort
- **Branch Listing**: View current branches across all repositories
- **Recursive Discovery**: Automatically finds all Git repositories in subdirectories
- **Concurrent Processing**: Uses configurable worker pools for fast execution
- **Flexible Filtering**: Skip or include specific directories with customizable rules
- **Progress Display**: Real-time updates with pagination for large repo sets
- **Log Capture**: Stores detailed logs for each repository operation
- **Cross-Platform**: Works on Windows, Linux, and macOS with symlink/junction support

## Installation

### Download Binary (Recommended)

**Windows:**
1. Download and extract `gb.exe` from [Releases](https://github.com/ntancardoso/gb/releases)
2. Move to a folder in your PATH or add the folder to PATH

**Linux/macOS:**
1. Download and extract `gb` from [Releases](https://github.com/ntancardoso/gb/releases)
2. Make executable and move to PATH:
```bash
chmod +x gb
sudo mv gb /usr/local/bin/
```

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

# Windows
# Move gb.exe to a directory in your PATH
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
gb 15.0
gb main
gb feature-branch
```

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

### Sync from Origin

Sync all repos to match a branch on `origin` — similar to Bitbucket's "Sync branch" button but across your entire workspace at once.

All three modes share the same pre-checks: repos are auto-switched to the target branch first (fetching from origin if needed). Repos are **skipped** (not failed) when the branch doesn't exist on origin, there is no origin remote, HEAD is detached, or the repo is already up to date.

**Soft reset** — move HEAD to `origin/<branch>`, keep working tree and index intact:
```bash
gb -rs main              # Short form
gb --reset-soft main     # Long form
gb -rs feature/xyz       # Works with any branch name
```
Safe for CI/non-interactive use. If a repo has staged changes before the reset, a warning is noted in the log output.

**Hard reset** — discard all local changes and reset to `origin/<branch>`:
```bash
gb -rh main              # Short form
gb --reset-hard main     # Long form
```
> **Destructive.** Requires an interactive terminal. Before executing, gb scans all repos for dirty state and shows a confirmation prompt listing any repos whose changes will be discarded. Repos that are mid-merge, mid-cherry-pick, or mid-revert are automatically skipped.

**Rebase** — rebase local commits onto `origin/<branch>`:
```bash
gb -rb develop           # Short form
gb --rebase develop      # Long form
```
> **Requires interactive terminal.** If a conflict occurs in any repo, `git rebase --abort` is run automatically to restore a clean state; the repo is reported as failed.

**CI / non-interactive use:**
```bash
# Safe in CI — soft reset does not require a terminal
gb -rs main

# These will exit with an error if stdin is not a TTY:
# gb -rh main
# gb -rb main
```

### Advanced Options

```bash
# Use more workers for faster processing
gb -w 50 -c "status"           # Short form
gb --workers 50 -c "status"    # Long form

# Custom page size for progress display
gb -ps 10 -c "status"          # Show 10 repos per page
gb --size 30 -c "status"       # Show 30 repos per page

# Skip additional directories
gb -s "build,dist,temp" -c "status"        # Short form
gb --skipDirs "build,dist,temp" -c "status"  # Long form

# Include normally skipped directories
gb -i "vendor,node_modules" -l           # Short form
gb --includeDirs "vendor,node_modules" --list  # Long form

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
  -s, --skipDirs string   Comma-separated list of directories to skip
  -i, --includeDirs string
                          Comma-separated list of directories to include
  -rs, --reset-soft string  Soft reset all repos to origin/<branch>
  -rh, --reset-hard string  Hard reset all repos to origin/<branch> (destructive, confirms first)
  -rb, --rebase string      Rebase all repos onto origin/<branch> (confirms first)

Examples:
  gb -c "status"               Execute 'git status' in all repositories
  gb --cmd "fetch origin"      Execute 'git fetch origin' in all repositories
  gb -sh "ls -la"              Execute 'ls -la' shell command in all repositories
  gb --shell "mkdir tmp"       Execute 'mkdir tmp' shell command in all repositories
  gb main                      Switch all repos to main branch
  gb -l                        List all current branches
  gb -w 50 -l                  Fast branch listing with 50 workers
  gb --workers 5 main          Switch with 5 concurrent workers
  gb -i "vendor,dist" 15.0     Include normally skipped directories
  gb -rs main                  Soft reset all repos to origin/main
  gb -rh feature/xyz           Hard reset all repos to origin/feature/xyz (with confirmation)
  gb -rb develop               Rebase all repos onto origin/develop (with confirmation)
```

## How It Works

1. **Discovery**: Recursively scans directories for Git repositories
2. **Filtering**: Applies include/exclude rules to select target repos
3. **Parallel Execution**: Uses worker pools to process multiple repositories simultaneously
4. **Progress Display**: Shows real-time updates with pagination for large sets
5. **Log Capture**: Stores detailed logs for each repository operation
6. **Summary**: Displays success/failure counts with option to review logs

## Git Commands vs Shell Commands

**Git Commands (`-c` / `--cmd`):**
- Executes `git <command>` in each repository
- Example: `gb -c "status"` runs `git status`
- Example: `gb -c "fetch origin"` runs `git fetch origin`
- Use this for git-specific operations

**Shell Commands (`-sh` / `--shell`):**
- Executes raw shell commands in each repository
- Cross-platform support (automatically uses `sh -c` on Unix/Linux/macOS and `cmd /c` on Windows)
- Example: `gb -sh "ls -la"` lists files in each repo directory
- Example: `gb -sh "mkdir tmp"` creates a `tmp` directory in each repo
- Use this for file operations, directory management, or any non-git shell command

## Default Skip Directories

By default, gb skips these directories to improve performance:
- `vendor`, `node_modules`, `.vscode`, `.idea`
- `build`, `dist`, `out`, `target`, `bin`, `obj`
- `.next`, `coverage`, `.nyc_output`
- `__pycache__`, `.pytest_cache`, `.tox`
- `.venv`, `venv`, `.env`, `env`

Use `-i` / `--includeDirs` to include specific directories or `-s` / `--skipDirs` to override the defaults.

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
- **Sync: branch not on origin**: Repo is skipped (not failed); counted separately in summary
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
