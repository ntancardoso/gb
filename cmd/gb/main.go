// Package main provides the entry point for the gb (Git Branch Switcher) CLI tool.
//
// gb is a specialized tool for maintaining consistent branch versions across
// multiple Git repositories, designed primarily for Odoo projects and OCA modules.
//
// Usage:
//
//	gb [options] <branch_name>
//	gb -list
//	gb -c "git command"
//
// For detailed usage information, run: gb -h
package main

import (
	"fmt"
	"os"

	"github.com/ntancardoso/gb/internal/core"
)

func main() {
	if err := core.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
