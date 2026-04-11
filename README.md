# cstow Repository 系统说明

更新时间：2026-04-11

> **⚠️ 核心开发准则**：代码的开发和演进必须无条件服从 `AGENTS.md` (主 PROMPT) 的约定，严格执行**每次功能迭代都是 MVP (最小可行性产品) 且必须可测试**的原则。**严禁碎片化提交，请在完整测好功能后再一次性提交。**

这份文档描述的是 **当前仓库中的 repository 系统 MVP**，不是最初的理想终态设计。

如果你想看历史设计稿和更激进的目标方案，参考：

- `docs/superpowers/specs/2026-03-31-repository-system-design.md`
- `docs/superpowers/specs/2026-04-01-repository-core-design.md`

本文优先回答三个问题：

1. 现在代码里到底支持什么
2. `repository` 和 `registry` 的边界是什么
3. 哪些字段只是“已解析”，哪些字段已经真正参与构建

## 一、先分清两个概念

### 1. `registry`

`registry` 用来存放和分发 **预编译产物**。

当前相关命令：

- `cstow publish`
- `cstow fetch`
- `cstow artifact show <hashid>`

核心实现：

- `internal/registry/`
- `internal/pack/`

### 2. `repository`

`repository` 用来存放 **源码构建 recipe**，也就是”这个 C++ 包该怎么拉源码、怎么配 CMake、怎么按平台/编译器做覆盖”。

当前相关命令：

- `cstow install`
- `cstow search <query>`

核心实现：

- `internal/repository/`
- `internal/builder/`

### 3. `git flow`

`git flow` 用来直接声明 Git 仓库依赖，在 `cstow.toml` 中配置 CMake 构建选项。

当前相关命令：

- `cstow add --source git --git-url <url> --tag <tag>`
- `cstow fetch` (git source fallback)

### 4. 三条路径已打通

当前三条依赖获取路径已连通：

- `cstow add` 会校验 dependency 是否存在于 registry 或 repository（git 源码跳过校验）
- `cstow fetch` 优先走 registry 预编译包；缺失时回退到 git source 或 repository recipe 源码构建
- `cstow build --fetch` 可以自动补全缺失依赖
- `cstow install` 支持递归构建 recipe 依赖，共享依赖会自动传播 `-fPIC`

## 二、当前命令关系

### 1. 项目依赖路径

这是今天项目构建真正依赖的路径：

1. `cstow add`
2. `cstow fetch`
3. `cstow build`

含义分别是：

- `add`：修改 `cstow.toml` 并生成/更新 `cstow.lock`，可通过 `--source` 指定来源 (`registry|git|local|repository`)，通过 `--build-type` 写入 dependency 的目标产物类型。Git 源码支持 `--git-url`、`--tag`、`--cmake-define`。
- `fetch`：优先从 registry 下载预编译包到 cache，缺失时可回退到 git source 或 repository recipe 源码构建，并在项目下生成 `./cstow_deps/<pkg>` 符号链接
- `build`：运行项目自身的 CMake，并把 `cstow_deps` 注入到 `CMAKE_PREFIX_PATH`。支持 `--fetch` 自动补全缺失依赖

这些命令现在都支持用重复的 `--repository <path>` 临时追加 repository 搜索路径；这些路径会以更高优先级参与当前命令解析，但不会替代全局或项目级 repository 配置。

### 2. repository recipe 路径

这是今天 repository 系统真正连通的路径：

1. `cstow install <package>[@<version>]`

含义：

- 从 `~/.cstow/config.toml` 读取 repository 搜索路径（支持项目级 `.cstow/repository/` 最高优先级）
- 查找 `package.toml` 和可选的 `versions/<ver>.toml`
- 合并 build 配置
- 递归处理 recipe 自身依赖（共享依赖自动传播 `-fPIC`）
- 拉源码（git / archive）
- 应用版本特定的 patch
- 本地构建
- 安装到 `~/.cstow/cache/<name>/<version>/<abi_tag>/<build_type>/`
- 索引到本地 artifact DB (`~/.cstow/cstow.db`)

同样支持通过 `--repository <path>` 临时追加一次性测试仓库，并优先于全局配置中的 repository 路径。

### 3. publish 的两种当前模式

`cstow publish` 目前有两种明确模式：

