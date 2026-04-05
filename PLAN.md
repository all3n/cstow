# cstow 路线图

更新时间：2026-04-06

这份计划不再沿用旧版“Phase 1-7 全部完成”的写法，而是按当前代码现实拆成三类内容：

- 已落地能力
- 已实现但尚未闭环的能力
- 下一阶段的真实优先级

目标是让后续开发围绕“把现有能力打通”推进，而不是继续在未收敛的基础上横向扩张。

## 一、当前项目状态

### 1. 已落地能力

- CLI 已具备基础命令：`init`、`build`、`add`、`fetch`、`publish`、`install`、`migrate`、`ci`、`workspace`、`checkabi`、`artifact`、`search`、`gen`、`clean`
- `cstow.toml` 解析已支持项目配置、workspace、hooks 等字段，支持 git 源码依赖配置（`cmake` 选项）
- `~/.cstow/config.toml` 全局配置已支持 repository 路径、缓存、工具链偏好等基础能力
- 工具链检测和 ABI 计算已可用
- `cstow.lock` 已记录 dependency `build_type` 和 `abi_tag`，支持 `git` 和 `local` 来源
- S3/R2/MinIO 风格 registry 的上传、下载、manifest 读写已具备，支持 `hash_id` 和 `build_tags` 索引
- `internal/repository/` 已实现 package definition、版本匹配、override 加载、merge 逻辑、包搜索
- `cstow install` / `cstow fetch` 已支持 repository recipe 和 Git 仓库的源码构建路径
- `cstow search <query>` 已支持在 repository 路径中按名称搜索包
- Repository 路径支持项目级 `.cstow/repository/`（最高优先级）
- 共享依赖自动传播 `-fPIC`（transitive `ForceShared`）
- `archive` 源码拉取和解包已支持 `.tar.gz`、`.zip` 和系统 `tar` 格式
- 源码构建前已支持自动应用版本特定的 `patch` 补丁
- `artifact list` / `artifact sync` / `artifact show <hashid>` 已可用，基于 SQLite 索引
- `workspace` 已支持基于拓扑排序的顺序构建和并行调度 (`--jobs`)
- `cstow gen` 已支持为 workspace 项目生成 `CMakeLists.txt` 和 `CMakePresets.json`
- Builder 已支持 debug profile 库名校验（`d` 后缀）和 shared 库变体搜索

### 2. 已实现但未闭环

这些是目前最容易误导后续开发的部分。

- `build` 命令对项目 `build.defines`、`build.sources` 的深度利用还不够，目前主要依赖原生 CMake
- `install` 虽然会递归构建依赖，但在极其复杂的循环依赖场景下可能还欠缺健壮性
- workspace 的 lock/cache 共享逻辑可以进一步优化
- `migrate` 生成结果仍需人工微调
- `cstow gen` 生成的 CMake 文件仍较基础，需要更多项目验证

### 3. 当前不建议继续扩张的方向

在主路径没闭环前，这些方向应该降级处理。

- 新增更多构建后端，如 `meson` / `autoconf` / `custom`
- 继续扩展更多 CLI 子命令，而不先统一现有依赖流
- 把 repository 远程同步、自动更新、团队分发做得很重

## 二、核心判断

项目主链路已基本打通：`项目声明依赖 -> 解析 -> 选择预编译或源码构建 (git/repo/registry) -> 放入本地缓存/依赖前缀 -> 项目构建成功`。

下一阶段的重点是：**并行化、健壮性与开发体验优化**。

## 三、阶段进展

## Phase A — 打通主用户路径 (已完成)

**目标**：让用户可以稳定完成“声明依赖、拿到依赖、构建项目”。

- 明确依赖获取策略 (Done: 预编译优先，源码回退)
- 让 `add` 在写入配置前做基本校验 (Done: 支持 registry/repo 校验，git 源码跳过校验)
- 让 `build` 支持自动补全依赖 (Done: `build --fetch`)
- 补齐项目构建路径中对 profile、defines、hooks 的使用 (Done)

## Phase B — 完成 repository & Git source-build (已完成)

**目标**：把源码构建路径提升为可用的成品。

- 实现 `archive` / `git` source 下载 (Done)
- 在源码构建前应用 patch (Done)
- 递归处理 repository package 自身依赖 (Done)
- 校验 `artifacts` / `install_targets` 与安装结果一致 (Done)

## Phase C — 强化 registry、lock 与缓存 (已完成)

**目标**：让预编译分发路径足够可靠。

- lock 中写入并使用 ABI 信息 (Done)
- 根据 manifest 做 artifact 选择 (Done)
- 下载后做 SHA256 校验 (Done)
- 支持 `hash_id` 和 `build_tags` (Done)

## Phase D — workspace 与并发构建 (已完成)

**目标**：提升多模块开发体验。

### 重点任务

- [x] workspace 成员依赖排序 (拓扑排序) (Done)
- [x] workspace 并行构建 (基于依赖关系的并行调度) (Done: `cstow workspace build --jobs N`)
- [x] 实现 Cache 清理策略 (Prune by age and size) (Done: `cstow artifact prune`)
- [x] 优化 workspace 下的共享 lock / cache 逻辑 (Done: workspace build 统一 fetch + project-level flock)
- [x] 完善 `migrate` 生成结果和 `ci` 模板 (Done: migrate 支持 std 检测和 URL，ci 支持 workspace)
- [x] 补充文档与示例仓库布局 (Done: examples/git-dependency-demo)

### 完成标准

- 多成员 workspace 可以稳定、高效（并行）构建
- CI 模板能直接服务当前主路径

## Phase E — 扩展项 (待启动)

- 更多构建后端支持 (Meson 等)
- 实现 cache 清理策略 (MaxSizeGB / RetentionDays)
- Windows/MSVC 的更完整端到端验证
- 更细粒度的 package authoring / lint / doctor 命令

## 四、近期建议执行顺序

1. **实现 Workspace 并行构建**：利用已有的拓扑排序，引入 worker pool 实现并行构建。
2. **实现 Cache 清理策略**：真正利用全局配置中的 `max_size_gb` 和 `retention_days`。
3. **优化错误诊断**：在构建失败时提供更清晰的日志指引和环境快照。
4. **文档与示例更新**：同步最新的 Git source 依赖用法。

## 五、文档维护要求

- **严守 MVP 拆分**：新增的规划必须能独立形成“最小可行性产品”，且在逻辑上可闭环测试。
- **杜绝半成品提交**：规划的代码实现必须坚持“完整测好功能 就提交一次”的底线纪律。
- `AGENTS.md`、`PLAN.md`、`README.md` 三者需要保持语义一致。
