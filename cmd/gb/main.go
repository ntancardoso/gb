package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/ntancardoso/gb/internal/core"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := core.Run(ctx, os.Args[1:]); err != nil {
		if !core.IsSilentError(err) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}
