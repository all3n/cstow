# cstow 路线图

更新时间：2026-04-02

这份计划不再沿用旧版“Phase 1-7 全部完成”的写法，而是按当前代码现实拆成三类内容：

- 已落地能力
- 已实现但尚未闭环的能力
- 下一阶段的真实优先级

目标是让后续开发围绕“把现有能力打通”推进，而不是继续在未收敛的基础上横向扩张。

## 一、当前项目状态

### 1. 已落地能力

- CLI 已具备基础命令：`init`、`build`、`add`、`fetch`、`publish`、`install`、`migrate`、`ci`、`workspace`、`checkabi`
- `cstow.toml` 解析已支持项目配置、workspace、hooks 等字段
- `~/.cstow/config.toml` 全局配置已支持 repository 路径、缓存、工具链偏好等基础能力
- 工具链检测和 ABI 计算已可用
- `cstow.lock` 的基础生成与读取已可用，并已记录 dependency `build_type`
- S3/R2/MinIO 风格 registry 的上传、下载、manifest 读写已具备基础实现
- `internal/repository/` 已实现 package definition、版本匹配、override 加载、merge 逻辑
- `cstow install` 已可基于 repository recipe 走源码构建路径
- `cmd/install` 当前已有集成测试覆盖 static / shared CMake 库的本地安装
- cache / registry artifact key 已按 `<abi>/<build_type>` 区分，旧路径和旧 key 仍可回读
- local artifact metadata 已在 `~/.cstow/cstow.db` 中用 SQLite 索引，支持 `hash_id` 和 `build_tags`
- `cstow artifact list` / `cstow artifact sync` / `cstow artifact show <hashid>` 已可用
- `workspace`、`legacy migrate`、`hooks`、`.tar.zst` 打包/解包 已有初版实现

### 2. 已实现但未闭环

这些是目前最容易误导后续开发的部分。

- `add` 仍然只是更新项目依赖和 lock，不会校验依赖是否真的存在于 repository 或 registry
- `build` 构建的是“当前项目”，不会直接消费 repository recipe 来自动构建第三方依赖
- `fetch` 已支持 registry 缺失时回退到 repository source build，并会在 manifest 可用时按 `abi + build_type` 选 artifact
- `install` 虽然会 merge repository 配置，但还没有递归构建 recipe 依赖
- version override 中的 `patch` 已进入 merged config，但实际构建前没有应用补丁
- `archive` 源码拉取还没有实现，当前基本只支持 git source
- merged `CXXFlags` / `LinkFlags` 已进入当前 `install` 的 CMake configure 链路，但 `artifacts` / `install_targets` 还没有形成安装结果校验闭环
- `build` 命令对项目 `build.defines`、`build.sources`、profile/hook 生命周期的利用还比较浅
- `fetch` 已开始使用 manifest 做 artifact 选择，下载后 SHA256 校验已实现
- workspace 目前是串行构建，没有成员依赖排序、共享 lock、并行调度

### 3. 当前不建议继续扩张的方向

在主路径没闭环前，这些方向应该降级处理。

- 新增更多构建后端，如 `meson` / `autoconf` / `custom`
- 继续扩展更多 CLI 子命令，而不先统一现有依赖流
- 把 repository 远程同步、自动更新、团队分发做得很重
- 在缺少稳定 ABI / manifest 选择逻辑前继续强化 registry 复杂特性

## 二、核心判断

项目方向本身没有问题，但计划内容需要调整。

旧计划的问题主要有两个：

1. 它把很多“模块已存在”的内容等同于“用户路径已打通”。
2. 它没有把 `registry` 和 `repository` 两条依赖获取路径的关系讲清楚。

当前最重要的不是再增加功能点，而是明确并打通下面这条主链路：

`项目声明依赖 -> 解析 -> 选择预编译或源码构建 -> 放入本地缓存/依赖前缀 -> 项目构建成功`

## 三、建议的新阶段划分

