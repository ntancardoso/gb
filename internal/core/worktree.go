package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func worktreePath(repoPath, branch string) string {
	parts := strings.Split(branch, "/")
	suffix := parts[len(parts)-1]
	return filepath.Join(filepath.Dir(repoPath), filepath.Base(repoPath)+"-"+suffix)
}

func worktreeListAll(ctx context.Context, root string, workers int, cfg *Config) error {
	repos, _ := discoverRepos(root, workers, cfg, true)
	if repos == nil {
		return nil
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Listing worktrees in %d repos...", len(repos))))

	type wtResult struct {
		relPath string
		output  string
		err     error
	}

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) wtResult {
		out, _, err := executeGitCommandWithRetry(ctx, r.Path, "worktree", "list")
		return wtResult{relPath: r.RelPath, output: string(out), err: err}
	})

	entries := make([]wtResult, 0, len(results))
	for _, res := range results {
		if res.err == nil {
			entries = append(entries, res)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].relPath < entries[j].relPath })

	for _, e := range entries {
		fmt.Printf("%s %s\n", StyleBold.Render("Repo:"), StyleSuccess.Render(e.relPath))
		fmt.Println(StyleDim.Render("-----------------"))
		fmt.Print(e.output)
		fmt.Println(StyleDim.Render("================="))
	}
	return nil
}

func worktreeCreate(ctx context.Context, root, branch, base string, workers int, cfg *Config) error {
	repos, _ := discoverRepos(root, workers, cfg, true)
	if repos == nil {
		return nil
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Creating worktrees for '%s' (base: %s) in %d repos with %d workers...", branch, base, len(repos), min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Creating worktree '%s'", branch), cfg.PageSize)
	stop := progress.start()

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) CommandResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, _ := logManager.CreateLogFile(r.RelPath)
		out := io.Writer(io.Discard)
		if logFile != nil {
			out = logFile
			defer func() { _ = logFile.Close() }()
		}

		wtPath := worktreePath(r.Path, branch)

		if _, statErr := os.Stat(wtPath); statErr == nil {
			_, _ = fmt.Fprintf(out, "worktree already exists at %s\n", wtPath)
			progress.UpdateStatus(r.RelPath, statusSkipped, "worktree already exists")
			return CommandResult{RelPath: r.RelPath, Skipped: true}
		}

		if _, _, refErr := executeGitCommandWithRetry(ctx, r.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); refErr != nil {
			_, _ = fmt.Fprintf(out, "Creating branch '%s' from '%s'...\n", branch, base)
			gitOut, _, bErr := executeGitCommandWithRetry(ctx, r.Path, "branch", branch, base)
			if bErr != nil && !strings.Contains(base, "/") {
				gitOut, _, bErr = executeGitCommandWithRetry(ctx, r.Path, "branch", branch, cfg.Remote+"/"+base)
			}
			if bErr != nil {
				_, _ = fmt.Fprintf(out, "%s", gitOut)
				_, _ = fmt.Fprintf(out, "Failed to create branch: %v\n", bErr)
				progress.UpdateStatus(r.RelPath, statusFailed, fmt.Sprintf("base '%s' not found", base))
				return CommandResult{RelPath: r.RelPath, Error: bErr}
			}
		}

		if mkErr := os.MkdirAll(filepath.Dir(wtPath), 0o755); mkErr != nil {
			progress.UpdateStatus(r.RelPath, statusFailed, "mkdir failed")
			return CommandResult{RelPath: r.RelPath, Error: mkErr}
		}

		gitOut, _, addErr := executeGitCommandWithRetry(ctx, r.Path, "worktree", "add", wtPath, branch)
		if addErr != nil {
			_, _ = fmt.Fprintf(out, "%s", gitOut)
			_, _ = fmt.Fprintf(out, "git worktree add failed: %v\n", addErr)
			progress.UpdateStatus(r.RelPath, statusFailed, "worktree add failed")
			return CommandResult{RelPath: r.RelPath, Error: addErr}
		}

		envSrc := filepath.Join(r.Path, ".env")
		if _, statErr := os.Stat(envSrc); statErr == nil {
			if copyErr := copyFile(envSrc, filepath.Join(wtPath, ".env")); copyErr == nil {
				_, _ = fmt.Fprintln(out, "Copied .env to worktree.")
			}
		}
		_, _ = fmt.Fprintf(out, "Worktree ready: %s\n", wtPath)

		progress.UpdateStatus(r.RelPath, statusCompleted, "")
		return CommandResult{RelPath: r.RelPath}
	})

	success, failed, skipped := 0, 0, 0
	for _, res := range results {
		switch {
		case res.Skipped:
			skipped++
		case res.Error != nil:
			failed++
		default:
			success++
		}
	}

	stop()

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Created worktrees for '%s': %s succeeded, %s skipped, %s failed\n",
		branch,
		StyleSuccess.Render(fmt.Sprintf("%d", success)),
		StyleSkipped.Render(fmt.Sprintf("%d", skipped)),
		StyleFailed.Render(fmt.Sprintf("%d", failed)))

	if PromptViewLogs() {
		DisplayLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
	}

	if failed > 0 {
		return errReposFailed
	}
	return nil
}

