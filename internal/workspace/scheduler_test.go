package workspace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/all3n/cstow/internal/config"
)

func TestScheduler(t *testing.T) {
	// Diamond graph: D depends on B and C, B depends on A, C depends on A.
	// A -> B -> D
	// A -> C -> D
	modules := []*Module{
		{Name: "A", Path: "/a", Cfg: &config.Config{}},
		{Name: "B", Path: "/b", Cfg: &config.Config{Dependencies: []config.Dependency{{Path: "../a", Source: "local"}}}},
		{Name: "C", Path: "/c", Cfg: &config.Config{Dependencies: []config.Dependency{{Path: "../a", Source: "local"}}}},
		{Name: "D", Path: "/d", Cfg: &config.Config{Dependencies: []config.Dependency{{Path: "../b", Source: "local"}, {Path: "../c", Source: "local"}}}},
	}

	g, err := ComputeGraph(modules)
	if err != nil {
		t.Fatalf("ComputeGraph failed: %v", err)
	}

	t.Run("Parallel execution", func(t *testing.T) {
		s := NewScheduler(g, 2)
		var mu sync.Mutex
		results := make(map[string]time.Time)
		
		task := func(ctx context.Context, m *Module) error {
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			results[m.Name] = time.Now()
			mu.Unlock()
			return nil
		}

		start := time.Now()
		if err := s.Run(context.Background(), task); err != nil {
			t.Errorf("Run failed: %v", err)
		}
		duration := time.Since(start)

		// Expected order: A first, then (B and C) in parallel, then D.
		// Total time should be around 3 * 100ms = 300ms, definitely less than 400ms.
		if duration < 300*time.Millisecond || duration > 450*time.Millisecond {
			t.Errorf("Unexpected duration: %v", duration)
		}

		if results["A"].After(results["B"]) || results["A"].After(results["C"]) {
			t.Errorf("A should complete before B and C")
		}
		if results["B"].After(results["D"]) || results["C"].After(results["D"]) {
			t.Errorf("B and C should complete before D")
		}
	})

	t.Run("Error handling", func(t *testing.T) {
		s := NewScheduler(g, 2)
		task := func(ctx context.Context, m *Module) error {
			if m.Name == "A" {
				return errors.New("fail A")
			}
			return nil
		}

		err := s.Run(context.Background(), task)
		if err == nil || err.Error() != "module A: fail A" {
			t.Errorf("Expected error from A, got %v", err)
		}
	})
	
	t.Run("Linear graph", func(t *testing.T) {
		// A -> B -> C
		modules := []*Module{
			{Name: "A", Path: "/a", Cfg: &config.Config{}},
			{Name: "B", Path: "/b", Cfg: &config.Config{Dependencies: []config.Dependency{{Path: "../a", Source: "local"}}}},
			{Name: "C", Path: "/c", Cfg: &config.Config{Dependencies: []config.Dependency{{Path: "../b", Source: "local"}}}},
		}
		g, _ := ComputeGraph(modules)
		s := NewScheduler(g, 10) // many workers, but linear graph limits parallelism
		
		var mu sync.Mutex
		var order []string
		task := func(ctx context.Context, m *Module) error {
			mu.Lock()
			order = append(order, m.Name)
			mu.Unlock()
			return nil
		}

		if err := s.Run(context.Background(), task); err != nil {
			t.Errorf("Run failed: %v", err)
		}

		if len(order) != 3 || order[0] != "A" || order[1] != "B" || order[2] != "C" {
			t.Errorf("Unexpected execution order: %v", order)
		}
	})

	t.Run("Independent modules", func(t *testing.T) {
		// A, B, C, D (no dependencies)
		modules := []*Module{
			{Name: "A", Path: "/a", Cfg: &config.Config{}},
			{Name: "B", Path: "/b", Cfg: &config.Config{}},
			{Name: "C", Path: "/c", Cfg: &config.Config{}},
			{Name: "D", Path: "/d", Cfg: &config.Config{}},
		}
		g, _ := ComputeGraph(modules)
		s := NewScheduler(g, 4)

		task := func(ctx context.Context, m *Module) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		}

		start := time.Now()
		if err := s.Run(context.Background(), task); err != nil {
			t.Errorf("Run failed: %v", err)
		}
		duration := time.Since(start)

		// With 4 workers and 4 independent tasks of 100ms, should take ~100ms
		if duration > 150*time.Millisecond {
			t.Errorf("Should take ~100ms, took %v", duration)
		}
	})
}
