以下是 **cstow** 的完整设计方案，分为 7 个可测试阶段，每阶段交付一个独立可运行的功能。

---

## 整体架构概览## 分阶段开发路线图---

## Phase 1 — CLI 骨架 + `cstow.toml` 解析 ✅ DONE

**目标**：`cstow init` 创建项目，`cstow build` 调用系统 cmake，配置文件完整解析。

### 目录结构

```
cstow/
├── cmd/
│   ├── root.go        # cobra root
│   ├── init.go        # cstow init
│   └── build.go       # cstow build
├── internal/
│   ├── config/
│   │   └── config.go  # cstow.toml 解析
│   └── project/
│       └── scaffold.go
├── main.go
└── go.mod
```

### `cstow.toml` 完整结构设计

```toml
[package]
name    = "mylib"
version = "0.1.0"
std     = "c++17"          # c++14 / c++17 / c++20 / c++23
authors = ["you@example.com"]

[build]
type    = "library"        # library | executable | header-only
sources = ["src/**/*.cpp"]
include = ["include"]
defines = ["MY_EXPORT=1"]

# 多 profile
[profile.debug]
optimize = "0"
debug    = true

[profile.release]
optimize = "3"
lto      = true

# 依赖声明
[[dependencies]]
name    = "fmt"
version = "^10.0.0"
source  = "registry"       # registry | local | git

[[dependencies]]
name    = "myutil"
version = "*"
source  = "local"
path    = "../myutil"

# 注册表后端（可多个）
[[registry]]
name     = "default"
url      = "s3://my-bucket/cstow-registry"
provider = "cloudflare"    # aws | cloudflare | minio | custom
region   = "auto"
# key/secret 从环境变量读取

# 工具链 profile
[toolchain]
compiler = "auto"          # auto | gcc | clang | msvc
minimum  = "gcc-11"
sysroot  = ""              # 交叉编译 sysroot

# CMake/Make 老项目集成
[legacy]
type       = "cmake"       # cmake | make | autoconf
root       = "."
extra_args = ["-DFOO=1"]
```

### 关键 Go 数据结构

```go
// internal/config/config.go
package config

type Config struct {
    Package      Package            `toml:"package"`
    Build        Build              `toml:"build"`
    Profiles     map[string]Profile `toml:"profile"`
    Dependencies []Dependency       `toml:"dependencies"`
    Registries   []Registry         `toml:"registry"`
    Toolchain    Toolchain          `toml:"toolchain"`
    Legacy       *Legacy            `toml:"legacy"`
}

type Package struct {
    Name    string   `toml:"name"`
    Version string   `toml:"version"`
    Std     string   `toml:"std"`
    Authors []string `toml:"authors"`
}

type Dependency struct {
    Name    string `toml:"name"`
    Version string `toml:"version"`
    Source  string `toml:"source"` // registry|local|git
    Path    string `toml:"path"`
    Git     string `toml:"git"`
    Rev     string `toml:"rev"`
}

type Registry struct {
    Name     string `toml:"name"`
    URL      string `toml:"url"`
    Provider string `toml:"provider"`
    Region   string `toml:"region"`
}
```

**测试点**：`cstow init myproject` 生成目录结构和 `cstow.toml`，`cstow build` 调用 cmake 成功编译 Hello World。

---

## Phase 2 — 工具链检测 + 多编译器支持 ✅ DONE

**目标**：自动发现本机 gcc/clang/msvc，通过 `--toolchain` 切换，生成正确 cmake 变量。

```go
// internal/toolchain/detect.go
type Toolchain struct {
    Kind     string   // gcc | clang | msvc | appleclang
    Version  [3]int   // major.minor.patch
    Path     string
    CXX      string
    CC       string
    Sysroot  string
    Target   string   // x86_64-linux-gnu / aarch64-linux-gnu ...
    ABITag   string   // 由 Phase 5 填充
}

// 探测优先级: cstow.toml > CC/CXX env > PATH 扫描
func Detect(cfg *config.Toolchain) (*Toolchain, error)
```

多编译器适配矩阵：

| 编译器 | Std flag | 警告 flags | MSVC 特殊处理 |
|--------|----------|-----------|---------------|
| GCC / Clang | `-std=c++17` | `-Wall -Wextra` | — |
| MSVC | `/std:c++17` | `/W4` | `/utf-8 /EHsc` |
| Apple Clang | `-std=c++17` | `-Wall` | `-stdlib=libc++` |

**测试点**：`cstow build --toolchain clang` 和 `--toolchain gcc` 分别生成带正确 `-DCMAKE_CXX_COMPILER=` 参数的 cmake 调用。

---