1. 项目目录发布（不带包名参数）
   - 从当前项目 `cstow.toml` 读取 `package.name/version`
   - 默认从 `build/release`（不存在则 `build/debug`）打包上传
2. 本地 artifact 直发（带包名参数）
   - 用法：`cstow publish <name> --version <ver> --abi-tag <abi> --build-type <type>`
   - 从本地 artifact 索引或 cache 路径定位对应 `(name, version, abi_tag, build_type)` 后打包上传
   - 本模式下 `--version` / `--abi-tag` / `--build-type` 都是必填

### 4. workspace 与诊断命令

当前代码中这两组辅助命令也已连通：

- `cstow workspace init [name]`
  - 在当前目录初始化 workspace 根配置
- `cstow workspace add <path>`
  - 把已有 member 加入当前 workspace
- `cstow workspace list|fetch|build|gen|clean`
  - 支持成员枚举、统一抓取依赖、按依赖顺序构建、生成 CMake 文件和批量清理
- `cstow doctor`
  - 检查 CMake、编译器、缓存目录、artifact DB，以及 registry 基础连通性

## 三、全局配置：`~/.cstow/config.toml`

当前代码中可解析的用户级配置结构如下：

```toml
[defaults]
std = "c++17"
profile = "debug"
jobs = 0
color = true

[cache]
dir = "~/.cstow/cache"
max_size_gb = 10
retention_days = 90

[[repositories]]
name = "team"
path = "/opt/cstow-pkgs"
priority = 10

[[repositories]]
name = "work"
path = "~/projects/pkgs"
priority = 20

[[registry]]
name = "default"
url = "s3://my-bucket/cstow"
provider = "cloudflare"
region = "auto"
profile = "cstow"
endpoint_url = "https://<account>.r2.cloudflarestorage.com"
# 可选：显式凭证。若设置，会优先于 ~/.aws/credentials
access_key = ""
secret_key = ""

[toolchain]
prefer = "clang"
min_gcc = "12"
min_clang = "16"

[build.flags]
cxx_flags = ["-fstack-protector-strong"]
link_flags = []
defines = []

[network]
proxy = "http://proxy:8080"
no_proxy = ["localhost", "127.0.0.1"]
timeout_sec = 30
retries = 5
```

### 这些字段当前真正生效的部分

- `defaults.std`
  - `cstow install` 计算 ABI tag 时会使用
- `toolchain.prefer`
  - `cstow install` 探测编译器时会使用
- `repositories[].path`
  - repository 搜索路径
- `repositories[].priority`
  - repository 搜索优先级，数字越小越优先
- `registry`
  - `publish` / `fetch` 使用
  - project `cstow.toml` 中未配置时，会回退到 `~/.cstow/config.toml`
  - 如果 project 和 global 都配置了同名或同 URL 的 registry，会优先使用 project 字段，并用 global 中缺失字段补全
  - `endpoint_url` 可以显式指定 S3 兼容 endpoint
  - 未显式配置 `endpoint_url` 时，会尝试从 `AWS_PROFILE` / `registry.profile` 对应的 `~/.aws/config` 读取 S3 endpoint
  - 显式凭证优先级：`CSTOW_REGISTRY_KEY/SECRET` > `registry.access_key/secret_key` > `~/.aws/credentials` / AWS 默认链
- `cache.dir`
  - 当前结构体支持，但 `resolver.NewFSCache()` 仍优先走 `CSTOW_CACHE_DIR` 或默认 `~/.cstow/cache`，还没有完全对齐到这个字段

### 这些字段当前只解析或只部分保留

- `repositories[].git`
- `repositories[].branch`
- `repositories[].archive`
- `repositories[].auto_update`
- `[build.flags]`
- `[network]`

这些字段在配置结构里已存在，但 repository / install 主路径还没有完整消费。

## 四、repository 目录布局

当前查找器约定的目录结构：

```text
~/.cstow/repository/
├── f/
│   └── fmt/
│       ├── package.toml
│       ├── versions/
│       │   └── 10.2.1.toml
│       └── patches/
│           └── 10.2.1-fix.patch
├── g/
│   └── googletest/
│       └── package.toml
└── _/
    └── 7zip/
        └── package.toml
```

规则：

