// Package core implements git repository discovery and branch switching operations.
package core

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	defaultWorkers = 20
)

var (
	// version is set at build time via ldflags
	// Example: go build -ldflags "-X github.com/ntancardoso/gb/internal/core.version=v1.0.0"
	version = "dev"
)

var (
	defaultSkipDirs = []string{
		"vendor", "node_modules", ".vscode", ".idea", "build", "dist", "out",
		"target", "bin", "obj", ".next", "coverage", ".nyc_output", "__pycache__",
		".pytest_cache", ".tox", ".venv", "venv", ".env", "env",
	}
)

// Config holds runtime configuration for repository operations.
// It manages which directories should be skipped or included during repository scanning.
type Config struct {
	skipSet    map[string]struct{}
	includeSet map[string]struct{}
}

func newConfig(skipDirs, includeDirs []string) *Config {
	cfg := &Config{
		skipSet:    make(map[string]struct{}),
		includeSet: make(map[string]struct{}),
	}

	// Build include set first
	for _, dir := range includeDirs {
		cfg.includeSet[dir] = struct{}{}
	}

	// Build skip set, excluding any included directories
	for _, dir := range skipDirs {
		if _, included := cfg.includeSet[dir]; !included {
			cfg.skipSet[dir] = struct{}{}
		}
	}

	return cfg
}

// Run is the main entry point for the gb CLI tool.
func Run(args []string) error {
	fs := flag.NewFlagSet("gb", flag.ContinueOnError)

	listBranches := fs.Bool("list", false, "List all branches found in repositories")
	fs.BoolVar(listBranches, "l", false, "List all branches (shorthand)")

	runCommand := fs.String("cmd", "", "Execute a git command in all repositories")
	fs.StringVar(runCommand, "c", "", "Execute a git command (shorthand)")

	workers := fs.Int("workers", defaultWorkers, "Number of concurrent workers")
	fs.IntVar(workers, "w", defaultWorkers, "Number of concurrent workers (shorthand)")

	skipDirsFlag := fs.String("skipDirs", "", "Comma-separated list of directories to skip (overrides defaults)")
	fs.StringVar(skipDirsFlag, "s", "", "Directories to skip (shorthand)")

	includeDirsFlag := fs.String("includeDirs", "", "Comma-separated list of directories to include (removes them from skipDirs)")
	fs.StringVar(includeDirsFlag, "i", "", "Directories to include (shorthand)")

	showVersion := fs.Bool("version", false, "Show version information")
	fs.BoolVar(showVersion, "v", false, "Show version (shorthand)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: gb [options] <branch_name>\n\n")
		fmt.Println("Options:")
		fmt.Println("  -h, --help              Show this help message")
		fmt.Println("  -v, --version           Show version information")
		fmt.Println("  -l, --list              List all branches found in repositories")
		fmt.Println("  -c, --cmd string        Execute a git command in all repositories")
		fmt.Println("  -w, --workers int       Number of concurrent workers (default 20)")
		fmt.Println("  -s, --skipDirs string   Comma-separated list of directories to skip")
		fmt.Println("  -i, --includeDirs string")
		fmt.Println("                          Comma-separated list of directories to include")
		fmt.Println("\nExamples:")
		fmt.Println("  gb main                      Switch all repos to main branch")
		fmt.Println("  gb -l                        List all current branches")
		fmt.Println("  gb --list                    List all current branches")
		fmt.Println("  gb -w 50 -l                  Fast branch listing with 50 workers")
		fmt.Println("  gb --workers 5 main          Switch with 5 concurrent workers")
		fmt.Println("  gb -i \"vendor,dist\" 15.0     Include normally skipped directories")
		fmt.Println("  gb -c \"status\"               Execute 'git status' in all repositories")
		fmt.Println("  gb --cmd \"fetch origin\"     Execute 'git fetch origin' in all repositories")
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			// Help was requested, this is not an error
			return nil
		}
		return err
	}

	if *showVersion {
		fmt.Printf("gb version %s\n", version)
		return nil
	}

	skipDirs := parseCommaSeparated(*skipDirsFlag, defaultSkipDirs)
	includeDirs := parseCommaSeparated(*includeDirsFlag, nil)

	cfg := newConfig(skipDirs, includeDirs)

	root, _ := os.Getwd()
	root = resolveRoot(root)

	if *runCommand != "" {
		executeCommandInRepos(root, *runCommand, *workers, cfg)
		return nil
	}

	if *listBranches {
		listAllBranches(root, *workers, cfg)
		return nil
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("branch name required")
	}

	targetBranch := fs.Arg(0)
	switchBranches(root, targetBranch, *workers, cfg)
	return nil
}

func parseCommaSeparated(input string, defaultValue []string) []string {
	if input == "" {
		return defaultValue
	}

	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
