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

func DisplayLogs(logManager *LogManager, results []CommandResult) {
	fmt.Println("\n=== Detailed Logs ===")
	fmt.Println()

	allLogs := logManager.GetAllLogPaths()

	fmt.Println("--- Failed Repositories ---")
	hasFailures := false
	for _, res := range results {
		if res.Error != nil {
			hasFailures = true
			displayRepoLog(logManager, res.RelPath, "FAILED")
		}
	}
	if !hasFailures {
		fmt.Println("(none)")
	}

	fmt.Println("\n--- Successful Repositories ---")
	hasSuccess := false
	for _, res := range results {
		if res.Error == nil {
			hasSuccess = true
			displayRepoLog(logManager, res.RelPath, "SUCCESS")
		}
	}
	if !hasSuccess {
		fmt.Println("(none)")
	}

	fmt.Printf("\n--- Log Location ---\n")
	fmt.Printf("Logs are stored in: %s\n", logManager.GetTempDir())
	fmt.Printf("Total log files: %d\n", len(allLogs))
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

// PromptConfirmDestructive shows a pre-flight warning and asks the user to confirm.
// Returns false (abort) if stdin is not a TTY or the user does not answer "y"/"yes".
func PromptConfirmDestructive(opDesc string, repoCount int, dirtyRepos []repoPreflightInfo) bool {
	if len(dirtyRepos) > 0 {
		fmt.Printf("\nWARNING: The following %d repos have changes that will be DISCARDED:\n", len(dirtyRepos))
		for _, r := range dirtyRepos {
			fmt.Printf("  - %s  (%s)\n", r.RelPath, r.DirtyStatus)
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

// DisplayResetLogs prints logs from a reset/rebase run, grouped by outcome.
func DisplayResetLogs(logManager *LogManager, results []ResetResult) {
	fmt.Println("\n=== Detailed Logs ===")
	fmt.Println()

	allLogs := logManager.GetAllLogPaths()

	fmt.Println("--- Failed Repositories ---")
	hasFailures := false
	for _, res := range results {
		if !res.Success && !res.Skipped {
			hasFailures = true
			displayRepoLog(logManager, res.RelPath, "FAILED")
		}
	}
	if !hasFailures {
		fmt.Println("(none)")
	}

	fmt.Println("\n--- Skipped Repositories ---")
	hasSkipped := false
	for _, res := range results {
		if res.Skipped {
			hasSkipped = true
			fmt.Printf("  [SKIPPED] %s - %s\n", res.RelPath, res.SkipReason)
		}
	}
	if !hasSkipped {
		fmt.Println("(none)")
	}

	fmt.Println("\n--- Successful Repositories ---")
	hasSuccess := false
	for _, res := range results {
		if res.Success {
			hasSuccess = true
			label := "SUCCESS"
			if res.Warning != "" {
				label = fmt.Sprintf("SUCCESS (warning: %s)", res.Warning)
			}
			displayRepoLog(logManager, res.RelPath, label)
		}
	}
	if !hasSuccess {
		fmt.Println("(none)")
	}

	fmt.Printf("\n--- Log Location ---\n")
	fmt.Printf("Logs are stored in: %s\n", logManager.GetTempDir())
	fmt.Printf("Total log files: %d\n", len(allLogs))
}

func DisplaySwitchLogs(logManager *LogManager, results []SwitchResult) {
	fmt.Println("\n=== Detailed Logs ===")
	fmt.Println()

	allLogs := logManager.GetAllLogPaths()

	fmt.Println("--- Failed Repositories ---")
	hasFailures := false
	for _, res := range results {
		if !res.Success {
			hasFailures = true
			displayRepoLog(logManager, res.RelPath, "FAILED")
		}
	}
	if !hasFailures {
		fmt.Println("(none)")
	}

	fmt.Println("\n--- Successful Repositories ---")
	hasSuccess := false
	for _, res := range results {
		if res.Success {
			hasSuccess = true
			displayRepoLog(logManager, res.RelPath, "SUCCESS")
		}
	}
	if !hasSuccess {
		fmt.Println("(none)")
	}

	fmt.Printf("\n--- Log Location ---\n")
	fmt.Printf("Logs are stored in: %s\n", logManager.GetTempDir())
	fmt.Printf("Total log files: %d\n", len(allLogs))
}
