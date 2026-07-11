# OpenList 4.2.2 后续改动同步实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 OpenList `3b1760e9..eb486712` 的累计业务代码改动同步到 PowerList，同时保持本仓库构建脚本和 GitHub Actions 完全不变。

**Architecture:** 对已确认的上游范围生成累计差异，并通过 Git 三方应用融合到 PowerList。保护路径从补丁输入中排除，冲突逐文件保留 PowerList 定制并接入上游逻辑，最后以保护文件对象校验、依赖一致性和全量 Go 测试验证结果。

**Tech Stack:** Git、Go、gofmt、Go modules

---

### Task 1: 固定基线与保护清单

**Files:**
- Inspect: `.github/workflows/**`
- Inspect: `build.sh`
- Inspect: `build/**`
- Inspect: `Dockerfile*`
- Create temporary snapshot: `/tmp/powerlist-openlist-sync-protected.before`

- [ ] **Step 1: 验证同步端点**

```bash
git rev-parse 3b1760e9 openlist/main
git rev-list --count 3b1760e9..openlist/main
```

Expected: 起点为 `3b1760e9...`，终点为 `eb486712...`，范围包含 75 个提交。

- [ ] **Step 2: 记录保护文件对象 ID**

```bash
git ls-files -s -- '.github/workflows/**' 'build.sh' 'build/**' 'Dockerfile*' '**/Dockerfile*' > /tmp/powerlist-openlist-sync-protected.before
```

Expected: 文件列出 PowerList 当前受保护的已跟踪文件及 blob ID。

- [ ] **Step 3: 确认上游范围内的构建类路径**

```bash
git diff --name-status 3b1760e9..openlist/main -- '.github/workflows/**' 'build.sh' 'build/**' 'Dockerfile*' '**/Dockerfile*'
```

Expected: 仅用于确认将被排除的上游改动，不修改工作区。

### Task 2: 应用上游累计差异

**Files:**
- Modify: `drivers/**`
- Modify: `internal/**`
- Modify: `pkg/**`
- Modify: `server/**`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: 上游范围内的普通文档和配置文件
- Preserve: `.github/workflows/**`, `build.sh`, `build/**`, `Dockerfile*`

- [ ] **Step 1: 三方应用排除保护路径后的累计差异**

```bash
git diff --binary 3b1760e9..openlist/main -- . ':(exclude).github/workflows/**' ':(exclude)build.sh' ':(exclude)build/**' ':(exclude)Dockerfile*' ':(exclude)**/Dockerfile*' | git apply --3way --index -
```

Expected: 无冲突文件自动进入暂存区；有冲突时 Git 标记冲突路径，保护路径不出现于暂存区或工作区差异。

- [ ] **Step 2: 枚举并逐项解决冲突**

```bash
git diff --name-only --diff-filter=U
```

Expected: 对每个输出文件比较 stage 1/2/3 版本；保留 PowerList 定制逻辑并融合上游变更，移除所有冲突标记后仅暂存本次同步文件。

- [ ] **Step 3: 检查没有构建或 Actions 路径进入差异**

```bash
git diff --name-only HEAD -- '.github/workflows/**' 'build.sh' 'build/**' 'Dockerfile*' '**/Dockerfile*'
```

Expected: 无输出。

### Task 3: 格式化与依赖一致性

**Files:**
- Modify: 本次同步涉及的 `*.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 格式化本次新增或修改的 Go 文件**

```bash
gofmt -w $(git diff --name-only --diff-filter=ACMR HEAD -- '*.go')
git add -- $(git diff --name-only --diff-filter=ACMR HEAD -- '*.go')
```

Expected: 所有合入 Go 文件符合 gofmt，且不触碰未跟踪文件。

- [ ] **Step 2: 整理依赖并重新暂存**

```bash
go mod tidy
git add go.mod go.sum
```

Expected: 命令退出码为 0，`go.mod` 与 `go.sum` 包含全部合入代码所需依赖。

- [ ] **Step 3: 检查补丁格式**

```bash
git diff --cached --check
```

Expected: 无空白错误或残留冲突标记。

### Task 4: 验证同步结果

**Files:**
- Verify: 全部暂存改动
- Verify: `/tmp/powerlist-openlist-sync-protected.before`

- [ ] **Step 1: 运行全量 Go 测试**

```bash
go test ./...
```

Expected: 所有包通过；若存在环境或基线失败，记录具体包和错误，不将其描述为通过。

- [ ] **Step 2: 再次校验保护文件对象 ID**

```bash
git ls-files -s -- '.github/workflows/**' 'build.sh' 'build/**' 'Dockerfile*' '**/Dockerfile*' > /tmp/powerlist-openlist-sync-protected.after
diff -u /tmp/powerlist-openlist-sync-protected.before /tmp/powerlist-openlist-sync-protected.after
```

Expected: `diff` 无输出且退出码为 0。

- [ ] **Step 3: 审核最终文件列表和差异统计**

```bash
git status --short
git diff --cached --stat
git diff --cached --name-status
```

Expected: 用户原有未跟踪文件仍保持未跟踪；暂存区只包含上游同步代码、测试、依赖和普通文档，不包含保护路径。

### Task 5: 创建同步提交

**Files:**
- Commit: 所有已验证的暂存改动

- [ ] **Step 1: 提交同步结果**

```bash
git commit -m "merge: sync OpenList changes after 4.2.2"
```

Expected: 创建单个同步提交，不包含用户原有未跟踪文件。

- [ ] **Step 2: 提交后最终验证**

```bash
git status --short --branch
git show --stat --oneline --summary HEAD
git diff HEAD^ HEAD --name-only -- '.github/workflows/**' 'build.sh' 'build/**' 'Dockerfile*' '**/Dockerfile*'
```

Expected: 分支领先远端两个提交（设计文档和同步提交），最后一条保护路径检查无输出。