func worktreeRemove(ctx context.Context, root, branch string, workers int, cfg *Config) error {
	repos, _ := discoverRepos(root, workers, cfg, true)
	if repos == nil {
		return nil
	}

	isGlob := hasGlobMeta(branch)
	fmt.Println(StyleInfo.Render(fmt.Sprintf("Removing worktrees for '%s' in %d repos with %d workers...", branch, len(repos), min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		return fmt.Errorf("log manager: %w", err)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Removing worktree '%s'", branch), cfg.PageSize)
	stop := progress.start()

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) CommandResult {
		progress.UpdateStatus(r.RelPath, statusProcessing, "")

		logFile, _ := logManager.CreateLogFile(r.RelPath)
		out := io.Writer(io.Discard)
		if logFile != nil {
			out = logFile
			defer func() { _ = logFile.Close() }()
		}

		if isGlob {
			return worktreeRemoveGlob(ctx, r, branch, out, progress)
		}
		return worktreeRemoveExact(ctx, r, branch, out, progress)
	})

	success, failed, skipped := 0, 0, 0
	for _, res := range results {
		switch {
		case res.Skipped:
			skipped++
		case res.Error != nil:
			failed++
		default:
			success++
		}
	}

	stop()

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Removed worktrees for '%s': %s succeeded, %s skipped, %s failed\n",
		branch,
		StyleSuccess.Render(fmt.Sprintf("%d", success)),
		StyleSkipped.Render(fmt.Sprintf("%d", skipped)),
		StyleFailed.Render(fmt.Sprintf("%d", failed)))

	if PromptViewLogs() {
		DisplayLogs(logManager, results)
	} else {
		fmt.Printf("\nLogs are available at: %s\n", logManager.GetTempDir())
	}

	if failed > 0 {
		return errReposFailed
	}
	return nil
}

func worktreeRemoveGlob(ctx context.Context, r RepoInfo, pattern string, out io.Writer, progress *ProgressState) CommandResult {
	porcelain, _, listErr := executeGitCommandWithRetry(ctx, r.Path, "worktree", "list", "--porcelain")
	if listErr != nil {
		_, _ = fmt.Fprintf(out, "git worktree list failed: %v\n", listErr)
		progress.UpdateStatus(r.RelPath, statusFailed, "worktree list failed")
		return CommandResult{RelPath: r.RelPath, Error: listErr}
	}
	pairs := parseWorktreeList(string(porcelain))
	if len(pairs) > 1 {
		pairs = pairs[1:]
	} else {
		pairs = nil
	}

	matched := 0
	var cmdErr error
	for _, pair := range pairs {
		wtBranch, wtPath := pair[0], pair[1]
		ok, _ := path.Match(pattern, wtBranch)
		if !ok {
			continue
		}
		matched++
		gitOut, _, removeErr := executeGitCommandWithRetry(ctx, r.Path, "worktree", "remove", wtPath)
		if removeErr != nil {
			_, _ = fmt.Fprintf(out, "%s", gitOut)
			_, _ = fmt.Fprintf(out, "git worktree remove failed for %s: %v\n", wtPath, removeErr)
			cmdErr = removeErr
			continue
		}
		_, _ = fmt.Fprintf(out, "Removed worktree: %s\n", wtPath)
		_ = os.Remove(filepath.Dir(wtPath))
		_ = os.Remove(filepath.Dir(filepath.Dir(wtPath)))
	}

	if matched == 0 {
		_, _ = fmt.Fprintf(out, "No worktrees matching '%s' found, skipping\n", pattern)
		progress.UpdateStatus(r.RelPath, statusSkipped, "no matching worktrees")
		return CommandResult{RelPath: r.RelPath, Skipped: true}
	}
	if cmdErr != nil {
		progress.UpdateStatus(r.RelPath, statusFailed, "worktree remove failed")
		return CommandResult{RelPath: r.RelPath, Error: cmdErr}
	}
	progress.UpdateStatus(r.RelPath, statusCompleted, "")
	return CommandResult{RelPath: r.RelPath}
}

