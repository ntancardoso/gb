# Git Branch Switcher (gb)

A specialized CLI tool for maintaining consistent branch versions across multiple Git repositories, designed primarily for Odoo projects and OCA modules.

## Primary Use Case

**Odoo Version Synchronization**  
Designed specifically for Odoo developers to:
- Maintain identical versions across core Odoo and all OCA modules
- Switch all related projects to the same version branch (e.g., 15.0, 16.0, 17.0)
- Manage branches in complex Odoo project structures like:
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

## Features

- **Recursive Discovery**: Automatically finds all Git repositories in subdirectories
- **Concurrent Processing**: Uses configurable worker pools for fast branch switching
- **Smart Branch Detection**: Handles local branches, remote tracking, and shallow repositories
- **Flexible Filtering**: Skip or include specific directories with customizable rules
- **Branch Listing**: View all current branches across repositories
- **Command Execution**: Execute arbitrary git commands across all repositories
- **Detailed Reporting**: Shows progress and summarizes results
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

**Execute a git command in all repositories:**
```bash
gb -c "status"           # Short form
gb --cmd "fetch origin"  # Long form
gb -c "pull"
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

### Advanced Options

```bash
# Use more workers for faster processing
gb -w 50 15.0              # Short form
gb --workers 50 15.0       # Long form

# Skip additional directories
gb -s "build,dist,temp" 15.0        # Short form
gb --skipDirs "build,dist,temp" 15.0  # Long form

# Include normally skipped directories
gb -i "vendor,node_modules" -l           # Short form
gb --includeDirs "vendor,node_modules" --list  # Long form

# Combine options (mix short and long forms)
gb -w 10 --includeDirs "custom-vendor" 16.0
gb --workers 10 -i "custom-vendor" 16.0

# Execute command with custom workers count
gb -c "status" -w 30
gb --cmd "status" --workers 30
```

### Full Command Reference

```
Usage: gb [options] <branch_name>

Options:
  -h, --help              Show this help message
  -v, --version           Show version information
  -l, --list              List all branches found in repositories
  -c, --cmd string        Execute a git command in all repositories
  -w, --workers int       Number of concurrent workers (default 20)
  -s, --skipDirs string   Comma-separated list of directories to skip
  -i, --includeDirs string
                          Comma-separated list of directories to include

Examples:
  gb main                      Switch all repos to main branch
  gb -l                        List all current branches
  gb --list                    List all current branches
  gb -w 50 -l                  Fast branch listing with 50 workers
  gb --workers 5 main          Switch with 5 concurrent workers
  gb -i "vendor,dist" 15.0     Include normally skipped directories
  gb -c "status"               Execute 'git status' in all repositories
  gb --cmd "fetch origin"      Execute 'git fetch origin' in all repositories
```

## Typical Odoo Workflow

1. **Navigate to your Odoo projects folder:**
```bash
cd ~/odoo-projects
```

2. **Check current branch status:**
```bash
gb -l
# or
gb --list
```

3. **Synchronize all repositories to version 15.0:**
```bash
gb 15.0
```

4. **Switch to development branch with progress monitoring:**
```bash
gb -w 10 development
# or
gb --workers 10 development
```

5. **Execute git commands across all repositories:**
```bash
gb -c "status"
gb --cmd "fetch origin"
```

## How It Works

1. **Discovery Phase**: Recursively scans directories for Git repositories
2. **Branch Analysis**: Checks if target branch exists locally or remotely  
3. **Concurrent Switching**: Uses worker pools to process multiple repositories simultaneously
4. **Smart Fallbacks**: Automatically handles:
   - Creating tracking branches from remote
   - Fetching missing branches (with shallow repository support)
   - Branch creation and checkout operations
5. **Progress Reporting**: Real-time updates and final summary

## Default Skip Directories

By default, gb skips these directories to improve performance:
- `vendor`, `node_modules`, `.vscode`, `.idea`
- `build`, `dist`, `out`, `target`, `bin`, `obj`
- `.next`, `coverage`, `.nyc_output`
- `__pycache__`, `.pytest_cache`, `.tox`
- `.venv`, `venv`, `.env`, `env`

Use `-i` / `--includeDirs` to include specific directories or `-s` / `--skipDirs` to override the defaults.

## Error Handling

- **Branch not found**: Reports repositories where the target branch doesn't exist
- **Switch failures**: Shows detailed error messages for failed operations
- **Permission issues**: Handles repository access problems gracefully
- **Network issues**: Manages remote fetch failures appropriately
- **Command execution failures**: Shows detailed error messages for failed git commands

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
2. Create your feature branch (`gb feature-branch`)
3. Run tests: `go test ./...`
4. Commit your changes
5. Push to the branch
6. Create a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.