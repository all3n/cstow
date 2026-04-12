# cstow 路线图

更新时间：2026-04-11

这份计划以**当前仓库代码**和**2026-04-11 本地跑通的 `go test ./...` 基线**为准，不沿用旧的“Phase 全部完成”叙事。

目标只有两个：

- 把已经存在的主链路继续做实
- 明确哪些能力只是“能用”，哪些能力才算“闭环”

## 一、当前基线

### 1. 已验证事实

- `go test ./...` 在当前 Unix-like 环境下通过
- CLI 主命令已存在并可调用：`init`、`build`、`add`、`fetch`、`publish`、`install`、`migrate`、`ci`、`workspace`、`check-abi`、`artifact`、`search`、`gen`、`clean`、`doctor`
- 三条主依赖路径已经连通：
  - registry 预编译分发
  - repository recipe 源码构建
  - git source 直接依赖
- `workspace fetch|build|gen|clean`、`artifact list|sync|show|prune`、`doctor` 都已经有可用实现

### 2. 已落地能力

- `add` 支持 `registry|git|local|repository` 四种来源，git 支持 `--git-url`、`--tag`、`--cmake-define`
- `fetch` 支持 manifest 选择、`hash_id` 直拉、registry 缺失时源码回退（git / repository）
- `install` 支持 repository recipe 和 git source 构建，并支持共享依赖传播 `-fPIC`
- repository 递归安装现在会对当前调用链做显式循环依赖检测，并给出依赖环路径
- repository / git source 构建错误现在会带上 `package -> stage` 上下文，`build --fetch` 也会透传这条错误链
- registry 预编译路径里的 hash 校验、解包失败也已有 `package -> stage` 上下文，`build --fetch` 会透传
- `fetch --source-fallback=false` 在缺少可用预编译包时现在会直接失败，并带出 manifest 选择/下载阶段原因
- registry 未配置时，`fetch` 也会明确提示是在“无 registry”前提下回退源码，或在禁用 fallback 时直接失败
- 当 `fetch` 先尝试预编译、再回退源码且源码也失败时，最终错误现在会同时保留前因（预编译失败原因）和后果（源码失败原因）
- `fetch` 的 source fallback warning 与成功输出现在已经统一为同一套提示风格（含 `[built-source]` / `[cached-source]`）
- `publish` 已写入 `hash_id`、`sha256`、`build_tags`
- `artifact` 已基于 SQLite 建立本地索引，并支持 `sync/show/prune`
- `workspace` 已支持成员发现、依赖排序、并行构建、统一 fetch
- `gen` / `workspace gen` 已能从 `cstow.toml` 生成基础 CMake 文件
- `doctor` 已检查 CMake、编译器、缓存目录、artifact DB、registry 基础连通性
- `internal/builder/` 已支持 CMake 和 Autotools，支持 patch、debug/shared 产物校验
- `internal/repository/source.go` 已支持 `git` 和 `archive`（`.tar.gz`、`.zip`）

## 二、当前真正没闭环的点

下面这些不是“未来锦上添花”，而是下一阶段最该补的缺口。

### P0. repository / git 源码构建的健壮性还不够

- `installFromRepository` 已补上显式循环依赖检测，source-build 与 registry 主路径的大部分失败已带上 `package -> stage` 级别上下文；这一层现在更偏向“补更多真实场景覆盖”，而不再是主错误链缺失
- 复杂 recipe 失败时，用户看到的是“构建报错”，但不容易判断是：
  - 源码抓取失败
  - patch 失败
  - 依赖图冲突
  - 共享/静态类型不匹配

### P0. 跨平台闭环还不够，尤其是 Windows/MSVC

- 现在的测试基线主要是 Unix-like 环境
- 多个集成/E2E 测试仍会在以下场景跳过：
  - Windows/MSVC
  - short mode
  - 依赖外部网络或外部仓库
- 这意味着“代码可运行”不等于“Windows 主路径稳定”
- `ci` 命令当前只生成基础 GitHub Actions，且模板仍偏 Linux/gcc/clang 视角

### P1. 配置模型与实现存在漂移

- `~/.cstow/config.toml` 的 `cache.dir` 现在已经统一接入 resolver / fetch / artifact sync / doctor / clean，artifact DB 默认路径也会跟随 resolved cache 根的父目录
- `repositories[].git` / `branch` / `archive` / `auto_update` 仍属于“已解析但未真正进入主流程”的字段
- 全局 `[network]` 已接入 archive 源码下载和 registry 客户端构建；`git` 路径还没有完整复用
- 全局 `[build.flags]` 已接入 build / install / fetch 的源码构建主链路，但 `gen` 等辅助面还没有完整复用

### P1. 辅助命令还是基础版，不该被写成“已完成产品”

