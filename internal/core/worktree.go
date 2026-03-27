package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// worktreePath returns the path where a worktree for the given branch should live.
// Convention: <parent-of-repo>/worktrees/<branch>/<repo-basename>
// This is unique per repo even when multiple repos share the same parent directory.
func worktreePath(repoPath, branch string) string {
	return filepath.Join(filepath.Dir(repoPath), "worktrees", branch, filepath.Base(repoPath))
}

func worktreeListAll(root string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	repos = cfg.filterWorktrees(repos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Listing worktrees in %d repos...", len(repos))))

	type wtResult struct {
		relPath string
		output  string
		err     error
	}

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan wtResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				out, _, err := executeGitCommandWithRetry(context.Background(), r.Path, "worktree", "list")
				resCh <- wtResult{relPath: r.RelPath, output: string(out), err: err}
			}
		}()
	}
	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)
	go func() { wg.Wait(); close(resCh) }()

	type entry struct {
		relPath string
		output  string
	}
	var entries []entry
	for res := range resCh {
		if res.err == nil {
			entries = append(entries, entry{res.relPath, res.output})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].relPath < entries[j].relPath })

	for _, e := range entries {
		fmt.Printf("%s %s\n", StyleBold.Render("Repo:"), StyleSuccess.Render(e.relPath))
		fmt.Println(StyleDim.Render("-----------------"))
		fmt.Print(e.output)
		fmt.Println(StyleDim.Render("================="))
	}
}

func worktreeCreate(root, branch, base string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	repos = cfg.filterWorktrees(repos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Creating worktrees for '%s' (base: %s) in %d repos with %d workers...", branch, base, len(repos), min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log manager: %s\n", err)
		os.Exit(1)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Creating worktree '%s'", branch), cfg.PageSize)
	progress.render()
	progress.StartInput()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				progress.UpdateStatus(r.RelPath, statusProcessing, "")

				logFile, _ := logManager.CreateLogFile(r.RelPath)
				out := io.Discard
				if logFile != nil {
					out = logFile
				}

				wtPath := worktreePath(r.Path, branch)
				var cmdErr error

				if _, statErr := os.Stat(wtPath); statErr == nil {
					msg := fmt.Sprintf("worktree already exists at %s", wtPath)
					_, _ = fmt.Fprintln(out, msg)
					progress.UpdateStatus(r.RelPath, statusSkipped, "worktree already exists")
					if logFile != nil {
						_ = logFile.Close()
					}
					resCh <- CommandResult{RelPath: r.RelPath}
					continue
				}

				// Create branch from base if it doesn't exist locally.
				if _, _, err2 := executeGitCommandWithRetry(context.Background(), r.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err2 != nil {
					_, _ = fmt.Fprintf(out, "Creating branch '%s' from '%s'...\n", branch, base)
					if _, _, err3 := executeGitCommandWithRetry(context.Background(), r.Path, "branch", branch, base); err3 != nil {
						_, _ = fmt.Fprintf(out, "Failed to create branch: %v\n", err3)
						progress.UpdateStatus(r.RelPath, statusFailed, "branch creation failed")
						if logFile != nil {
							_ = logFile.Close()
						}
						resCh <- CommandResult{RelPath: r.RelPath, Error: err3}
						continue
					}
				}

				if err3 := os.MkdirAll(filepath.Dir(wtPath), 0o755); err3 != nil {
					progress.UpdateStatus(r.RelPath, statusFailed, "mkdir failed")
					if logFile != nil {
						_ = logFile.Close()
					}
					resCh <- CommandResult{RelPath: r.RelPath, Error: err3}
					continue
				}

				if _, _, err3 := executeGitCommandWithRetry(context.Background(), r.Path, "worktree", "add", wtPath, branch); err3 != nil {
					cmdErr = err3
					_, _ = fmt.Fprintf(out, "git worktree add failed: %v\n", err3)
				} else {
					// Copy .env if present.
					envSrc := filepath.Join(r.Path, ".env")
					if _, statErr := os.Stat(envSrc); statErr == nil {
						envDst := filepath.Join(wtPath, ".env")
						if copyErr := copyFile(envSrc, envDst); copyErr == nil {
							_, _ = fmt.Fprintf(out, "Copied .env to worktree.\n")
						}
					}
					_, _ = fmt.Fprintf(out, "Worktree ready: %s\n", wtPath)
				}

				if logFile != nil {
					_ = logFile.Close()
				}

				if cmdErr != nil {
					progress.UpdateStatus(r.RelPath, statusFailed, "worktree add failed")
				} else {
					progress.UpdateStatus(r.RelPath, statusCompleted, "")
				}
				resCh <- CommandResult{RelPath: r.RelPath, Error: cmdErr}
			}
		}()
	}
	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)
	go func() { wg.Wait(); close(resCh) }()

	var results []CommandResult
	success, failed, skipped := 0, 0, 0
	for res := range resCh {
		results = append(results, res)
		if res.Skipped {
			skipped++
		} else if res.Error != nil {
			failed++
		} else {
			success++
		}
	}

	progress.RenderFinal()
	progress.StopInput()

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
		os.Exit(1)
	}
}

