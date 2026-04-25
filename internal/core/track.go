package core

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type TrackResult struct {
	RelPath  string
	Branch   string
	Upstream string
	Error    string
}

func processSingleTrack(repo RepoInfo) TrackResult {
	branch, err := getBranch(repo.Path)
	if err != nil {
		return TrackResult{RelPath: repo.RelPath, Error: "failed to get branch: " + err.Error()}
	}

	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Dir = repo.Path
	out, err := cmd.Output()
	if err != nil {
		return TrackResult{RelPath: repo.RelPath, Branch: branch, Upstream: "(none)"}
	}

	upstream := strings.TrimSpace(string(out))
	if upstream == "" || upstream == "@{u}" {
		upstream = "(none)"
	}

	return TrackResult{RelPath: repo.RelPath, Branch: branch, Upstream: upstream}
}

func checkTrack(ctx context.Context, root string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf(
		"Found %d repos (filtered from %d discovered), checking upstream tracking with %d workers...",
		len(repos), total, min(workers, len(repos)))))

	results := runPool(ctx, repos, workers, func(_ context.Context, r RepoInfo) TrackResult {
		return processSingleTrack(r)
	})

	sort.Slice(results, func(i, j int) bool { return results[i].RelPath < results[j].RelPath })

	maxPath, maxBranch := 0, 0
	for _, r := range results {
		if len(r.RelPath) > maxPath {
			maxPath = len(r.RelPath)
		}
		if len(r.Branch) > maxBranch {
			maxBranch = len(r.Branch)
		}
	}

	tracking, untracked := 0, 0
	for _, r := range results {
		pathCol := fmt.Sprintf("%-*s", maxPath, r.RelPath)
		branchCol := fmt.Sprintf("%-*s", maxBranch, r.Branch)

		if r.Error != "" {
			fmt.Printf("%s  %s  → %s\n", pathCol, branchCol, StyleFailed.Render(r.Error))
			continue
		}

		if r.Upstream == "(none)" {
			fmt.Printf("%s  %s  → %s\n", pathCol, branchCol, StyleSkipped.Render("(none)"))
			untracked++
		} else {
			fmt.Printf("%s  %s  → %s\n", pathCol, branchCol, StyleSuccess.Render(r.Upstream))
			tracking++
		}
	}

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("%d repos: %s tracking a remote branch, %s untracked\n",
		len(repos),
		StyleSuccess.Render(fmt.Sprintf("%d", tracking)),
		StyleSkipped.Render(fmt.Sprintf("%d", untracked)))

	return nil
}
