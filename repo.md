先用两张结构图把整体布局和配置继承关系说清楚，再给出所有文件的完整规格。

## 全局目录结构## 配置继承与覆盖流程---

## 一、`~/.cstow/config.toml` — 全局用户配置

```toml
# ~/.cstow/config.toml
# 所有字段均有合理默认值，不需要全部填写

[defaults]
std         = "c++17"          # 全局默认 C++ 标准
profile     = "debug"          # 默认构建 profile
jobs        = 0                # 并发编译数，0 = CPU 核心数
color       = true             # 彩色终端输出

[cache]
dir         = "~/.cstow/cache" # 可重定向（如 NFS 共享缓存）
max_size_gb = 10               # 超出后按 LRU 淘汰，0 = 不限制
retention_days = 90            # 0 = 永久保留

# 注册的额外 repository 目录（有序，优先级从高到低）
# 内置 ~/.cstow/repository/ 始终作为最后一个 fallback
[[repositories]]
name = "work"
path = "/opt/cstow-repos/work"   # 本地目录

[[repositories]]
name = "team"
path = "~/projects/cstow-pkgs"   # ~ 会被展开

# S3 远端 registry（用于 publish/fetch 预编译产物，与 repository 是两个概念）
[[registry]]
name     = "default"
url      = "s3://my-bucket/cstow"
provider = "cloudflare"           # aws | cloudflare | minio | custom
region   = "auto"
key_env  = "CSTOW_KEY"            # 从哪个环境变量读 access key
secret_env = "CSTOW_SECRET"

[[registry]]
name     = "readonly-mirror"
url      = "https://cstow-mirror.example.com"
provider = "custom"
read_only = true                  # 只允许 fetch，不允许 publish

# 全局编译器偏好（可被项目 cstow.toml [toolchain] 覆盖）
[toolchain]
prefer  = "clang"                # auto | gcc | clang | msvc
min_gcc = "11"
min_clang = "14"

# 全局默认构建 flags（package.toml 可追加或覆盖）
[build.flags]
cxx_flags  = ["-fstack-protector-strong"]
link_flags = []
defines    = []

# 代理（下载依赖时使用）
[network]
proxy       = ""                 # "http://proxy:8080"
no_proxy    = ["localhost", "127.0.0.1"]
timeout_sec = 60
retries     = 3
```

---

## 二、`~/.cstow/repository/` — 包定义库结构

### 目录约定

```
~/.cstow/repository/
├── g/
│   └── googletest/
│       ├── package.toml          # 包级主配置（共享编译逻辑）
│       ├── versions/             # 版本特定覆盖（可选，按需创建）
│       │   ├── 1.14.0.toml
│       │   └── 1.13.0.toml
│       └── patches/              # 补丁文件（可选）
│           └── 1.14.0-fix-msvc.patch
├── f/
│   └── fmt/
│       ├── package.toml
│       └── versions/
│           └── 11.0.0.toml
├── s/
│   └── spdlog/
│       └── package.toml          # 无版本覆盖时只需这一个文件
└── z/
    └── zlib/
        └── package.toml
```

索引规则：取包名第一个小写字母，`_` `-` 开头的包归入 `_` 目录。

---

## 三、`package.toml` — 包级共享配置（核心设计）

这是整个 repository 系统最重要的文件，承载所有版本共享的构建知识。

