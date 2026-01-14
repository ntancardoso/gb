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