## Phase 3 — 本地依赖管理 + `cstow.lock` ✅ DONE

**目标**：`cstow add fmt@^10` 解析依赖树，生成 lock 文件，本地 cache 命中复用。

### Lock 文件格式

```toml
# cstow.lock — 自动生成，勿手动编辑
version = 1

[[package]]
name    = "fmt"
version = "10.2.1"
source  = "registry:default"
sha256  = "abcdef..."
abi_tag = "gcc13-cxx17-x86_64"

[[package]]
name    = "spdlog"
version = "1.13.0"
source  = "registry:default"
sha256  = "fedcba..."
deps    = ["fmt"]
```

### 依赖解析器

```go
// internal/resolver/resolver.go
type Resolver struct {
    cache    CacheStore
    registry RegistryClient
}

// SAT-based semver 解析（Pub-grub 简化版）
func (r *Resolver) Resolve(root []config.Dependency) (*LockFile, error)
```

**缓存目录结构**：`~/.cstow/cache/<name>/<version>/<abi_tag>/`，包含预编译产物 + 头文件。

**测试点**：`cstow add spdlog` 解析传递依赖 fmt，生成 lock，第二次 build 命中本地 cache 跳过下载。

---

## Phase 4 — S3 Registry：发布与下载 ✅ DONE

**目标**：`cstow publish` 把当前包打包上传 S3，`cstow add` 从 S3 拉取预编译包。

### S3 Registry 布局（约定大于配置）

```
<bucket>/
  cstow-registry/
    fmt/
      10.2.1/
        manifest.toml          # 包元数据
        gcc13-cxx17-linux-x86_64.tar.zst   # 预编译包
        gcc13-cxx17-linux-aarch64.tar.zst
        headers.tar.zst        # header-only 包
```

### Cloudflare R2 / AWS S3 适配

```go
// internal/registry/s3client.go
type S3Client struct {
    // 使用 aws-sdk-go-v2，同时兼容 R2
    // R2 endpoint: https://<accountid>.r2.cloudflarestorage.com
    // 凭证: AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
    //       或 cstow.toml [registry] 中指定的 env 变量名
}

// 支持 presigned URL 下载（无需 client 有 bucket 写权限）
func (c *S3Client) Download(pkg, version, abiTag string) (io.Reader, error)
func (c *S3Client) Upload(pkg, version, abiTag string, r io.Reader) error
func (c *S3Client) ListVersions(pkg string) ([]string, error)
```

**manifest.toml 示例**：

```toml
name    = "fmt"
version = "10.2.1"
std     = "c++17"
license = "MIT"

[[artifact]]
abi_tag  = "gcc13-cxx17-linux-x86_64"
sha256   = "abc..."
size     = 1234567
url      = "https://pub-xxx.r2.dev/..."   # 可选 CDN 直链
```

**测试点**：在本地 MinIO（或 CF R2 staging）上 `cstow publish`，换机器 `cstow add` 拉取，build 成功。

---

## Phase 5 — ABI 管理 + 多 triplet ✅ DONE

这是 C++ 工具链最复杂的部分，cstow 通过 **ABI Tag** 统一描述。

### ABI Tag 编码规则

```
<compiler><major>-cxx<std_year>-<stdlib>-<os>-<arch>[-<extra>]

示例:
  gcc13-cxx17-libstdc-linux-x86_64
  clang17-cxx20-libcxx-macos-arm64
  msvc193-cxx17-msvcrt-windows-x64
  clang16-cxx17-libcxx-linux-aarch64-android29  # 交叉编译
```

```go
// internal/abi/abi.go
type ABITag struct {
    Compiler string  // gcc | clang | msvc | appleclang
    CompVer  int     // major version
    CxxStd   int     // 14 17 20 23
    Stdlib   string  // libstdc | libcxx | msvcrt
    OS       string  // linux | macos | windows | android | ios
    Arch     string  // x86_64 | aarch64 | arm | wasm32
    Extra    string  // android api level, etc.
}

func (t ABITag) String() string
func Parse(s string) (ABITag, error)
func Compatible(have, need ABITag) bool  // 兼容性检查
```

### 兼容性矩阵（核心逻辑）

```go
func Compatible(have, need ABITag) bool {
    // 1. OS 和 Arch 必须完全一致
    // 2. stdlib 必须相同（libstdc++ 和 libc++ 二进制不兼容）
    // 3. C++ std 可以向上兼容（have.CxxStd >= need.CxxStd）
    // 4. 编译器版本：同族向上兼容（gcc12 可满足 gcc11 的包）
    // 5. MSVC: 仅同主版本兼容
}
```