- 包名首字母是字母时，使用小写首字母目录
- 非字母开头时放到 `_`
- `versions/` 可选
- `patches/` 可选

### 搜索顺序

`Finder` 的搜索顺序是：

1. 项目级仓库 `<project>/.cstow/repository/`（最高优先级，如存在）
2. `~/.cstow/config.toml` 中声明的 `repositories`
3. 按 `priority` 从小到大排序
4. 最后追加内置 fallback：`~/.cstow/repository`

一旦在更高优先级仓库中找到匹配版本，就不会继续往后查找。

## 五、`package.toml` 当前支持的 schema

当前 `internal/repository/package.go` 支持的核心结构如下。

```toml
versions = ["1.14.0", "1.13.0"]

[package]
name = "googletest"
description = "Google C++ Testing Framework"
homepage = "https://github.com/google/googletest"
license = "BSD-3-Clause"

[source]
type = "git"
url = "https://github.com/google/googletest.git"
tag_template = "v{version}"

[build]
system = "cmake"
type = "static"

[build.cmake]
defines = ["BUILD_SHARED_LIBS=OFF", "INSTALL_GTEST=OFF"]
cxx_flags = ["-Wall"]
link_flags = []
install_targets = ["gtest", "gmock"]

[build.profile.debug]
defines = ["CMAKE_BUILD_TYPE=Debug"]
cxx_flags = ["-g", "-O0"]
link_flags = []

[build.profile.release]
defines = ["CMAKE_BUILD_TYPE=Release"]
cxx_flags = ["-O3"]
link_flags = []

[build.compiler.gcc]
cxx_flags = ["-fPIC"]

[build.compiler.msvc]
defines = ["_CRT_SECURE_NO_WARNINGS=1"]
cxx_flags = ["/EHsc"]

[build.platform.linux]
defines = ["LINUX=1"]

[build.platform.windows]
defines = ["WINDOWS=1"]

[artifacts]
include_dirs = ["googletest/include", "googlemock/include"]
libs = ["libgtest.a", "libgmock.a"]

[[dependencies]]
name = "absl"
version = "^20240116"
source = "registry"
build_type = "shared"
```

### 关键说明

- `versions` 是顶层字段，不在 `[package]` 里
- profile 层当前写法是 `[build.profile.<name>]`
- compiler 层当前写法是 `[build.compiler.<kind>]`
- platform 层当前写法是 `[build.platform.<goos>]`
- `build.system` 当前数据结构允许多种值，但真正构建路径目前主要支持 CMake
- `build.type` 当前用于区分 `static` / `shared` / `header-only`
- `cstow add --build-type <kind>` 会把 dependency 的目标类型写入 `cstow.toml` 和 `cstow.lock`
- `cstow install --type <kind>` 会覆盖 recipe 默认 `build.type`；当前实现里这个显式覆盖也会压过 recipe 中的 `BUILD_SHARED_LIBS=...` define
- `[[dependencies]].build_type` 当前会进入 `cstow.lock`，并用于区分 `fetch` / `install` / cache 路径 / registry artifact key
- `[[dependencies]]` 已支持递归安装，`cstow install` 会自动构建 recipe 的传递依赖

### `source` 字段当前状态

- `type = "git"`
  - 已支持
- `type = "archive"`
  - 已支持 `.tar.gz` / `.tgz` / `.zip`；其他格式尝试调用系统 `tar`
  - 支持 `SHA256` 校验

### `artifacts` 字段当前状态

- `include_dirs`
  - 对 `header-only` 安装路径有实际作用
- `libs`
  - 已进入安装校验流程，`builder.ValidateInstall` 会检查声明的库文件是否存在
  - 支持 debug profile 的 `d` 后缀变体（如 `libprotobufd.a`）
  - 支持 shared 构建时搜索 `.so` / `.dylib` 变体

### `install_targets` 字段当前状态

已被解析并用于安装结果校验。

## 六、版本覆盖：`versions/<version>.toml`

当前 `VersionOverride` 支持这些字段：

