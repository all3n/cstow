package workspace

import (
	"context"
	"fmt"
	"sync"
)

// TaskFunc defines the function to be executed for each module.
type TaskFunc func(ctx context.Context, m *Module) error

// Scheduler manages the parallel execution of tasks based on a dependency graph.
type Scheduler struct {
	graph *Graph
	jobs  int
}

// NewScheduler creates a new parallel task scheduler.
func NewScheduler(g *Graph, jobs int) *Scheduler {
	if jobs <= 0 {
		jobs = 1
	}
	return &Scheduler{graph: g, jobs: jobs}
}

// Run executes the task for each module in the graph, respecting dependencies.
// It uses a worker pool of the configured size.
func (s *Scheduler) Run(ctx context.Context, task TaskFunc) error {
	// Initialize in-degree map for tracking progress
	inDeg := make(map[string]int)
	for name, count := range s.graph.InDeg {
		inDeg[name] = count
	}

	// Channel for modules ready to be processed
	ready := make(chan string, len(s.graph.Modules))
	
	// Error handling
	var (
		errOnce sync.Once
		firstErr error
		wg       sync.WaitGroup
		mu       sync.Mutex
	)

	// Context with cancellation on first error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	setError := func(err error) {
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	// Initial set of ready modules (in-degree 0)
	for name, count := range inDeg {
		if count == 0 {
			ready <- name
		}
	}

	// Worker pool
	for i := 0; i < s.jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case name, ok := <-ready:
					if !ok {
						return
					}

					m := s.graph.Modules[name]
					if err := task(ctx, m); err != nil {
						setError(fmt.Errorf("module %s: %w", name, err))
						return
					}

					// Update dependents
					mu.Lock()
					for _, depOnMe := range s.graph.Rev[name] {
						inDeg[depOnMe]--
						if inDeg[depOnMe] == 0 {
							ready <- depOnMe
						}
					}
					
					// Check if all modules are done
					delete(inDeg, name)
					if len(inDeg) == 0 {
						close(ready)
					}
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return firstErr
}
