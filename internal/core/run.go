package core

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultWorkers  = 20
	defaultPageSize = 20
)

var version = "dev"

var (
	defaultExcludeDirs = []string{
		"vendor", "node_modules", ".vscode", ".idea", "build", "dist", "out",
		"target", "bin", "obj", ".next", "coverage", ".nyc_output", "__pycache__",
		".pytest_cache", ".tox", ".venv", "venv", ".env", "env",
	}
)

type Config struct {
	excludeSet       map[string]struct{}
	includeSet       map[string]struct{}
	excludeBranchSet map[string]struct{}
	includeBranchSet map[string]struct{}
	PageSize         int
}

func newConfig(excludeDirs, includeDirs, excludeBranches, includeBranches []string, pageSize int) *Config {
	cfg := &Config{
		excludeSet:       make(map[string]struct{}),
		includeSet:       make(map[string]struct{}),
		excludeBranchSet: make(map[string]struct{}),
		includeBranchSet: make(map[string]struct{}),
		PageSize:         pageSize,
	}

	for _, dir := range includeDirs {
		cfg.includeSet[dir] = struct{}{}
	}

	for _, dir := range excludeDirs {
		if _, included := cfg.includeSet[dir]; !included {
			cfg.excludeSet[dir] = struct{}{}
		}
	}

	for _, b := range includeBranches {
		cfg.includeBranchSet[b] = struct{}{}
	}

	for _, b := range excludeBranches {
		if _, included := cfg.includeBranchSet[b]; !included {
			cfg.excludeBranchSet[b] = struct{}{}
		}
	}

	for _, b := range includeBranches {
		cfg.includeBranchSet[b] = struct{}{}
	}

	for _, b := range skipBranches {
		if _, included := cfg.includeBranchSet[b]; !included {
			cfg.skipBranchSet[b] = struct{}{}
		}
	}

	return cfg
}

func (cfg *Config) shouldExecuteInBranch(branch string) bool {
	if len(cfg.includeBranchSet) > 0 {
		_, ok := cfg.includeBranchSet[branch]
		return ok
	}
	if len(cfg.excludeBranchSet) > 0 {
		_, ok := cfg.excludeBranchSet[branch]
		return !ok
	}
	return true
}

func (cfg *Config) shouldExecuteInRepo(relPath string) bool {
	if len(cfg.includeSet) > 0 {
		return cfg.containsPath(cfg.includeSet, relPath)
	}

	if len(cfg.excludeSet) > 0 {
		return !cfg.containsPath(cfg.excludeSet, relPath)
	}

	return true
}

func (cfg *Config) containsPath(set map[string]struct{}, relPath string) bool {
	cleanPath := filepath.Clean(relPath)

	if _, exists := set[cleanPath]; exists {
		return true
	}

	for dir := range set {
		if cfg.isParentPath(dir, cleanPath) {
			return true
		}
	}

	return false
}

func (cfg *Config) isParentPath(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)

	if parent == child {
		return true
	}

	parentWithSep := parent + string(filepath.Separator)
	childWithSep := child + string(filepath.Separator)

	return strings.HasPrefix(childWithSep, parentWithSep)
}

func (cfg *Config) filterReposForExecution(repos []RepoInfo) []RepoInfo {
	if len(cfg.includeSet) == 0 && len(cfg.excludeSet) == 0 {
		return repos
	}

	var filtered []RepoInfo
	for _, repo := range repos {
		if cfg.shouldExecuteInRepo(repo.RelPath) {
			filtered = append(filtered, repo)
		}
	}

	return filtered
}

func reorderArgs(args []string) []string {
	var flags []string
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			boolFlags := map[string]bool{
				"-l": true, "--list": true,
				"-v": true, "--version": true,
				"-h": true, "--help": true,
			}
			if !boolFlags[arg] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, arg)
		}
		i++
	}

	return append(flags, positional...)
}

