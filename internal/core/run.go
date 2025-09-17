package core

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

var (
	defaultWorkers = 20

	// default dirs to skip
	skipDirs = []string{
		"vendor", "node_modules", ".vscode", ".idea", "build", "dist", "out",
		"target", "bin", "obj", ".next", "coverage", ".nyc_output", "__pycache__",
		".pytest_cache", ".tox", ".venv", "venv", ".env", "env",
	}

	skipSet    map[string]struct{}
	includeSet map[string]struct{}
)

func Run(args []string) error {
	fs := flag.NewFlagSet("gb", flag.ContinueOnError)

	listBranches := fs.Bool("list", false, "List all branches found in repositories")
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
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Parse skipDirs
	if *skipDirsFlag != "" {
		skipDirs = strings.Split(*skipDirsFlag, ",")
		for i := range skipDirs {
			skipDirs[i] = strings.TrimSpace(skipDirs[i])
		}
	}

	// Parse includeDirs
	if *includeDirsFlag != "" {
		incs := strings.Split(*includeDirsFlag, ",")
		for i := range incs {
			incs[i] = strings.TrimSpace(incs[i])
		}
		includeSet = make(map[string]struct{}, len(incs))
		for _, d := range incs {
			includeSet[d] = struct{}{}
		}
		// filter skipDirs
		tmp := make(map[string]bool)
		for _, s := range skipDirs {
			tmp[s] = true
		}
		for _, inc := range incs {
			delete(tmp, inc)
		}
		filtered := []string{}
		for s := range tmp {
			filtered = append(filtered, s)
		}
		skipDirs = filtered
	}

	skipSet = buildSkipSet(skipDirs)

	// Resolve root with symlink/junction handling
	root, _ := os.Getwd()
	root = resolveRoot(root)

	if *listBranches {
		listAllBranches(root, *workers)
		return nil
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("branch name required")
	}

	targetBranch := fs.Arg(0)
	switchBranches(root, targetBranch, *workers)
	return nil
}
