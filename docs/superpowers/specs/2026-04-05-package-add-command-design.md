# Design: `cstow package add` Command

This document defines the design for the `cstow package add` command, which helps users quickly create new package recipe skeletons in a repository.

## 1. Goal
Provide a convenient way to add new package recipes to local repositories, following the directory structure expected by the `Finder`.

## 2. Requirements

- Add a root command `cstow package`.
- Add a sub-command `cstow package add <pkg_name>`.
- Support specifying the target repository via:
  - `--repo_dir <path>`: Direct directory path.
  - `--repo_name <name>`: Reference a repository by name from `~/.cstow/config.toml`.
  - Default: `./.cstow/repository`.
- Follow the subdirectory structure: `repo_root/<letter>/<pkg_name>/package.toml`.
- Create an empty `versions/` subdirectory within the package directory.
- Populate `package.toml` with a sensible default template.
- Error if the package already exists to prevent accidental overwrite.

## 3. Architecture

### 3.1. Command Layer (`cmd/package.go`)
- Defines `packageCmd` and `packageAddCmd`.
- Uses `cobra` for argument parsing and flag handling.
- Resolves the target repository path using `internal/config`.

### 3.2. Config Layer (`internal/config/config.go`)
- Adds `Global.RepoPathByName(name string) (string, bool)` to find a repository by name in the global configuration.
- Adds `Global.DefaultRepoPath() string` to return the default repository path for the current project.

### 3.3. Repository Layer (`internal/repository/scaffold.go`)
- Adds `ScaffoldPackage(repoDir, pkgName string) error` to create the directory structure and the `package.toml` file.
- Uses `internal/repository/package.go`'s `PackageDef` struct to ensure consistency.

## 4. Data Flow

1. **User input**: `cstow package add mylib --repo_name local`.
2. **Path Resolution**:
   - `mylib` -> `m/mylib`.
   - `--repo_name local` -> lookup `local` in `~/.cstow/config.toml` -> `/home/user/workspaces/cstow-repository`.
3. **Directory Creation**:
   - Create `/home/user/workspaces/cstow-repository/m/mylib/versions/`.
4. **File Creation**:
   - Create `/home/user/workspaces/cstow-repository/m/mylib/package.toml` with:
     ```toml
     [package]
     name = "mylib"
     description = ""

     versions = ["0.1.0"]

     [source]
     type = "git"
     url = ""
     tag_template = "{version}"

     [build]
     system = "cmake"
     type = "static"

     [artifacts]
     include_dirs = ["include"]
     ```

## 5. Error Handling

- **Invalid Repo Name**: Return error if `--repo_name` is not found.
- **Missing Repo Path**: Return error if target repository directory cannot be determined or created.
- **Already Exists**: Return error if `package.toml` already exists at the target path.

## 6. Testing

- **Unit test** for `Global.RepoPathByName`.
- **Unit test** for `ScaffoldPackage` (verifying directory and file creation).
- **Integration test** in `cmd/package_test.go` (if possible) or `cmd/install_integration_test.go` style.