```toml
# ~/.cstow/repository/g/googletest/package.toml

[package]
name        = "googletest"
description = "Google C++ Testing and Mocking Framework"
homepage    = "https://github.com/google/googletest"
license     = "BSD-3-Clause"

# 支持的版本列表（semver，新版在前）
# 每个版本可额外有 versions/<ver>.toml 覆盖部分字段
versions = [
  "1.14.0",
  "1.13.0",
  "1.12.1",
  "1.11.0",
]

# 源码获取方式（所有版本共享，版本文件可覆盖 url/tag）
[source]
type = "git"
url  = "https://github.com/google/googletest.git"
# tag 模板：{version} 会被替换为实际版本号
tag_template = "v{version}"

# 也支持 archive 方式（与 git 二选一）
# [source]
# type = "archive"
# url_template = "https://github.com/google/googletest/archive/refs/tags/v{version}.tar.gz"
# sha256_versions = { "1.14.0" = "abcdef...", "1.13.0" = "fedcba..." }

# ── 共享的构建配置（所有版本默认使用）──────────────────────────
[build]
system       = "cmake"           # cmake | make | autoconf | meson | header-only | custom
type         = "static"          # static | shared | header-only | both

# CMake 专用配置
[build.cmake]
defines = [
  "BUILD_SHARED_LIBS=OFF",
  "BUILD_GMOCK=ON",
  "INSTALL_GTEST=OFF",
]
# install_targets 决定 cstow 从 build tree 里取哪些产物
install_targets = ["gtest", "gtest_main", "gmock", "gmock_main"]

# 各 profile 下的编译参数（叠加在全局之上）
[build.cmake.profile.debug]
defines   = ["CMAKE_BUILD_TYPE=Debug"]
cxx_flags = ["-g", "-O0"]

[build.cmake.profile.release]
defines   = ["CMAKE_BUILD_TYPE=Release"]
cxx_flags = ["-O3", "-DNDEBUG"]

# ── 编译器特殊适配 ─────────────────────────────────────────────
[build.compiler.msvc]
defines   = ["_SILENCE_TR1_NAMESPACE_DEPRECATION_WARNING=1"]
cxx_flags = ["/EHsc", "/utf-8"]

[build.compiler.clang]
cxx_flags = ["-Wno-unused-parameter"]

# ── 平台特殊适配 ───────────────────────────────────────────────
[build.platform.windows]
defines   = ["GTEST_OS_WINDOWS=1"]

[build.platform.macos]
cxx_flags = ["-mmacosx-version-min=11.0"]

# ── 产物描述（cstow 按此收集头文件和库文件）──────────────────
[artifacts]
include_dirs = ["googletest/include", "googlemock/include"]
libs         = ["libgtest.a", "libgtest_main.a", "libgmock.a", "libgmock_main.a"]
# Windows 下 .lib 后缀自动处理

# ── 依赖（此包自身的依赖）────────────────────────────────────
# googletest 本身无外部依赖，留空示例
# [[dependencies]]
# name    = "foo"
# version = "^1.0"
```

---

## 四、`versions/1.14.0.toml` — 版本特定覆盖

版本文件只需写与 `package.toml` 不同的字段，其余全部继承。

```toml
# ~/.cstow/repository/g/googletest/versions/1.14.0.toml
# 只写需要覆盖的内容

# 覆盖 sha256（archive 模式下校验用）
[source]
sha256 = "8ad598c73ad796e0d8280b082cebd82a630d73e73cd3c70057938a6501bba5d7"

# 此版本新增的 cmake 选项（追加到 package.toml 的 defines 之后）
[build.cmake]
defines = [
  "BUILD_SHARED_LIBS=OFF",
  "BUILD_GMOCK=ON",
  "INSTALL_GTEST=OFF",
  "GTEST_HAS_ABSL=OFF",   # 1.14 新增，旧版本没有这个选项
]

# 此版本在 MSVC 下有特殊问题，额外补丁
[build.compiler.msvc]
defines   = [
  "_SILENCE_TR1_NAMESPACE_DEPRECATION_WARNING=1",
  "_SILENCE_STDEXT_ARR_ITERS_DEPRECATION_WARNING=1",  # 1.14 特有
]
patch = "1.14.0-fix-msvc.patch"   # 相对于 patches/ 目录
```

---

## 五、项目 `cstow.toml` — 极简声明（Cargo 风格）

项目里的配置文件只管"我要什么"，不管"怎么编译它"。

```toml
[package]
name    = "my-game-engine"
version = "0.3.0"
std     = "c++20"

# 就这么简单，版本支持 semver 范围
[[dependencies]]
name    = "googletest"
version = "^1.14"

[[dependencies]]
name    = "fmt"
version = "^10"

[[dependencies]]
name    = "spdlog"
version = ">=1.12, <2.0"

# 本地依赖（路径相对于当前项目）
[[dependencies]]
name    = "myutil"
version = "*"
path    = "../myutil"

# dev-only 依赖（不打进发布产物）
[[dev-dependencies]]
name    = "benchmark"
version = "^1.8"

# 可选：覆盖全局工具链选择
[toolchain]
prefer = "clang"
```

