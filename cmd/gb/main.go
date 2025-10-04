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
