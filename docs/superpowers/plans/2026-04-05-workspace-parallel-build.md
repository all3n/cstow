# Workspace Parallel Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement parallel building for workspace modules based on their dependency graph to improve performance.

**Architecture:** 
1. Refactor `internal/workspace/graph.go` to expose a `Graph` structure containing adjacency lists and in-degree information.
2. Implement a `Scheduler` in `internal/workspace/scheduler.go` that uses a worker pool to process modules. It will use an in-degree map to identify "ready" modules (those with 0 remaining dependencies) and dispatch them as workers become available.
3. Update `cmd/workspace.go` to add a `--jobs` (or `-j`) flag to `cstow workspace build` and use the new scheduler.

**Tech Stack:** Go 1.25+, standard library concurrency primitives (channels, WaitGroups, Mutexes).

---

### Task 1: Refactor graph.go to expose Graph structure

**Files:**
- Modify: `internal/workspace/graph.go`
- Test: `internal/workspace/graph_test.go`

- [ ] **Step 1: Define Graph struct and ComputeGraph function**

Modify `internal/workspace/graph.go` to include:

```go
// Graph represents the dependency relationship between modules.
type Graph struct {
	Order   []string            // One valid topological order
	Edges   map[string][]string // Forward edges: A depends on [B, C] -> Edges[A] = [B, C]
	Rev     map[string][]string // Reverse edges: A is depended on by [B, C] -> Rev[A] = [B, C]
	InDeg   map[string]int      // Number of dependencies for each module
	Modules map[string]*Module  // Lookup by module name
}

// ComputeGraph builds the dependency graph and returns it.
func ComputeGraph(modules []*Module) (*Graph, error) {
    // ... logic from BuildGraph but populating Graph struct ...
}
```

- [ ] **Step 2: Update BuildGraph to use ComputeGraph**

```go
func BuildGraph(modules []*Module) ([]string, error) {
	g, err := ComputeGraph(modules)
	if err != nil {
		return nil, err
	}
	return g.Order, nil
}
```

- [ ] **Step 3: Run existing tests to ensure no regressions**

Run: `go test ./internal/workspace/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/workspace/graph.go
git commit -m "refactor: expose Graph structure in workspace package"
```

---

### Task 2: Implement Parallel Scheduler

**Files:**
- Create: `internal/workspace/scheduler.go`
- Create: `internal/workspace/scheduler_test.go`

- [ ] **Step 1: Define Scheduler and TaskFunc**

In `internal/workspace/scheduler.go`:

```go
package workspace

import (
	"context"
	"fmt"
	"sync"
)

type TaskFunc func(ctx context.Context, m *Module) error

type Scheduler struct {
	graph *Graph
	jobs  int
}

func NewScheduler(g *Graph, jobs int) *Scheduler {
	if jobs <= 0 {
		jobs = 1
	}
	return &Scheduler{graph: g, jobs: jobs}
}
```

- [ ] **Step 2: Implement Run method**

The `Run` method should:
1. Initialize an in-degree map from `graph.InDeg`.
2. Find all modules with in-degree 0 (ready to build).
3. Use a worker pool of `jobs` size.
4. When a task completes, decrement in-degree of its dependents in `graph.Rev`. If a dependent reaches 0, it's ready to build.
5. Handle errors by stopping new tasks and waiting for active ones.

- [ ] **Step 3: Write tests for Scheduler**

In `internal/workspace/scheduler_test.go`, test with mock tasks and different graph shapes (linear, diamond, independent). Use `time.Sleep` in mock tasks to verify parallelism.

- [ ] **Step 4: Run scheduler tests**

Run: `go test ./internal/workspace/ -run TestScheduler -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workspace/scheduler.go internal/workspace/scheduler_test.go
git commit -m "feat: implement parallel workspace build scheduler"
```

---

### Task 3: Update workspace build command

**Files:**
- Modify: `cmd/workspace.go`

- [ ] **Step 1: Add --jobs flag to workspace build**

In `init()` of `cmd/workspace.go`:
```go
workspaceBuildCmd.Flags().IntP("jobs", "j", 1, "number of parallel build jobs")
```

- [ ] **Step 2: Update workspaceBuildCmd to use Scheduler**

Modify `workspaceBuildCmd.RunE`:
1. Load modules and compute graph.
2. Initialize `Scheduler`.
3. Define `TaskFunc` that calls `runBuildInDir`.
4. Call `scheduler.Run`.

- [ ] **Step 3: Manual validation**

Build `cstow` and run `workspace build --jobs 4` in a sample workspace (like `examples/workspace-demo`).

- [ ] **Step 4: Commit**

```bash
git add cmd/workspace.go
git commit -m "feat: add --jobs flag to workspace build"
```