**项目 toml 不需要写任何编译参数** — 所有构建知识都在 repository 的 `package.toml` 里，项目只声明依赖关系。

---

## 六、配置合并优先级与 Go 实现

### 优先级（从低到高）

```
内置 package.toml [build]
  ↓ 版本 versions/x.y.z.toml（部分字段覆盖）
    ↓ ~/.cstow/config.toml [build.flags]（用户全局偏好）
      ↓ 项目 cstow.toml [toolchain]（项目工具链选择）
        ↓ --profile release CLI 参数
          ↓ 环境变量 CSTOW_CXX_FLAGS / CSTOW_DEFINES（最高）
```

### Go 核心数据结构

```go
// internal/repository/package.go

type PackageDef struct {
    Package    PackageMeta        `toml:"package"`
    Versions   []string           `toml:"versions"`
    Source     SourceDef          `toml:"source"`
    Build      BuildDef           `toml:"build"`
    Artifacts  ArtifactsDef       `toml:"artifacts"`
    Deps       []DepRef           `toml:"dependencies"`
}

type BuildDef struct {
    System  string              `toml:"system"`   // cmake|make|meson|...
    Type    string              `toml:"type"`     // static|shared|header-only
    CMake   CMakeBuildDef       `toml:"cmake"`
    Profiles map[string]ProfileOverride `toml:"profile"`
    Compiler map[string]CompilerOverride `toml:"compiler"` // msvc|gcc|clang
    Platform map[string]PlatformOverride `toml:"platform"` // windows|linux|macos
}

// VersionOverride 只存储与 PackageDef 不同的字段，nil = 继承
type VersionOverride struct {
    Source  *SourceOverride  `toml:"source"`
    Build   *BuildOverride   `toml:"build"`
    Patch   string           `toml:"patch"`
}
```

### Repository 查找器

```go
// internal/repository/finder.go

type Finder struct {
    // 查找顺序：外部目录列表（按 config.toml 声明顺序）+ 内置目录
    searchPaths []string
}

func NewFinder(globalCfg *config.Global) *Finder {
    paths := make([]string, 0)
    for _, r := range globalCfg.Repositories {
        paths = append(paths, expandHome(r.Path))
    }
    // 内置目录始终最后
    paths = append(paths, filepath.Join(cstowHome(), "repository"))
    return &Finder{searchPaths: paths}
}

// Find 返回第一个命中的 PackageDef + 可选的版本覆盖
func (f *Finder) Find(name, version string) (*ResolvedPkg, error) {
    letter := string([]rune(strings.ToLower(name))[0])
    if !unicode.IsLetter([]rune(letter)[0]) {
        letter = "_"
    }
    for _, root := range f.searchPaths {
        pkgDir := filepath.Join(root, letter, name)
        pkgFile := filepath.Join(pkgDir, "package.toml")
        if _, err := os.Stat(pkgFile); err != nil {
            continue // 此 repository 没有这个包，继续找下一个
        }
        pkg, err := loadPackage(pkgFile)
        if err != nil {
            return nil, fmt.Errorf("loading %s: %w", pkgFile, err)
        }
        // 检查版本是否在支持列表内
        matched, err := matchVersion(pkg.Versions, version)
        if err != nil || matched == "" {
            continue
        }
        // 加载版本覆盖（如果存在）
        override := loadVersionOverride(pkgDir, matched) // nil if not exists
        return &ResolvedPkg{
            Def:      pkg,
            Version:  matched,
            Override: override,
            RepoPath: root,
        }, nil
    }
    return nil, fmt.Errorf("package %q@%q not found in any repository", name, version)
}
```

### 配置合并器