func Run(args []string) error {
	args = reorderArgs(args)

	fs := flag.NewFlagSet("gb", flag.ContinueOnError)

	listBranches := fs.Bool("list", false, "List all branches found in repositories")
	fs.BoolVar(listBranches, "l", false, "List all branches (shorthand)")

	runCommand := fs.String("cmd", "", "Execute a git command in all repositories")
	fs.StringVar(runCommand, "c", "", "Execute a git command (shorthand)")

	runShell := fs.String("shell", "", "Execute a shell command in all repositories")
	fs.StringVar(runShell, "sh", "", "Execute a shell command (shorthand)")

	workers := fs.Int("workers", defaultWorkers, "Number of concurrent workers")
	fs.IntVar(workers, "w", defaultWorkers, "Number of concurrent workers (shorthand)")

	excludeDirsFlag := fs.String("excludeDirs", "", "Comma-separated list of directories to exclude from command execution")
	fs.StringVar(excludeDirsFlag, "e", "", "Directories to exclude from execution (shorthand)")

	includeDirsFlag := fs.String("includeDirs", "", "Comma-separated list of directories to include in command execution (only execute in these directories)")
	fs.StringVar(includeDirsFlag, "i", "", "Directories to include in execution (shorthand)")

	pageSize := fs.Int("size", defaultPageSize, "Number of repos to display per page")
	fs.IntVar(pageSize, "ps", defaultPageSize, "Repos per page (shorthand)")

	excludeBranchesFlag := fs.String("excludeBranches", "", "Comma-separated branch names to exclude")
	fs.StringVar(excludeBranchesFlag, "eb", "", "Branch names to exclude (shorthand)")

	includeBranchesFlag := fs.String("includeBranches", "", "Comma-separated branch names to include (only these)")
	fs.StringVar(includeBranchesFlag, "ib", "", "Branch names to include (shorthand)")

	showVersion := fs.Bool("version", false, "Show version information")
	fs.BoolVar(showVersion, "v", false, "Show version (shorthand)")

	resetSoft := fs.String("reset-soft", "", "Soft reset all repos to origin/<branch>")
	fs.StringVar(resetSoft, "rs", "", "Soft reset to origin/<branch> (shorthand)")

	resetHard := fs.String("reset-hard", "", "Hard reset all repos to origin/<branch> (destructive, confirms first)")
	fs.StringVar(resetHard, "rh", "", "Hard reset to origin/<branch> (shorthand)")

	rebaseBranch := fs.String("rebase", "", "Rebase all repos onto origin/<branch> (confirms first)")
	fs.StringVar(rebaseBranch, "rb", "", "Rebase onto origin/<branch> (shorthand)")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: gb [options] <branch_name>\n\n")
		fmt.Println("Options:")
		fmt.Println("  -h, --help              Show this help message")
		fmt.Println("  -v, --version           Show version information")
		fmt.Println("  -l, --list              List all branches found in repositories")
		fmt.Println("  -c, --cmd string        Execute a git command in all repositories")
		fmt.Println("  -sh, --shell string     Execute a shell command in all repositories")
		fmt.Println("  -w, --workers int       Number of concurrent workers (default 20)")
		fmt.Println("  -ps, --size int         Number of repos to display per page (default 20)")
		fmt.Println("  -e, --excludeDirs string   Comma-separated list of directories to exclude from execution")
		fmt.Println("  -i, --includeDirs string")
		fmt.Println("                          Comma-separated list of directories to include in execution")
		fmt.Println("  -rs, --reset-soft string  Soft reset all repos to origin/<branch>")
		fmt.Println("  -rh, --reset-hard string  Hard reset all repos to origin/<branch> (destructive, confirms first)")
		fmt.Println("  -rb, --rebase string      Rebase all repos onto origin/<branch> (confirms first)")
		fmt.Println("  -ib, --includeBranches string")
		fmt.Println("                            Only operate on repos currently on these branches (comma-separated)")
		fmt.Println("  -eb, --excludeBranches string")
		fmt.Println("                            Exclude repos currently on these branches (comma-separated)")
		fmt.Println("\nExamples:")
		fmt.Println("  gb main                      Switch all repos to main branch")
		fmt.Println("  gb -l                        List all current branches")
		fmt.Println("  gb --list                    List all current branches")
		fmt.Println("  gb -w 50 -l                  Fast branch listing with 50 workers")
		fmt.Println("  gb --workers 5 main          Switch with 5 concurrent workers")
		fmt.Println("  gb -i \"vendor,custom\" 15.0   Execute only in vendor and custom directories")
		fmt.Println("  gb -e \"build,temp\" -l        List branches, excluding build and temp directories")
		fmt.Println("  gb -c \"status\"               Execute 'git status' in all repositories")
		fmt.Println("  gb -c \"status\" -i \"abc,def\"  Execute 'git status' only in abc and def directories")
		fmt.Println("  gb --cmd \"fetch origin\"     Execute 'git fetch origin' in all repositories")
		fmt.Println("  gb -sh \"ls -la\"              Execute 'ls -la' shell command in all repositories")
		fmt.Println("  gb -sh \"pwd\" -i \"vendor\"     Execute 'pwd' only in vendor directory")
		fmt.Println("  gb --shell \"mkdir tmp\"      Execute 'mkdir tmp' shell command in all repositories")
		fmt.Println("  gb -rs main              Soft reset all repos to origin/main")
		fmt.Println("  gb -rh feature/xyz       Hard reset all repos to origin/feature/xyz (with confirmation)")
		fmt.Println("  gb -rb develop           Rebase all repos onto origin/develop (with confirmation)")
		fmt.Println("  gb -ib main -l           List branches, only repos currently on main")
		fmt.Println("  gb -eb main -c \"fetch origin\"  Fetch in all repos except those on main")
		fmt.Println("  gb -ib develop -c \"status\"     Git status only in repos on develop")
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if *showVersion {
		fmt.Println(StyleBold.Render(fmt.Sprintf("gb version %s", version)))
		return nil
	}

	excludeDirs := parseCommaSeparated(*excludeDirsFlag, defaultExcludeDirs)
	includeDirs := parseCommaSeparated(*includeDirsFlag, nil)
	excludeBranches := parseCommaSeparated(*excludeBranchesFlag, nil)
	includeBranches := parseCommaSeparated(*includeBranchesFlag, nil)

	cfg := newConfig(excludeDirs, includeDirs, excludeBranches, includeBranches, *pageSize)

	root, _ := os.Getwd()
	root = resolveRoot(root)

	if *runCommand != "" {
		executeCommandInRepos(root, *runCommand, *workers, cfg)
		return nil
	}

	if *runShell != "" {
		executeShellInRepos(root, *runShell, *workers, cfg)
		return nil
	}

	if *listBranches {
		listAllBranches(root, *workers, cfg)
		return nil
	}

	if *resetSoft != "" {
		syncBranch(root, *resetSoft, "soft", *workers, cfg)
		return nil
	}

	if *resetHard != "" {
		syncBranch(root, *resetHard, "hard", *workers, cfg)
		return nil
	}

	if *rebaseBranch != "" {
		syncBranch(root, *rebaseBranch, "rebase", *workers, cfg)
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