- `migrate` 目前本质上还是一个 **CMake bootstrap 扫描器**
  - 只支持 `--from cmake`
  - `FetchContent URL` 场景处理还比较粗
  - 生成结果仍需要人工复核
- `gen` / `workspace gen` 生成的是基础 CMake 模板，不等于“各种项目形态都验证过”
- `doctor` 当前覆盖的是基础环境诊断，还没有检查：
  - `git`
  - `patch`
  - `tar`
  - `make` / `ninja`
  - Autotools 所需工具
- 目前还没有 package authoring / lint 命令，把 recipe 编写错误前移暴露

### P2. 文档与示例还需要追上代码

- README / PLAN / AGENTS 需要继续同步，避免把“基础可用”写成“完全闭环”
- 缺少更系统的示例来覆盖：
  - Windows/MSVC
  - shared/debug 变体
  - archive + patch
  - Autotools
  - workspace 多模块

## 三、下一阶段按 MVP 拆分的真实执行顺序

### MVP-1：补齐源码构建依赖图闭环

目标：让 repository / git source-build 在复杂依赖场景下更稳，不再靠用户手工猜错因。

范围：

- 巩固 repository 递归安装的显式循环依赖检测
- 统一 `fetch` / `install` / `build --fetch` 的错误上下文
- 把 patch、源码下载、构建失败的诊断输出分层

完成标准：

- 有覆盖循环依赖的单元/集成测试
- 出错时能看出失败发生在哪一层（解析 / 下载 / patch / build）
- `install` 与 `fetch` 的依赖构建行为更一致

### MVP-2：补齐 Windows/MSVC 和 CI 主路径

目标：把“理论支持 MSVC”升级成“主路径被验证”。

范围：

- 补 Windows/MSVC 下的 `check-abi`、`fetch`、`install`、`build` 验证
- 调整 `ci` 生成模板，和当前 Go / toolchain 现实对齐
- 明确哪些测试必须联网，哪些测试应改成本地 fixture

完成标准：

- 至少一条 Windows/MSVC 主路径有可复现验证
- CI 模板不再与 `go.mod` 漂移
- 平台相关 skip 有清晰分类，而不是“统统跳过”

### MVP-3：清理配置字段与实现漂移

目标：让配置文档说的内容，和代码真正消费的内容一致。

范围：

- 统一 cache 根路径解析，贯通 resolver / artifactdb / doctor / fetch
- 决定全局 `[network]`、`[build.flags]`、`repositories[].git|branch|archive|auto_update` 的去留：
  - 要么接入主链路
  - 要么降级为未实现并在文档中明确
- 把“已解析未消费”的字段单独标注

完成标准：

- `cache.dir` 行为一致
- 配置字段分成“已生效 / 部分生效 / 未实现”三类并与代码一致
- 不再出现文档默认暗示“全都已支持”

### MVP-4：把辅助命令从基础版提升到可依赖

目标：让 `doctor`、`migrate`、`gen` 真正成为降低门槛的工具，而不是演示性质功能。

范围：

- 扩展 `doctor` 的工具链和构建工具检查
- 增强 `migrate` 的结果说明、警告和人工确认点
- 为 repository recipe 增加 lint / validate 命令
- 继续用真实样例校验 `gen` / `workspace gen`

完成标准：

- 用户能在构建前得到更明确的环境诊断
- recipe 作者能在构建前发现常见 schema / 文件布局错误
- `migrate` 输出能明确告诉用户哪些部分是自动识别、哪些部分需要手改

### MVP-5：补文档和示例闭环

目标：让新用户和后续 AI agent 都能沿着真实路径开发，不再被历史设计稿带偏。

范围：

- 同步 README / PLAN / AGENTS 的能力表述
- 增加与当前主路径一致的示例项目
- 把未实现项明确写成未实现，不再模糊表述

完成标准：

- 三份核心文档语义一致
- 每条主路径至少有一个能复现的示例
- 新 agent 读文档后不会把基础功能误判成完全完成

## 四、当前明确降级、不优先做的方向

- 再扩更多构建后端，尤其是在主路径健壮性没补齐前
- 继续横向扩 CLI 子命令，而不先收敛已有命令行为
- 做很重的 repository 远程同步 / 自动更新 / 团队分发机制
- 在没有 authoring/lint 的前提下先扩 package schema

## 五、文档维护规则

- 以后写“Done”必须满足三个条件：
  - 命令已接到 CLI
  - 有测试覆盖
  - 主路径在真实环境走通过
- 对 `migrate`、`gen`、`ci`、`doctor` 这类辅助命令，默认按“基础可用”表述，除非已经有跨项目验证
- `AGENTS.md`、`PLAN.md`、`README.md` 必须同时维护，避免语义漂移
