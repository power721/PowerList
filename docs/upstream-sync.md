# 定期同步 OpenList 上游指南

本文记录 PowerList 定期同步 OpenList 上游的标准流程。目标是在引入上游功能、安全修复和依赖更新的同时，保留 PowerList 自有功能，并避免覆盖本仓库的发布与 CI 配置。

## 同步原则

- 后端同步来源为 `OpenListTeam/OpenList` 的 `main` 分支。
- 每次从“上一次已合入 PowerList 的最后一个上游提交”开始，只同步其后的累计改动，避免重复应用已经 squash 合入的提交。
- PowerList 自有驱动、索引、缓存、播放和 CAS 行为优先保留；冲突时融合上游修复，不使用整文件覆盖丢弃本地定制。
- `.github/workflows/**` 保持 PowerList 版本，不引入上游 GitHub Actions 改动或新增工作流。
- 构建脚本的逻辑保持 PowerList 版本。`build.sh` 中固定的 `webVersion` 是例外：每次同步必须更新为本次 OpenList 最新稳定版本。
- `go.mod` 和 `go.sum` 是业务代码依赖清单，需要随上游代码同步并执行 `go mod tidy`。
- 用户已有的未跟踪文件不参与同步，不删除、不覆盖、不提交。

## 1. 检查工作区和远端

```bash
git status --short --branch
git remote -v
git fetch openlist --tags --prune
```

确认：

- 当前分支和预期一致。
- 工作区中的已有改动已识别并受到保护。
- `openlist/main` 已刷新到本机 OpenList 仓库或官方上游的最新状态。

如果尚未配置上游远端：

```bash
git remote add openlist https://github.com/OpenListTeam/OpenList.git
git fetch openlist --tags
```

## 2. 确定后端同步范围

不要仅根据版本标签猜测起点。PowerList 通常以 squash 方式同步上游，因此上游 tag 未必是当前分支的祖先。

先查看最近一次同步提交的说明和历史：

```bash
git log --oneline --decorate -20
git log --graph --oneline --decorate --all --simplify-by-decoration -80
```

确定上次已包含的最后一个上游提交，记为 `<last-upstream-sha>`。检查新的同步范围：

```bash
git rev-parse <last-upstream-sha> openlist/main
git rev-list --count <last-upstream-sha>..openlist/main
git log --oneline <last-upstream-sha>..openlist/main
git diff --stat <last-upstream-sha>..openlist/main
git diff --name-status <last-upstream-sha>..openlist/main
```

同步文档和提交信息必须记录实际的起止 SHA，便于下一次继续。

## 3. 确定 Web 版本

OpenList-Frontend 的最新版本与 OpenList 当前最新稳定版本保持一致，不需要单独查询 Frontend 仓库。

获取本次 OpenList 同步目标对应的最新稳定 tag：

```bash
git describe --tags --abbrev=0 openlist/main
```

也可以查看同步范围内的版本 tag：

```bash
git tag --contains <last-upstream-sha> --merged openlist/main --sort=-v:refname
```

将 OpenList 最新稳定 tag 原样记录到同步记录。`build.sh` 的 `webVersion` 使用去掉开头 `v` 的版本号。例如 OpenList 最新稳定 tag 为 `v4.2.3` 时，写入值为 `4.2.3`：

```bash
webVersion=<latest-openlist-version-without-v>
```

```bash
webVersion=4.2.3
```

只修改该版本值。不得借同步之机改写下载、打包、静态链接检查或其他构建逻辑。

## 4. 建立保护清单

应用上游补丁前记录受保护文件：

```bash
git ls-files -s -- \
  '.github/workflows/**' \
  'build/**' \
  'Dockerfile*' \
  '**/Dockerfile*'
```

`build.sh` 需要单独处理：除 `webVersion` 更新为本次 OpenList 最新稳定版本外，其余内容必须保持同步前版本。

先检查上游涉及哪些受保护路径：

```bash
git diff --name-status <last-upstream-sha>..openlist/main -- \
  '.github/workflows/**' \
  'build.sh' \
  'build/**' \
  'Dockerfile*' \
  '**/Dockerfile*'
```

## 5. 应用累计改动

推荐应用累计差异并形成一个同步提交。这与 PowerList 既有的 squash 同步方式一致，也能减少逐提交反复解决相同冲突的成本。

```bash
git diff --binary <last-upstream-sha>..openlist/main -- . \
  ':(exclude).github/workflows/**' \
  ':(exclude)build.sh' \
  ':(exclude)build/**' \
  ':(exclude)Dockerfile*' \
  ':(exclude)**/Dockerfile*' \
  | git apply --3way --index -
```

补丁完成后，再单独把 `build.sh` 的 `webVersion` 更新到第 3 节确定的 OpenList 最新稳定版本。

如果上游修改了 PowerList 中不存在的子系统文件，应先确认：

