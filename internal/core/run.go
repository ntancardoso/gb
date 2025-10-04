// Package core implements the core functionality for the gb (Git Branch Switcher) tool.
//
// This package provides functions for discovering Git repositories, switching branches
// across multiple repositories concurrently, and executing git commands in bulk.
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
	// defaultSkipDirs contains directories commonly excluded from repository scanning
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

// newConfig creates a new Config with the specified skip and include directories.
// Directories in includeDirs take precedence over those in skipDirs.
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
// It parses command-line arguments and executes the appropriate operation:
// - Branch listing (-list flag)
// - Command execution (-c flag)
// - Branch switching (default with branch name argument)
func Run(args []string) error {
	fs := flag.NewFlagSet("gb", flag.ContinueOnError)

	listBranches := fs.Bool("list", false, "List all branches found in repositories")
	runCommand := fs.String("c", "", "Execute a git command in all repositories")
	workers := fs.Int("workers", defaultWorkers, "Number of concurrent workers")
	skipDirsFlag := fs.String("skipDirs", "", "Comma-separated list of directories to skip (overrides defaults)")
	includeDirsFlag := fs.String("includeDirs", "", "Comma-separated list of directories to include (removes them from skipDirs)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: gb [options] <branch_name>\n")
		fmt.Println("Options:")
		fs.PrintDefaults()
		fmt.Println("Examples:")
		fmt.Println("  gb main")
		fmt.Println("  gb -list")
		fmt.Println("  gb -workers 50 -list")
		fmt.Println("  gb -workers 5 main")
		fmt.Println("  gb -list -includeDirs \"dir1,dir2\"")
		fmt.Println("  gb -c \"status\"")
		fmt.Println("  gb -c \"fetch origin\"")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Parse directory filters
	skipDirs := parseCommaSeparated(*skipDirsFlag, defaultSkipDirs)
	includeDirs := parseCommaSeparated(*includeDirsFlag, nil)

	// Create configuration
	cfg := newConfig(skipDirs, includeDirs)

	// Resolve root with symlink/junction handling
	root, _ := os.Getwd()
	root = resolveRoot(root)

	// Handle the new command execution feature
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

// parseCommaSeparated splits a comma-separated string and trims whitespace.
// Returns defaultValue if input is empty.
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
