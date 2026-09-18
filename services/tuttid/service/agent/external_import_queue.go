package agent

import (
	"context"
	"sync"
)

const defaultExternalImportParseConcurrency = 8

func (s *Service) externalImportConcurrency() int {
	if s != nil && s.externalImportParseConcurrency > 0 {
		return s.externalImportParseConcurrency
	}
	return defaultExternalImportParseConcurrency
}

func runBoundedIndexed[T any](
	ctx context.Context,
	concurrency int,
	count int,
	fn func(context.Context, int) T,
) []T {
	if count <= 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	out := make([]T, count)
	if count == 1 || concurrency == 1 {
		for index := 0; index < count; index++ {
			if ctx.Err() != nil {
				return out
			}
			out[index] = fn(ctx, index)
		}
		return out
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	workerCount := concurrency
	if workerCount > count {
		workerCount = count
	}
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				out[index] = fn(ctx, index)
			}
		}()
	}
	for index := 0; index < count; index++ {
		if ctx.Err() != nil {
			break
		}
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return out
}
