# Git Branch Switcher (gb)

A specialized CLI tool for maintaining consistent branch versions across Odoo projects and OCA modules.

## Primary Use Case

**Odoo Version Synchronization**  
Designed specifically for Odoo developers to:
- Maintain identical versions across core Odoo and all OCA modules
- Switch all related projects to the same version branch (e.g., 15.0, 16.0)
- Manage branches in complex Odoo project structures like:
```
/odoo-projects/
├── odoo/ # Core Odoo
├── design-themes/ # Themes
├── OCA/
│ ├── project/ # OCA project module
│ ├── survey/ # OCA survey module
│ └── ... # Other OCA modules
└── custom/ # Custom modules
```

## Features

- Recursively switches branches through Odoo project structures
- Handles both Odoo core and OCA module repositories
- Smart branch detection (local → remote fallback)
- Preserves branch tracking relationships
- Skip non-relevant directories (`vendor/`, `node_modules/`)

## Installation

```bash
go build -o gb main.go
sudo mv gb /usr/local/bin/  # Linux/macOS
# On windows use go build -o gb.exe # and add to env PATH
```

## Usage
```bash
gb 15.0 
```

## Typical Odoo Workflow
1. Navigate to your Odoo projects folder:
```bash
cd ~/odoo-projects
```

2. Synchronize all repositories to version 15.0:
```bash
gb 15.0
```

# Requirements
- Odoo project structure with Git repositories
- Git installed
- Go 1.16+ (for building)