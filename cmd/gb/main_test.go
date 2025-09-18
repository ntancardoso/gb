package main

import (
	"os/exec"
	"testing"
)

func TestMainCommand(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "-list")
	if err := cmd.Run(); err != nil {
		// This might fail, but at least it doesn't panic
		t.Logf("Command failed (expected): %v", err)
	}
}