- 该子系统是否已被 PowerList 删除或替代。
- 上游修复是否仍适用于 PowerList 的替代实现。
- 是否需要迁移修复，而不是重新引入整个已删除子系统。

确认不适用后，才可从补丁输入中排除对应路径，并在同步记录中说明原因。

## 6. 解决冲突

列出冲突：

```bash
git diff --name-only --diff-filter=U
git ls-files -u
```

逐文件比较三方版本：

```bash
git show :1:path/to/file
git show :2:path/to/file
git show :3:path/to/file
```

处理顺序：

1. 理解上游提交要解决的问题。
2. 判断 PowerList 是否已有等价实现。
3. 保留 PowerList 独有接口和行为。
4. 融合仍适用的上游安全修复、错误处理和新功能。
5. 对函数签名、依赖版本和调用方做全局搜索，避免只解决冲突标记而留下接口不一致。

禁止简单地对所有冲突统一选择 `ours` 或 `theirs`。

## 7. 格式化和依赖整理

```bash
gofmt -w <本次修改的 Go 文件>
go mod tidy
git diff --check
```

如果 Go 缓存目录在沙箱中不可写，可以只为命令设置可写缓存：

```bash
GOCACHE=/tmp/powerlist-go-build-cache go test ./...
```

不要通过修改构建脚本绕过本机环境问题。

## 8. 验证

至少执行：

```bash
go test ./...
git diff --check
git diff --name-only --diff-filter=U
```

如果全量测试在同步前已经失败，应先保存合并前基线，再比较合并后的失败集合。必须明确区分：

- 本次同步新增的失败：必须修复后再提交。
- 合并前已存在的失败：可以在用户明确同意后继续，但必须在提交或 PR 中如实记录。
- 环境依赖失败：例如缺少系统头文件或外部测试服务，必须记录具体依赖，不能描述为测试通过。

对新增功能和冲突区域运行相关包测试。例如：

```bash
go test ./drivers/189pc ./drivers/115_open ./drivers/123
go test ./server/mcp ./server/s3 ./internal/net
```

## 9. 验证保护项

确认 GitHub Actions 没有变化：

```bash
git diff --name-only <base>..HEAD -- '.github/workflows/**'
```

确认构建文件只有允许的 Web tag 更新：

```bash
git diff <base>..HEAD -- build.sh build Dockerfile Dockerfile.ci Dockerfile.ci-host
```

预期：

- `.github/workflows/**` 无输出。
- `build/**` 和 Dockerfile 无输出。
- `build.sh` 最多只有 `webVersion=<latest-openlist-version-without-v>` 一处变化。

## 10. 提交与 PR

推荐结构：

1. 可选：同步设计文档提交。
2. 可选：实施计划提交。
3. 上游同步提交。

同步提交示例：

```text
chore(upstream): sync OpenList after vX.Y.Z

- Merge upstream changes through <upstream-head-sha> while preserving PowerList customizations.
- Keep GitHub Actions and PowerList build logic unchanged.
- Update build.sh webVersion to the current OpenList stable version.
- Add upstream features, security fixes, tests, and dependency updates.
```

创建 PR 前确认：

```bash
git log --oneline origin/main..HEAD
git diff --stat origin/main..HEAD
git diff origin/main..HEAD -- '.github/workflows/**'
git diff origin/main..HEAD -- build.sh build Dockerfile Dockerfile.ci Dockerfile.ci-host
```

PR 描述必须包含：

- 后端同步起止 SHA。
- OpenList 最新稳定 tag 及其对应的 `webVersion`。
- 被保护和被排除的文件。
- 主要冲突及解决原则。
- 实际执行的测试和未通过原因。
- AI 使用披露（如适用）。

## 11. 同步记录模板

每次同步在 PR 描述或维护记录中填写：

```markdown
## Upstream sync record

- Sync date: YYYY-MM-DD
- Previous upstream SHA: <sha>
- New upstream SHA: <sha>
- OpenList release/tag reference: <tag or range>
- OpenList latest stable tag: <tag>
- build.sh webVersion: <version without v>
- Protected paths: .github/workflows/**, build/**, Dockerfile*
- Allowed build change: build.sh webVersion=<version without v>
- Excluded non-applicable upstream paths: <paths and reasons>
- PowerList customizations retained: <summary>
- Conflict resolutions: <summary>
- Tests passed: <commands>
- Known baseline/environment failures: <failures>
- PR: <url>
```

## 本次同步示例

- 同步日期：2026-07-11 至 2026-07-12。
- 已包含的最后一个上游提交：`3b1760e9`。
- 新上游提交：`eb486712`。
- 同步提交范围：75 个上游提交。
- OpenList 最新稳定 release tag：`v4.2.3`。
- GitHub Actions：未修改。
- 构建变更：仅将 `build.sh` 的 `webVersion` 从 `4.2.2` 更新为 `4.2.3`。
- PR：<https://github.com/power721/PowerList/pull/26>。