```toml
patch = "1.14.0-fix-msvc.patch"

[source]
sha256 = "8ad598c73ad796e0d8280b082cebd82a630d73e73cd3c70057938a6501bba5d7"

[build]
type = "shared"

[build.cmake]
defines = [
  "BUILD_SHARED_LIBS=OFF",
  "INSTALL_GTEST=OFF",
  "GTEST_HAS_ABSL=OFF",
]
cxx_flags = ["-Wno-error"]
link_flags = ["-lpthread"]

[build.compiler.clang]
cxx_flags = ["-Wno-unused-private-field"]
```

### 当前生效规则

- `build.type`
  - 会覆盖 package 基础层的 `build.type`
- `build.cmake.defines`
  - 如果非空，会 **替换** 基础层/profile/compiler/platform 累积出来的 defines
- `build.cmake.cxx_flags`
  - 追加
- `build.cmake.link_flags`
  - 如果非空，会替换原有 link flags
- `build.compiler.<kind>`
  - 追加到对应编译器层
- `patch`
  - 自动应用（需系统已安装 `patch` 命令，且补丁文件存放在 `patches/` 目录下）
- `source.sha256`
  - `archive` 拉取时会进行强校验

## 七、Finder 行为

`Finder.Find(name, versionConstraint)` 的行为：

1. 根据包名首字母定位子目录
2. 按 repository 搜索顺序查找 `package.toml`（项目级仓库优先）
3. 使用 semver 选择满足约束的最高版本
4. 如果存在 `versions/<matched>.toml`，一并加载
5. 返回：
   - `PackageDef`
   - 解析后的具体版本号
   - 可选 `VersionOverride`
   - 命中的 repository 根路径

`Finder.Search(query)` 的行为：

1. 扫描所有 repository 路径（项目级优先）
2. 按包名过滤（模糊匹配，不区分大小写）
3. 去重（同名包取第一个匹配的仓库）
4. 返回包名、描述、最新版本、匹配的仓库路径

CLI: `cstow search <query>`（传空字符串列出所有包）

### 版本选择规则

- `*` 或空约束：取最高版本
- semver 范围：取满足范围的最高版本
- 不能解析为 semver constraint 时：按“精确版本字符串”匹配

## 八、Merge 行为

当前 `Merge(pkg, ver, toolchainKind, profile, goos)` 的优先级从低到高是：

1. package 基础层 `build.cmake`
2. `build.profile.<profile>`
3. `build.compiler.<toolchainKind>`
4. `build.platform.<goos>`
5. `versions/<ver>.toml` 中的 build override

输出结构：

```go
type MergedBuildConfig struct {
    System       string
    CMakeDefines []string
    CXXFlags     []string
    LinkFlags    []string
    IncludeDirs  []string
    Libs         []string
    Patch        string
    BuildType    string
}
```

### 当前已经真正进入构建路径的部分

- `BuildType`
- `CMakeDefines`
- `CXXFlags`
  - 作为 `CMAKE_CXX_FLAGS` 传给当前 CMake configure
- `LinkFlags`
  - 作为 `CMAKE_EXE_LINKER_FLAGS` / `CMAKE_SHARED_LINKER_FLAGS` / `CMAKE_MODULE_LINKER_FLAGS` 传给当前 CMake configure
- `IncludeDirs`
  - 仅 header-only 路径使用
- `Patch`
  - 源码构建前自动应用版本特定的 patch
- `Libs`
  - 安装后校验声明的库文件是否存在
- `install_targets`
  - 安装后校验目标是否与安装结果一致

### 当前还没有完整进入构建路径的部分

暂无 — 所有 Merge 输出字段均已接入构建链路。

## 九、`cstow install` 当前工作流

当前源码构建命令的行为是：

```text
cstow install fmt@^10

1. 读取 ~/.cstow/config.toml
2. 计算 repository 搜索路径
3. Finder.Find("fmt", "^10")
4. 探测 toolchain，并按 defaults.std 生成 ABI tag
5. 检查本地 cache 是否已有安装结果
6. Merge package + version override
7. 拉取源码
8. 使用 builder 进行安装
9. 输出安装目录 ~/.cstow/cache/<name>/<version>/<abi_tag>/<build_type>/
```

### 当前 builder 的实际能力

