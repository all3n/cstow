# cstow 已知问题与待办列表 (ISSUES)

此文件记录了在测试流程中发现的待修复 Bug 和待增强功能。

## 1. 依赖管理 (Dependency UX)

### [FIXED] `cstow add` 无法更新现有依赖
- **现象**：如果 `cstow.toml` 中已存在同名包，执行 `add` 会提示“already present”并跳过，无法更新版本或更改来源（如从 registry 切换到 git）。
- **优先级**：高

### [ENHANCEMENT] Git 源码在 lock 文件中的版本记录薄弱
- **现象**：Git 依赖在 `cstow.lock` 中经常被记录为 `0.0.0`，即使指定了 `@version`。这导致缓存路径缺乏辨识度。
- **预期**：如果用户指定了 `@version`，应将其作为逻辑版本写入 lock，而非强制降级为 `0.0.0`。
- **优先级**：高

### [FEATURE] 缺少临时仓库/注册表标志 (`--repository`, `--registry`)
- **现象**：`add`、`fetch`、`install` 等命令必须依赖全局或项目配置，无法在命令行临时指定一个测试用的仓库路径。
- **优先级**：中

## 2. Workspace 工作流 (Workspace Workflow)

### [FEATURE] 缺少 `workspace init` 和 `workspace add`
- **现象**：目前必须手动编辑 `cstow.toml` 来配置 workspace。缺少 CLI 命令来快速初始化根项目或添加子模块。
- **优先级**：中

### [ENHANCEMENT] 并行构建日志交织
- **现象**：`workspace build --jobs N` 时，各模块的 CMake 输出混杂在一起，失败时难以定位是哪个模块报错。
- **预期**：引入按模块缓冲日志，或在失败时清晰打印该模块的最后 10 行输出。
- **优先级**：低

## 3. 维护与健壮性 (Maintenance & Robustness)

### [FIXED] `artifact sync` 对 "default" 构建类型的处理不一致
- **现象**：`sync` 逻辑可能会清理掉 `build_type` 为空（旧版本或 Header-only）的有效条目，导致本地索引失效。
- **优先级**：中

### [FEATURE] `cstow doctor` 环境诊断工具
- **现象**：缺少一键检测编译器、CMake、S3 凭据和缓存目录状态的手段。
- **优先级**：低
