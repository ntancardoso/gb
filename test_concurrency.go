package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

func main() {
	fmt.Println("GOMAXPROCS:", runtime.GOMAXPROCS(0))
	fmt.Println("NumCPU:", runtime.NumCPU())

	workers := 20
	jobs := 50
	
	jobCh := make(chan int, jobs)
	resCh := make(chan int, jobs)
	var wg sync.WaitGroup

	start := time.Now()
	
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for job := range jobCh {
				fmt.Printf("Worker %d processing job %d\n", id, job)
				time.Sleep(100 * time.Millisecond)
				resCh <- job
			}
		}(i)
	}

	go func() {
		for i := 0; i < jobs; i++ {
			jobCh <- i
		}
		close(jobCh)
	}()

	go func() {
		wg.Wait()
		close(resCh)
	}()

	count := 0
	for range resCh {
		count++
	}
	
	elapsed := time.Since(start)
	fmt.Printf("\nProcessed %d jobs in %v\n", count, elapsed)
	fmt.Printf("Expected ~%v with %d workers (serial would be ~%v)\n", 
		time.Duration(jobs)*100*time.Millisecond/time.Duration(workers),
		workers,
		time.Duration(jobs)*100*time.Millisecond)
}
