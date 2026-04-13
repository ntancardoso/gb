package core

import (
	"context"
	"sync"
)

func runPool[R any](ctx context.Context, repos []RepoInfo, workers int, process func(context.Context, RepoInfo) R) []R {
	if workers > len(repos) {
		workers = len(repos)
	}
	if workers < 1 {
		workers = 1
	}

	repoCh := make(chan RepoInfo, len(repos))
	resCh := make(chan R, len(repos))

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for r := range repoCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				resCh <- process(ctx, r)
			}
		})
	}
	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)
	go func() { wg.Wait(); close(resCh) }()

	results := make([]R, 0, len(repos))
	for res := range resCh {
		results = append(results, res)
	}
	return results
}