## Phase A — 打通主用户路径

**目标**：让用户可以稳定完成“声明依赖、拿到依赖、构建项目”。

### 重点任务

- 明确依赖获取策略：
  - 先尝试 registry 预编译包
  - 缺失时回退到 repository recipe + source build
- 让 `add` 在写入配置前至少做基本校验：
  - registry 中是否存在版本
  - 或 repository 中是否存在 recipe
- 让 `build` 对缺失依赖给出明确动作建议，或直接触发受控的补全流程
- 补齐项目构建路径中对 profile、defines、hooks 的使用
- 增加覆盖真实命令链路的集成测试

### 完成标准

- 一个最小示例项目可以稳定跑通：
  - `cstow init`
  - `cstow add`
  - `cstow fetch` 或 `cstow install`
  - `cstow build`

## Phase B — 完成 repository source-build MVP

**目标**：把 `cstow install` 从“能跑一部分 recipe”提升为可维护的最小成品。

### 重点任务

- 实现 `archive` source 下载和解包
- 在源码构建前应用 version / compiler / platform patch
- 递归处理 repository package 自身依赖
- 让 merged `CXXFlags` / `LinkFlags` 在 `build` / `install` 两条链路上保持一致，并补上安装结果校验
- 校验 `artifacts` / `install_targets` 是否与安装结果一致
- 改善构建失败时的错误信息和临时目录排查体验

### 完成标准

- git source 和 archive source 都能安装
- recipe 依赖和 patch 都能生效
- 至少覆盖 `static`、`shared`、`header-only` 三种常见类型

## Phase C — 强化 registry、lock 与缓存

**目标**：让预编译分发路径足够可靠。

### 重点任务

- lock 中写入并使用 ABI 信息
- 根据 manifest 做 artifact 选择，而不是直接假定 `abiTag.tar.zst`
- 下载后做 SHA256 校验
- 明确多 registry、只读 registry、profile 凭证的行为
- 实现 cache 清理策略，真正利用全局配置中的缓存参数

### 完成标准

- 同一包多 ABI 产物可正确选择
- fetch 不再依赖“默认 abiTag”猜测
- 缓存能被检查、复用和清理

## Phase D — workspace 与开发体验

**目标**：把现有的辅助能力从“占位版”提升到实际可用。

### 重点任务

- workspace 成员依赖排序
- workspace 并行构建与失败收敛
- 统一 workspace 下的 lock / cache / build 输出体验
- 完善 `migrate` 生成结果和 `ci` 模板
- 补充文档与示例仓库布局

### 完成标准

- 多成员 workspace 可以稳定构建
- CI 模板能直接服务当前主路径
- 文档能解释 registry 与 repository 的职责边界

## Phase E — 扩展项

这些内容应在前四个阶段稳定后再推进。

- `make` / `autoconf` / `meson` / `custom` builder
- repository 远程同步、镜像、自动更新
- 更细粒度的 package authoring / lint / doctor 命令
- Windows/MSVC 的更完整端到端验证

## 四、近期建议执行顺序

如果只看接下来一到两个迭代，建议按下面顺序推进：

1. 明确并实现“预编译优先，源码回退”的依赖获取策略
2. 补齐 `build` / `install` 中对 merged flags、hooks、缺失依赖处理的闭环
3. 完成 archive source、patch 应用、recipe 依赖递归
4. 完成 manifest/ABI/校验和驱动的 fetch 逻辑
5. 最后再处理 workspace 并行、更多 builder、更多 CLI 扩展

## 五、文档维护要求

后续更新计划时，遵守下面规则：

- 不要把“模块存在”写成“功能完成”
- 不要把 design doc 里的能力默认视为代码已实现
- 每个阶段必须同时写清：
  - 目标用户路径
  - 当前阻塞点
  - 完成标准
- `AGENTS.md`、`PLAN.md`、`repo.md` 三者需要保持语义一致