```go
// internal/repository/merge.go

type MergedBuildConfig struct {
    System      string
    CMakeDefines []string
    CXXFlags    []string
    LinkFlags   []string
    Profile     string
    Patch       string
}

func Merge(
    pkg     *PackageDef,
    ver     *VersionOverride,   // nil = 无版本覆盖
    global  *config.Global,
    toolchain *Toolchain,
    profile string,
) *MergedBuildConfig {
    out := &MergedBuildConfig{System: pkg.Build.System}

    // 1. 包级基础
    out.CMakeDefines = slices.Clone(pkg.Build.CMake.Defines)
    out.CXXFlags     = slices.Clone(pkg.Build.CMake.CXXFlags)

    // 2. Profile 叠加（debug/release）
    if po, ok := pkg.Build.Profiles[profile]; ok {
        out.CMakeDefines = append(out.CMakeDefines, po.Defines...)
        out.CXXFlags     = append(out.CXXFlags,     po.CXXFlags...)
    }

    // 3. 编译器适配
    if co, ok := pkg.Build.Compiler[toolchain.Kind]; ok {
        out.CMakeDefines = append(out.CMakeDefines, co.Defines...)
        out.CXXFlags     = append(out.CXXFlags,     co.CXXFlags...)
    }

    // 4. 平台适配
    if po, ok := pkg.Build.Platform[currentOS()]; ok {
        out.CMakeDefines = append(out.CMakeDefines, po.Defines...)
        out.CXXFlags     = append(out.CXXFlags,     po.CXXFlags...)
    }

    // 5. 版本特定覆盖（完整替换对应字段，不是追加）
    if ver != nil && ver.Build != nil {
        if len(ver.Build.CMake.Defines) > 0 {
            out.CMakeDefines = ver.Build.CMake.Defines  // 版本文件写了就全量替换
        }
        out.Patch = ver.Patch
    }

    // 6. 用户全局 flags（追加）
    out.CXXFlags  = append(out.CXXFlags,  global.Build.Flags.CXXFlags...)
    out.LinkFlags = append(out.LinkFlags, global.Build.Flags.LinkFlags...)

    // 7. 环境变量（最高优先级，追加）
    if envFlags := os.Getenv("CSTOW_CXX_FLAGS"); envFlags != "" {
        out.CXXFlags = append(out.CXXFlags, strings.Fields(envFlags)...)
    }

    return out
}
```

---

## 七、外部 repository 目录扩展

支持三种注册方式，全部在 `~/.cstow/config.toml` 中声明：

```toml
# 方式 1：本地文件系统路径（团队共享 NFS / 项目内）
[[repositories]]
name     = "team-internal"
path     = "/mnt/shared/cstow-pkgs"
priority = 10                        # 数字越小优先级越高，默认 50

# 方式 2：Git 仓库（自动 clone/pull 到本地缓存）
[[repositories]]
name     = "community"
git      = "https://github.com/yourorg/cstow-pkgs.git"
branch   = "main"
auto_update = true                   # cstow update 时自动 git pull

# 方式 3：tar.gz 快照（离线环境）
[[repositories]]
name     = "offline-snapshot"
archive  = "/opt/cstow-snapshot-2024.tar.gz"
```

对应的 `cstow` 命令：

```bash
# 添加外部 repository
cstow repo add --name team --path /mnt/shared/cstow-pkgs
cstow repo add --name community --git https://github.com/org/pkgs.git

# 列出所有已注册的 repository 及其优先级
cstow repo list

# 更新所有 git 类型的 repository
cstow repo update

# 在所有 repository 中搜索包
cstow search googletest
# 输出: [team-internal] googletest  1.14.0, 1.13.0
#        [builtin]       googletest  1.14.0, 1.13.0, 1.12.1, 1.11.0
```

---

## 八、`cstow add` 完整工作流（串联所有层）

```
用户: cstow add googletest@^1.14

  1. 读取 ~/.cstow/config.toml → 得到 searchPaths
  2. Finder.Find("googletest", "^1.14")
       → 命中 ~/.cstow/repository/g/googletest/package.toml
       → 选出 matched = "1.14.0"
       → 加载 versions/1.14.0.toml（存在）
  3. Merge(pkg, ver, globalCfg, toolchain, profile)
       → MergedBuildConfig{ cmake defines + flags + patches }
  4. 检查 cache: ~/.cstow/cache/googletest/1.14.0/<abi_tag>/
       → 命中 → 直接链接，结束
       → 未命中 → 尝试从 registry 拉取预编译包
           → 仍未命中 → 从 source.url clone 源码编译
  5. 编译完成 → 写入 cache + 写入 cstow.lock
  6. 向项目 cstow.toml 追加 [[dependencies]] 条目
```

这样项目开发者永远只需要写 `googletest = "^1.14"` 这一行，所有 "怎么编译 googletest" 的知识都沉淀在 repository 里，可以被所有项目复用，也方便团队统一维护构建配置。
