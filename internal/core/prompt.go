package core

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func PromptViewLogs() bool {
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	if (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		return false
	}

	fmt.Print("\nView detailed logs? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

type logEntry struct {
	relPath    string
	failed     bool
	skipped    bool
	skipReason string
	label      string
}

func displayLogEntries(logManager *LogManager, entries []logEntry) {
	fmt.Println("\n" + StyleBold.Render("=== Detailed Logs ==="))
	fmt.Println()

	allLogs := logManager.GetAllLogPaths()

	fmt.Println(StyleFailed.Render("--- Failed Repositories ---"))
	hasFailed := false
	for _, e := range entries {
		if e.failed {
			hasFailed = true
			displayRepoLog(logManager, e.relPath, StyleFailed.Render("FAILED"))
		}
	}
	if !hasFailed {
		fmt.Println(StyleDim.Render("(none)"))
	}

	hasSkipped := false
	for _, e := range entries {
		if e.skipped {
			hasSkipped = true
			break
		}
	}
	if hasSkipped {
		fmt.Println("\n" + StyleSkipped.Render("--- Skipped Repositories ---"))
		for _, e := range entries {
			if e.skipped {
				fmt.Printf("  [SKIPPED] %s - %s\n", e.relPath, e.skipReason)
			}
		}
	}

	fmt.Println("\n" + StyleSuccess.Render("--- Successful Repositories ---"))
	hasSuccess := false
	for _, e := range entries {
		if !e.failed && !e.skipped {
			hasSuccess = true
			label := e.label
			if label == "" {
				label = StyleSuccess.Render("SUCCESS")
			}
			displayRepoLog(logManager, e.relPath, label)
		}
	}
	if !hasSuccess {
		fmt.Println(StyleDim.Render("(none)"))
	}

	fmt.Println("\n" + StyleBold.Render("--- Log Location ---"))
	fmt.Printf("Logs are stored in: %s\n", StyleDim.Render(logManager.GetTempDir()))
	fmt.Printf("Total log files: %s\n", StyleDim.Render(fmt.Sprintf("%d", len(allLogs))))
}

func DisplayLogs(logManager *LogManager, results []CommandResult) {
	entries := make([]logEntry, len(results))
	for i, res := range results {
		entries[i] = logEntry{
			relPath: res.RelPath,
			failed:  res.Error != nil,
		}
	}
	displayLogEntries(logManager, entries)
}

func DisplayResetLogs(logManager *LogManager, results []ResetResult) {
	entries := make([]logEntry, len(results))
	for i, res := range results {
		e := logEntry{
			relPath:    res.RelPath,
			failed:     !res.Success && !res.Skipped,
			skipped:    res.Skipped,
			skipReason: res.SkipReason,
		}
		if res.Success && res.Warning != "" {
			e.label = StyleSuccess.Render(fmt.Sprintf("SUCCESS (warning: %s)", res.Warning))
		}
		entries[i] = e
	}
	displayLogEntries(logManager, entries)
}

func DisplaySwitchLogs(logManager *LogManager, results []SwitchResult) {
	entries := make([]logEntry, len(results))
	for i, res := range results {
		entries[i] = logEntry{
			relPath:    res.RelPath,
			failed:     !res.Success && !res.Skipped,
			skipped:    res.Skipped,
			skipReason: res.Error,
		}
	}
	displayLogEntries(logManager, entries)
}

func displayRepoLog(logManager *LogManager, relPath, status string) {
	content, err := logManager.ReadLog(relPath)
	if err != nil {
		fmt.Printf("\n[%s] %s\n", status, relPath)
		fmt.Printf("Error reading log: %s\n", err)
		return
	}

	fmt.Printf("\n[%s] %s\n", status, relPath)
	fmt.Println("---")
	if strings.TrimSpace(content) == "" {
		fmt.Println("(no output)")
	} else {
		fmt.Println(content)
	}
	fmt.Println("---")
}

func PromptConfirmDestructive(opDesc string, repoCount int, dirtyRepos []repoPreflightInfo) bool {
	if len(dirtyRepos) > 0 {
		fmt.Printf("\n%s The following %d repos have changes that will be DISCARDED:\n", StyleFailed.Render("WARNING:"), len(dirtyRepos))
		for _, r := range dirtyRepos {
			fmt.Printf("  - %s  (%s)\n", StyleFailed.Render(r.RelPath), StyleDim.Render(r.DirtyStatus))
		}
		fmt.Println()
	}

	fmt.Printf("This will run '%s' on %d repos.\n", opDesc, repoCount)
	fmt.Print("Proceed? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