func worktreeRemove(root, branch string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	repos = cfg.filterWorktrees(repos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf("Removing worktrees for '%s' in %d repos with %d workers...", branch, len(repos), min(workers, len(repos)))))

	logManager, err := NewLogManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log manager: %s\n", err)
		os.Exit(1)
	}

	progress := NewProgressState(repos, fmt.Sprintf("Removing worktree '%s'", branch), cfg.PageSize)
	progress.render()
	progress.StartInput()

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan CommandResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				progress.UpdateStatus(r.RelPath, statusProcessing, "")

				logFile, _ := logManager.CreateLogFile(r.RelPath)
				out := io.Discard
				if logFile != nil {
					out = logFile
				}

				wtPath := worktreePath(r.Path, branch)

				if _, statErr := os.Stat(wtPath); statErr != nil {
					_, _ = fmt.Fprintf(out, "No worktree found at %s, skipping\n", wtPath)
					progress.UpdateStatus(r.RelPath, statusSkipped, "no worktree found")
					if logFile != nil {
						_ = logFile.Close()
					}
					resCh <- CommandResult{RelPath: r.RelPath}
					continue
				}

				_, _, cmdErr := executeGitCommandWithRetry(context.Background(), r.Path, "worktree", "remove", wtPath)
				if cmdErr != nil {
					_, _ = fmt.Fprintf(out, "git worktree remove failed: %v\n", cmdErr)
				} else {
					_, _ = fmt.Fprintf(out, "Removed worktree: %s\n", wtPath)
					// Clean up empty parent dirs (best-effort).
					_ = os.Remove(filepath.Dir(wtPath))
					_ = os.Remove(filepath.Dir(filepath.Dir(wtPath)))
				}

				if logFile != nil {
					_ = logFile.Close()
				}

				if cmdErr != nil {
					progress.UpdateStatus(r.RelPath, statusFailed, "worktree remove failed")
				} else {
					progress.UpdateStatus(r.RelPath, statusCompleted, "")
				}
				resCh <- CommandResult{RelPath: r.RelPath, Error: cmdErr}
			}
		}()
	}
	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)
	go func() { wg.Wait(); close(resCh) }()

	var results []CommandResult
	success, failed, skipped := 0, 0, 0
	for res := range resCh {
		results = append(results, res)
		if res.Skipped {
			skipped++
		} else if res.Error != nil {
			failed++
		} else {
			success++
		}
	}

	progress.RenderFinal()
	progress.StopInput()

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
		os.Exit(1)
	}
}

func worktreeOpen(root, branch string, workers int, cfg *Config) {
	fmt.Printf("Discovering repos in %s...\n", root)
	allRepos, err := findGitRepos(root, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(allRepos) == 0 {
		fmt.Println("No repos found")
		return
	}

	repos := cfg.filterReposForExecution(allRepos)
	repos = cfg.filterWorktrees(repos)
	if len(repos) == 0 {
		fmt.Println("No repos match the specified include/exclude criteria")
		return
	}

	type pathResult struct {
		relPath string
		wtPath  string
		exists  bool
	}

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan pathResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range repoCh {
				wtPath := worktreePath(r.Path, branch)
				_, statErr := os.Stat(wtPath)
				resCh <- pathResult{relPath: r.RelPath, wtPath: wtPath, exists: statErr == nil}
			}
		}()
	}
	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)
	go func() { wg.Wait(); close(resCh) }()

	var entries []pathResult
	for res := range resCh {
		entries = append(entries, res)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].relPath < entries[j].relPath })

	for _, e := range entries {
		if e.exists {
			fmt.Printf("%s %s\n", StyleSuccess.Render(e.relPath+":"), e.wtPath)
		} else {
			fmt.Printf("%s %s\n", StyleDim.Render(e.relPath+":"), StyleDim.Render("(no worktree)"))
		}
	}
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