func worktreeRemoveExact(ctx context.Context, r RepoInfo, branch string, out io.Writer, progress *ProgressState) CommandResult {
	var wtPath string
	if porcelain, _, listErr := executeGitCommandWithRetry(ctx, r.Path, "worktree", "list", "--porcelain"); listErr == nil {
		for _, pair := range parseWorktreeList(string(porcelain)) {
			if pair[0] == branch {
				wtPath = pair[1]
				break
			}
		}
	}
	if wtPath == "" {
		wtPath = worktreePath(r.Path, branch)
		if _, statErr := os.Stat(wtPath); statErr != nil {
			altPath := filepath.Join(filepath.Dir(r.Path), branch)
			if _, altErr := os.Stat(altPath); altErr == nil {
				wtPath = altPath
			}
		}
	}

	if _, statErr := os.Stat(wtPath); statErr != nil {
		_, _ = fmt.Fprintf(out, "No worktree found for branch '%s', skipping\n", branch)
		progress.UpdateStatus(r.RelPath, statusSkipped, "no worktree found")
		return CommandResult{RelPath: r.RelPath, Skipped: true}
	}

	gitOut, _, removeErr := executeGitCommandWithRetry(ctx, r.Path, "worktree", "remove", wtPath)
	if removeErr != nil {
		_, _ = fmt.Fprintf(out, "%s", gitOut)
		_, _ = fmt.Fprintf(out, "git worktree remove failed: %v\n", removeErr)
		progress.UpdateStatus(r.RelPath, statusFailed, "worktree remove failed")
		return CommandResult{RelPath: r.RelPath, Error: removeErr}
	}
	_, _ = fmt.Fprintf(out, "Removed worktree: %s\n", wtPath)
	_ = os.Remove(filepath.Dir(wtPath))
	_ = os.Remove(filepath.Dir(filepath.Dir(wtPath)))
	progress.UpdateStatus(r.RelPath, statusCompleted, "")
	return CommandResult{RelPath: r.RelPath}
}

func worktreeOpen(ctx context.Context, root, branch string, workers int, cfg *Config) error {
	repos, _ := discoverRepos(root, workers, cfg, true)
	if repos == nil {
		return nil
	}

	type pathResult struct {
		relPath string
		wtPath  string
		exists  bool
	}

	results := runPool(ctx, repos, workers, func(ctx context.Context, r RepoInfo) pathResult {
		var wtPath string
		if porcelain, _, listErr := executeGitCommandWithRetry(ctx, r.Path, "worktree", "list", "--porcelain"); listErr == nil {
			for _, pair := range parseWorktreeList(string(porcelain)) {
				if pair[0] == branch {
					wtPath = pair[1]
					break
				}
			}
		}
		if wtPath == "" {
			wtPath = worktreePath(r.Path, branch)
		}
		_, statErr := os.Stat(wtPath)
		return pathResult{relPath: r.RelPath, wtPath: wtPath, exists: statErr == nil}
	})

	sort.Slice(results, func(i, j int) bool { return results[i].relPath < results[j].relPath })

	for _, e := range results {
		if e.exists {
			fmt.Printf("%s %s\n", StyleSuccess.Render(e.relPath+":"), e.wtPath)
		} else {
			fmt.Printf("%s %s\n", StyleDim.Render(e.relPath+":"), StyleDim.Render("(no worktree)"))
		}
	}
	return nil
}

func parseWorktreeList(output string) [][2]string {
	var results [][2]string
	var currentPath, currentBranch string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			currentPath = strings.TrimPrefix(line, "worktree ")
			currentBranch = ""
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			currentBranch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "":
			if currentPath != "" && currentBranch != "" {
				results = append(results, [2]string{currentBranch, currentPath})
			}
			currentPath = ""
			currentBranch = ""
		}
	}
	if currentPath != "" && currentBranch != "" {
		results = append(results, [2]string{currentBranch, currentPath})
	}
	return results
}

func gitMainWorktreePath(repoPath string) string {
	gitPath := filepath.Join(repoPath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil || info.IsDir() {
		return repoPath
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return repoPath
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return repoPath
	}
	gitdir := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(repoPath, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	dir := gitdir
	for {
		if filepath.Base(dir) == ".git" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return repoPath
}

func deduplicateByMainWorktree(repos []RepoInfo) []RepoInfo {
	mainPaths := make(map[string]struct{})
	for _, r := range repos {
		if !r.IsWorktree {
			mainPaths[filepath.Clean(r.Path)] = struct{}{}
		}
	}
	var out []RepoInfo
	for _, r := range repos {
		if !r.IsWorktree {
			out = append(out, r)
			continue
		}
		mainPath := filepath.Clean(gitMainWorktreePath(r.Path))
		if _, present := mainPaths[mainPath]; !present {
			out = append(out, r)
		}
	}
	return out
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err = io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
