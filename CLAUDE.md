# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**cstow** is a C++ package manager and build system written in Go. It wraps CMake for compilation, manages C++ dependencies with semver resolution, distributes pre-compiled packages via S3-compatible storage, and handles ABI compatibility across compilers and platforms.

## Build & Run

```bash
go build -o cstow .          # build the CLI
go test ./...                 # run all tests
go test ./internal/config/    # run tests for a single package
go test -run TestParse ./internal/config/  # run a specific test
```

## Architecture

The project follows a phased development plan (see PLAN.md for full design spec in Chinese):

- **`cmd/`** — Cobra CLI commands (`root.go`, `init.go`, `build.go`, etc.)
- **`internal/config/`** — `cstow.toml` parsing and Go config structs (`Config`, `Package`, `Dependency`, `Registry`, `Toolchain`, `Legacy`)
- **`internal/project/`** — Project scaffolding (`cstow init`)
- **`internal/toolchain/`** — Compiler detection (gcc/clang/msvc), version parsing, flag generation
- **`internal/resolver/`** — Semver dependency resolution, lock file generation
- **`internal/registry/`** — S3 client for package publish/download (AWS S3, Cloudflare R2, MinIO)
- **`internal/abi/`** — ABI tag system for C++ binary compatibility checking
- **`internal/legacy/`** — CMake/Make project migration and integration

## Key Design Decisions

- **Config format**: TOML (`cstow.toml` + `cstow.lock`), parsed with `BurntSushi/toml`
- **Build backend**: CMake wrapper — cstow generates CMake invocations, does not replace CMake
- **Package format**: `.tar.zst` (zstd-compressed tarballs) for pre-compiled artifacts
- **ABI tags**: `<compiler><ver>-cxx<year>-<stdlib>-<os>-<arch>` encoding for binary compatibility
- **Registry backend**: S3-compatible object storage with presigned URL support
- **CLI framework**: `spf13/cobra` + `spf13/viper`

## Key Dependencies

| Purpose | Package |
|---------|---------|
| CLI | `spf13/cobra`, `spf13/viper` |
| TOML | `BurntSushi/toml` |
| S3 | `aws/aws-sdk-go-v2` |
| Semver | `Masterminds/semver/v3` |
| Compression | `klauspost/compress` (zstd) |
| Concurrency | `golang.org/x/sync/errgroup` |
| Testing | `stretchr/testify` |

## Environment Variables

- `CSTOW_REGISTRY_KEY` / `CSTOW_REGISTRY_SECRET` — S3 registry credentials
- `CSTOW_REGISTRY_URL` — Custom registry endpoint
- `CSTOW_CXX` / `CSTOW_CC` — Override compiler detection
- `CSTOW_SYSROOT` — Cross-compilation sysroot
- `CSTOW_CACHE_DIR` — Package cache (default: `~/.cstow/cache`)
- `CSTOW_CI` — Non-interactive CI mode