- CMake configure / build / install
- `build.type = "static"` / `"shared"` 会切换 `BUILD_SHARED_LIBS`
- `cstow install --type shared|static` 会优先于 recipe 自带的 `BUILD_SHARED_LIBS=...` define
- merged `CXXFlags` / `LinkFlags` 会传给当前 CMake configure
- 源码构建前自动应用版本特定的 patch
- 安装后校验 `libs` 文件存在（支持 debug profile `d` 后缀和 shared `.so`/`.dylib` 变体）
- 递归构建 recipe 依赖，共享依赖自动传播 `-fPIC`（transitive `ForceShared`）
- `header-only` 复制 `include_dirs`
- `cmd/install` 的集成测试已覆盖本地 static / shared 库安装，以及同 ABI 下 static / shared 分目录共存（见 `cmd/install_integration_test.go`）

### 当前 builder 还不支持或未打通的部分

- `make` / `autoconf` / `meson` / `custom`
- 将 `install` 结果自动接入项目的 `cstow_deps`

## 十、cache / lock / registry 的 `build_type`

当前实现里，`build_type` 已经进入这几层：

- `cstow.toml`
  - `[[dependencies]].build_type`
- `cstow.lock`
  - `[[package]].build_type`
- 本地 cache
  - 新路径为 `~/.cstow/cache/<name>/<version>/<abi_tag>/<build_type>/`
  - 读取时兼容旧路径 `~/.cstow/cache/<name>/<version>/<abi_tag>/`
- registry artifact key
  - 新对象 key 为 `<pkg>/<version>/<abi_tag>/<build_type>.tar.zst`
  - 下载时兼容旧 key `<pkg>/<version>/<abi_tag>.tar.zst`
- manifest
  - `artifact.build_type` 已写入元数据
  - `fetch` 会在 manifest 可用时按 `abi_tag + build_type` 选 artifact；取不到 manifest 时再回退到旧 key 猜测

当前优先级：

1. CLI 显式覆盖，例如 `cstow install --type shared`
2. `cstow.lock` 里的 `build_type`
3. `cstow.toml` 中 dependency 的 `build_type`
4. repository recipe 的 `build.type`
5. 最后回落到 `static`

对 `fetch` 来说，只有前 2-3 层属于“显式配置”。如果 lock/config 里没有 `build_type`，当前实现会先按未指定状态查 cache / registry 旧路径；只有走到 source fallback 时，才由 repository recipe 决定最终构建类型。

## 十一、推荐把 repository 系统理解成什么

截至现在，最准确的理解方式是：

- `repository` 已具备完整的 recipe 数据模型、查找、版本覆盖、merge、递归依赖、patch 应用和源码构建
- 它已作为项目依赖管理主路径中的自动 fallback（`fetch` 预编译缺失时回退到 repository 源码构建）
- 支持项目级仓库 (`<project>/.cstow/repository/`)、全局仓库和包搜索

所以如果你要继续推进这个系统，优先级应该是：

1. 优化 workspace 下的共享 lock / cache 逻辑
2. 实现 cache 清理策略
3. 支持更多构建后端 (Meson 等)

## 十二、`hash_id` / `build_tags` 与按哈希操作

当前实现里，artifact 元数据新增了：

- `hash_id`
  - 内容寻址 ID；第一版中它就是上传包（`.tar.zst`）的完整 SHA-256 十六进制串
  - `publish` 打包后按上传字节计算 SHA-256，并同时写入 manifest 的 `hash_id` 与 `sha256`
- `build_tags`
  - 额外构建标签元数据（来自 `publish --build-tag key=value`）
  - 会写入 manifest 与本地 SQLite artifact 索引

与 `hash_id` 相关的 CLI：

- `cstow artifact show <hashid>`
  - 在本地 SQLite 索引中按完整 `hash_id` 或唯一前缀查询并展示 artifact 元数据
- `cstow fetch --artifact <hashid>`
  - 按完整 `hash_id` 或唯一前缀拉取
  - 若本地索引命中且路径存在，直接复用本地 artifact 并链接到 `./cstow_deps`
  - 否则会基于 lock/config 中可候选的包版本加载 manifest，用 `hash_id` 匹配后下载并写回本地索引

## 十三、与其他文档的关系

- `AGENTS.md`
  - 面向代理/开发协作，强调当前真实能力边界
- `PLAN.md`
  - 面向路线图，说明下一阶段优先级
- `README.md`
  - 面向 repository 系统本身的“当前可用规格”

如果三者冲突，以当前代码行为为准，并及时回写文档。