**测试点**：`cstow check-abi` 报告当前环境 ABI tag，`cstow add` 时自动选择兼容包，不兼容时给出明确错误信息而非崩溃。

---

## Phase 6 — CMake / Make 老项目一键集成 ✅ DONE

**目标**：存量项目零修改接入 cstow 依赖管理。

### 两种接入模式

**模式 A：完全包装**（老项目根目录加一个 `cstow.toml`）

```toml
[package]
name = "legacy-engine"
version = "2.0.0"

[legacy]
type = "cmake"
root = "."
extra_args = ["-DBUILD_SHARED_LIBS=OFF"]

# 声明依赖，cstow 负责下载，注入 cmake PREFIX
[[dependencies]]
name    = "zlib"
version = "^1.3"
```

cstow 工作流：拉取依赖 → 写入 `CMAKE_PREFIX_PATH` → 调用原生 cmake。

**模式 B：migrate 扫描**（生成 cstow 原生配置）

```bash
cstow migrate --from cmake
```

扫描 `CMakeLists.txt` 的 `find_package()` / `FetchContent_Declare()` 调用，自动生成对应 `[[dependencies]]` 条目和等价 `cstow.toml`。

```go
// internal/legacy/cmake_scanner.go
type CMakeScanner struct{}

func (s *CMakeScanner) Scan(cmakePath string) ([]config.Dependency, error) {
    // 解析 find_package(Foo VERSION x.y.z REQUIRED)
    // 解析 FetchContent_Declare(foo GIT_REPOSITORY ... GIT_TAG ...)
    // 输出建议的 [[dependencies]] 列表
}
```

**测试点**：克隆一个真实 CMake 开源项目，`cstow migrate`，得到 `cstow.toml`，`cstow build` 编译成功。

---

## Phase 7 — Workspace + CI + 插件

**workspace 多包管理**（Cargo workspace 同款）：

```toml
# 根目录 cstow.toml
[workspace]
members = ["engine", "renderer", "tools/*"]
```

**GitHub Actions 一键集成**：

```yaml
# cstow ci --emit github 自动生成
- uses: actions/cache@v4
  with:
    path: ~/.cstow/cache
    key: cstow-${{ matrix.toolchain }}-${{ hashFiles('cstow.lock') }}

- run: cstow build --profile release --toolchain ${{ matrix.toolchain }}
  env:
    CSTOW_REGISTRY_KEY: ${{ secrets.R2_ACCESS_KEY }}
    CSTOW_REGISTRY_SECRET: ${{ secrets.R2_SECRET }}
```

**hooks 扩展点**（`cstow.toml`）：

```toml
[hooks]
pre-build  = "scripts/codegen.sh"
post-build = "scripts/sign.sh"
pre-publish = "scripts/test-all.sh"
```

---

## 关键技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| CLI 框架 | `spf13/cobra` + `spf13/viper` | 业界标准，子命令 + 配置绑定 |
| TOML 解析 | `BurntSushi/toml` | 最成熟的 Go TOML 库 |
| S3 客户端 | `aws/aws-sdk-go-v2` | 兼容所有 S3 协议实现（含 R2） |
| Semver | `Masterminds/semver/v3` | 完整 semver 2.0 支持 |
| 压缩格式 | `.tar.zst`（zstd） | 比 gz 快 3–5 倍，`klauspost/compress` |
| 并行下载 | `golang.org/x/sync/errgroup` | 并发依赖拉取 |
| 测试 | `stretchr/testify` | 标准断言库 |
| 进度条 | `schollz/progressbar/v3` | 下载 / 编译进度展示 |

---

## 环境变量约定

```bash
# 注册表鉴权
CSTOW_REGISTRY_KEY=...
CSTOW_REGISTRY_SECRET=...
CSTOW_REGISTRY_URL=https://<id>.r2.cloudflarestorage.com

# 工具链覆盖
CSTOW_CXX=clang++
CSTOW_CC=clang
CSTOW_SYSROOT=/path/to/sysroot

# 缓存目录
CSTOW_CACHE_DIR=~/.cstow/cache

# CI 模式（禁用交互，失败即退出）
CSTOW_CI=1
```

---

## 快速启动（Phase 1 可立即验证）

```bash
git clone https://github.com/all3n/cstow.git
cd cstow
go build -o cstow .

# 新建项目
./cstow init hello-cpp
cd hello-cpp
cat cstow.toml

# 构建（依赖系统 cmake）
./cstow build
./build/hello-cpp
```

每个阶段都可以独立 PR + 集成测试，Phase 1–2 完成后已经可以作为 cmake 的前端使用，后续阶段在此基础上叠加能力，不需要重写
