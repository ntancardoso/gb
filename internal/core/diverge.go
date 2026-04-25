package core

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type DivergeResult struct {
	RelPath     string
	Branch      string
	UpstreamRef string
	Ahead       int
	Behind      int
	Success     bool
	Skipped     bool
	SkipReason  string
	Error       string
}

func getTrackingRef(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" || ref == "@{u}" {
		return "", fmt.Errorf("no upstream tracking branch")
	}
	return ref, nil
}

func processSingleDiverge(repo RepoInfo, ref, defaultRemote string) DivergeResult {
	if !checkHasCommits(repo.Path) {
		return DivergeResult{RelPath: repo.RelPath, Skipped: true, SkipReason: "no commits"}
	}

	branch, err := getBranch(repo.Path)
	if err != nil {
		return DivergeResult{RelPath: repo.RelPath, Error: "failed to get branch: " + err.Error()}
	}

	var remoteRef string
	if ref == "" {
		trackingRef, err := getTrackingRef(repo.Path)
		if err != nil {
			return DivergeResult{RelPath: repo.RelPath, Branch: branch, Skipped: true, SkipReason: "no upstream tracking"}
		}
		remoteRef = trackingRef
	} else {
		remote, resolvedBranch := resolveRemoteAndBranch(repo.Path, ref, defaultRemote)
		remoteRef = remote + "/" + resolvedBranch
	}

	verifyCmd := exec.Command("git", "rev-parse", "--verify", remoteRef)
	verifyCmd.Dir = repo.Path
	if verifyCmd.Run() != nil {
		return DivergeResult{RelPath: repo.RelPath, Branch: branch, UpstreamRef: remoteRef, Skipped: true, SkipReason: "remote ref not found"}
	}

	cmd := exec.Command("git", "rev-list", "--left-right", "--count", "HEAD..."+remoteRef)
	cmd.Dir = repo.Path
	out, err := cmd.Output()
	if err != nil {
		return DivergeResult{RelPath: repo.RelPath, Branch: branch, Error: "rev-list failed"}
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return DivergeResult{RelPath: repo.RelPath, Branch: branch, Error: "unexpected output: " + strings.TrimSpace(string(out))}
	}

	ahead, _ := strconv.Atoi(parts[0])
	behind, _ := strconv.Atoi(parts[1])

	return DivergeResult{
		RelPath:     repo.RelPath,
		Branch:      branch,
		UpstreamRef: remoteRef,
		Ahead:       ahead,
		Behind:      behind,
		Success:     true,
	}
}

func checkDiverge(ctx context.Context, root, ref string, workers int, cfg *Config) error {
	repos, total := discoverRepos(root, workers, cfg, false)
	if repos == nil {
		return nil
	}

	trackingMode := ref == ""
	displayRef := ref
	if trackingMode {
		displayRef = "tracked branch"
	} else if !strings.Contains(ref, "/") {
		displayRef = cfg.Remote + "/" + ref
	}

	fmt.Println(StyleInfo.Render(fmt.Sprintf(
		"Found %d repos (filtered from %d discovered), checking divergence vs %s with %d workers...",
		len(repos), total, displayRef, min(workers, len(repos)))))

	results := runPool(ctx, repos, workers, func(_ context.Context, r RepoInfo) DivergeResult {
		return processSingleDiverge(r, ref, cfg.Remote)
	})

	sort.Slice(results, func(i, j int) bool { return results[i].RelPath < results[j].RelPath })

	const noTrackingPlaceholder = "(no tracking)"
	maxPath, maxBranch, maxUpstream := 0, 0, 0
	for _, r := range results {
		if len(r.RelPath) > maxPath {
			maxPath = len(r.RelPath)
		}
		if len(r.Branch) > maxBranch {
			maxBranch = len(r.Branch)
		}
		if trackingMode {
			w := len(r.UpstreamRef)
			if w == 0 {
				w = len(noTrackingPlaceholder)
			}
			if w > maxUpstream {
				maxUpstream = w
			}
		}
	}

	upToDate, changed, failed, skipped := 0, 0, 0, 0
	skipReasons := make(map[string]int)

	for _, r := range results {
		pathCol := fmt.Sprintf("%-*s", maxPath, r.RelPath)
		branchCol := fmt.Sprintf("%-*s", maxBranch, r.Branch)

		var upstreamCol string
		if trackingMode {
			upstream := r.UpstreamRef
			if upstream == "" {
				upstream = noTrackingPlaceholder
			}
			upstreamCol = fmt.Sprintf("  %-*s", maxUpstream, upstream)
		}

		switch {
		case r.Error != "":
			fmt.Printf("%s  %s%s  %s\n", pathCol, branchCol, upstreamCol, StyleFailed.Render(r.Error))
			failed++
		case r.Skipped:
			fmt.Printf("%s  %s%s  %s\n", pathCol, branchCol, upstreamCol, StyleSkipped.Render("✗ "+r.SkipReason))
			skipped++
			skipReasons[r.SkipReason]++
		case r.Ahead == 0 && r.Behind == 0:
			fmt.Printf("%s  %s%s  %s\n", pathCol, branchCol, upstreamCol, StyleSuccess.Render("✓ up to date"))
			upToDate++
		default:
			fmt.Printf("%s  %s%s  %s\n", pathCol, branchCol, upstreamCol, buildDivergeStatus(r.Ahead, r.Behind))
			changed++
		}
	}

	fmt.Println("\n" + StyleBold.Render("--- Summary ---"))
	fmt.Printf("Checked %s in %d repos:\n", StyleBold.Render(displayRef), len(repos))
	fmt.Printf("  %s up to date\n", StyleSuccess.Render(fmt.Sprintf("%d", upToDate)))
	fmt.Printf("  %s with changes\n", StyleInfo.Render(fmt.Sprintf("%d", changed)))
	if failed > 0 {
		fmt.Printf("  %s failed\n", StyleFailed.Render(fmt.Sprintf("%d", failed)))
	}
	if skipped > 0 {
		reasons := make([]string, 0, len(skipReasons))
		for reason := range skipReasons {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		reasonParts := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			reasonParts = append(reasonParts, fmt.Sprintf("%s: %d", reason, skipReasons[reason]))
		}
		fmt.Printf("  %s skipped (%s)\n", StyleSkipped.Render(fmt.Sprintf("%d", skipped)), strings.Join(reasonParts, ", "))
	}

	return nil
}

func buildDivergeStatus(ahead, behind int) string {
	var parts []string
	if ahead > 0 {
		parts = append(parts, StyleInfo.Render(fmt.Sprintf("⬆ %d ahead", ahead)))
	}
	if behind > 0 {
		parts = append(parts, StyleFailed.Render(fmt.Sprintf("⬇ %d behind", behind)))
	}
	return strings.Join(parts, "  ")
}
