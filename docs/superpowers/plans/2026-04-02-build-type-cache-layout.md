# Build Type Cache Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `install`, `fetch`, and `publish` carry `build_type` end-to-end so static/shared/header-only artifacts are stored and resolved in distinct cache and registry locations.

**Architecture:** Extend dependency, lock, cache, and registry metadata with `build_type`, then route all cache lookups and artifact publish/download keys through a new `<abi>/<build_type>` layout. Preserve read compatibility with legacy `<abi>` directories and old lock files that do not record `build_type`.

**Tech Stack:** Go, Cobra CLI, TOML config/lock files, local filesystem cache, S3-compatible registry manifest flow

---

### Task 1: Add failing tests for schema and cache layout

**Files:**
- Modify: `cmd/install_integration_test.go`
- Modify: `cmd/deps_test.go`
- Modify: `internal/resolver/resolver_test.go`
- Modify: `internal/registry/s3client_test.go` or create focused registry tests if absent

- [ ] **Step 1: Write failing tests for build-type-specific cache paths and lock compatibility**
- [ ] **Step 2: Run targeted tests to verify they fail for the expected reason**
- [ ] **Step 3: Keep the failures focused on missing `build_type` support instead of unrelated regressions**

### Task 2: Implement cache path and lock/config schema changes

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/resolver/resolver.go`
- Modify: `cmd/deps.go`
- Modify: `cmd/fetch.go`

- [ ] **Step 1: Add `build_type` fields to dependency and lock structures**
- [ ] **Step 2: Extend cache path helpers to write `<abi>/<build_type>` and read legacy `<abi>`**
- [ ] **Step 3: Update dependency resolution helpers so install/fetch can derive an effective build type**
- [ ] **Step 4: Run targeted tests and make them pass**

### Task 3: Carry build_type through install, fetch, publish, and registry manifest

**Files:**
- Modify: `cmd/install.go`
- Modify: `cmd/fetch.go`
- Modify: `cmd/publish.go`
- Modify: `internal/registry/s3client.go`
- Modify: manifest structs/tests in `internal/registry/`

- [ ] **Step 1: Make `install` store and read build-type-specific cache entries**
- [ ] **Step 2: Make `fetch` prefer lock/config build types and use them for cache and source fallback**
- [ ] **Step 3: Make `publish` and registry manifest/object keys include `build_type`**
- [ ] **Step 4: Run targeted tests for install/fetch/publish and make them pass**

### Task 4: Update documentation and verify the full repository

**Files:**
- Modify: `README.md`
- Modify: `PLAN.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Update docs to describe `build_type` in dependencies, lock/cache layout, and registry flow**
- [ ] **Step 2: Run `go test ./... -count=1`**
- [ ] **Step 3: Confirm new behavior and legacy compatibility in the final report**
