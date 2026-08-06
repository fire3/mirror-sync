# 增量下载（快照对比 + 新增下载 + 删除脚本）设计文档

> 更新日期：2026-07-27
> 设计决策依据用户讨论确认。
> 本功能改造现有 `incremental-download` 任务（TUI/CLI 入口已存在，本次补齐 diff 报告、新增下载与删除脚本能力）。

## 1. 概述

### 1.1 背景

`mirror-sync` 已有两条主链路：

1. `metadata-sync`：抓取全量 PyPI simple 元数据，落盘为 `snapshots/pypi-{date}/simple/{pkg}/index.html` + `index_v1.json`。
2. `artifact-download`：按单日快照全量下载包文件到 `{mirrorRoot}/pypi-{date}/packages/xx/xx/...`。

日常增量维护需要第三条链路：**对比两份快照，只处理增量**——下载新增/变更的 artifacts，并把"快照中已消失"的内容转成可人工审查的删除脚本，避免每天全量重下与全量重扫。

### 1.2 目标

1. 对比新旧两份 metadata snapshot，产出结构化 diff 报告。
2. 找出**新快照中删除的 package**，记录列表。
3. 对**新旧都存在的 package**，找出新增的 whl / 源码包 artifacts，记录其下载 URL。
4. 对**新旧都存在的 package**，找出被删除的 artifacts，记录下来。
5. 依据上述数据下载新增 artifacts——路径保持 `packages/xx/xx/...`，与 `artifact-download` 一致，**复用现有下载器**。
6. 对删除的 packages 与 artifacts 生成**删除脚本**（只生成，不执行）。

### 1.3 与既有 incremental-download 骨架的关系

现有 `runner.runIncrementalDownload` + `planner.DiffSnapshotManifests` 已具备：

- 双快照加载（`loadManifestByDate`：优先 `manifest.json`，缺失则从 `simple/` 构建）
- artifact 维度 diff（`added / changed / removed / unchanged`，key 为 `RelativePath`）
- `BuildDownloadPlan` 取 `added + changed` 下载

本次改造补齐缺口：

| 缺口 | 现状 | 本设计 |
|---|---|---|
| 删除的 package 列表 | 无 | `diff-report.json` 的 `removedPackages` |
| 删除 artifacts 清单 | `diff.Removed` 仅存内存，未落盘 | `diff-report.json` + `removed-artifacts.jsonl` |
| 新增 artifacts 的下载 URL 报告 | `diff.Added` 未落盘 | `diff-report.json` 的 `addedArtifacts`（含 URL） |
| 删除脚本 | 无 | `cleanup-{oldDate}-to-{newDate}.sh`（bash 内联 rm，不执行） |
| 输出目录 | `pypi-{outputDate}` | `pypi-diff-{newDate}-{oldDate}`（见决策 2） |

## 2. 关键设计决策（已确认）

### 决策 1：改造现有 incremental-download 任务

不新增任务类型，复用既有 TUI 任务项、CLI 入口（`-a/-b`）与配置结构，在其上补齐能力。任务标签更新为"增量下载（快照对比）"。

### 决策 2：下载输出目录 = `pypi-diff-{newDate}-{oldDate}`

```
输出根目录: {mirrorRoot}/pypi-diff-2025-07-25-2025-07-24/
  packages/xx/xx/...            # 新增/变更 artifacts（路径与 URL 相对路径一致）
  state/                        # failed.jsonl / not-found.jsonl（复用 state store）
  diff-report.json              # 结构化对比报告
  removed-artifacts.jsonl       # 删除清单（审计数据源）
  cleanup-2025-07-24-to-2025-07-25.sh  # 删除脚本
  run-summary.json              # 运行摘要
```

- 目录名由 `newDate`/`oldDate` 推导，**可被 `--output-dir` 覆盖**（与 `artifact-download` 的 `OutputDir` 逻辑一致）。
- 该目录是**增量文件集合**（非完整镜像），可用于 rsync 合并进主镜像或单独发布。
- `IncrementalDownloadTaskConfig` 改造：移除 `OutputDate`，新增 `OutputDir`（默认 `pypi-diff-{newDate}-{oldDate}`）与 `CleanupRoot`（见决策 4）。

### 决策 3：删除脚本 = bash 内联 rm 命令

- 每行一条 `rm -f -- '...'`，路径用单引号包裹并转义（`'` → `'\''`），兼容 `+`/`!`/空格等字符。
- 脚本头部含：用途注释、`CLEANUP_ROOT`（目标根目录，可手工修改）、生成时间、删除文件/包统计、人工审查提示。
- **不执行**：只写文件；脚本末尾不自动 `rmdir` 清理空目录（提供注释提示可用 `find ... -type d -empty -delete` 手动清理）。
- 另输出 `removed-artifacts.jsonl`（每行 `{package, filename, relativePath, reason}`，reason ∈ `package-removed` | `artifact-removed`）作为审计数据源。

### 决策 4：删除脚本目标根目录参数化 + 用户确认

- `CleanupRoot` 默认 = `{mirrorRoot}/pypi-{oldDate}`（旧日期的镜像目录）。
- **CLI**：`--cleanup-root` 可覆盖；缺省用默认值，运行结束打印醒目提示"删除脚本针对 {cleanupRoot}，请人工审查后执行"（CLI 不阻塞，脚本本就只生成）。
- **TUI**：确认页展示清理根目录并弹确认步骤，文案明确"请确认该目录是 {oldDate} 的 pypi mirror"，用户确认后才写入脚本；可回退修改路径或取消。
- 脚本内的 `CLEANUP_ROOT` 变量写在头部，即使参数有误也可手工修正后再执行。

## 3. 数据流

```
输入: old snapshot (pypi-2025-07-24)   new snapshot (pypi-2025-07-25)
  │
  ▼
[1 Prepare]        读取 old/new 日期，输出根目录 = pypi-diff-{new}-{old}
  ▼
[2 Load Manifest]  loadManifestByDate 两份（manifest.json 优先，缺失现场构建）
  ▼
[3 Diff]
   ├─ artifact 维度: planner.DiffSnapshotManifests → added/changed/removed/unchanged
   └─ package 维度: 包名集合差集 → addedPackages / removedPackages
  ▼
[4 Build Report]   diff-report.json（含 removedPackages / addedArtifacts(URL) /
                    removedArtifacts / addedPackages / changedArtifacts）
  ▼
[5 Filter]         下载条目 + 删除路径统一过 ShouldIncludeArtifact（过滤一致性）
  ▼
[6 Download]       BuildDownloadPlan(added+changed) → ExecuteDownloadPlan
                   输出到 pypi-diff-{new}-{old}/packages/...（复用下载器）
                   失败写入 state/failed.jsonl, 404 写入 state/not-found.jsonl
  ▼
[7 Cleanup Script] 合并「removedPackages 的全部旧 artifacts」+「共有包 removed artifacts」
                   生成 cleanup-{old}-to-{new}.sh（内联 rm，不执行）
                   + removed-artifacts.jsonl
  ▼
[8 Summary]        run-summary.json + SyncRunResult
```

### 3.1 下载范围

| 类别 | 是否下载 | 理由 |
|---|---|---|
| `added`（含**全新包**的 artifacts） | ✅ | 新包全部文件都是新增，不下载则镜像不完整 |
| `changed`（同 RelativePath 内容/hash 变化） | ✅ | 不覆盖则内容过期 |
| `unchanged` | ❌ | 跳过 |
| `removed` | ❌ | 进删除脚本 |

> 需求 2 字面只提"共有包的新增 artifacts"；`added` 中来自全新包的部分一并纳入下载，并在报告中以 `addedPackages` 标识，便于区分。

### 3.2 过滤一致性

下载与删除脚本基于**同一份过滤规则**（`pypi.DefaultFilterOptions`，与 `artifact-download` 一致；配置覆盖留待后续统一接入）：

- 下载：`ShouldIncludeArtifact` 过滤后才入 plan。
- 删除脚本：只列出过滤后应存在于镜像的 artifacts（避免"镜像只有过滤后文件，脚本却列出过滤外路径"的错位）。
- 被过滤或非 `packages/` 路径的删除条目计入 `removedArtifactsSkipped` 并在 CLI/TUI/报告中提示。

## 4. 数据结构

### 4.1 类型改动（`types/types.go`）

```go
// IncrementalDownloadTaskConfig 改造
type IncrementalDownloadTaskConfig struct {
    OldMetadataDate string `json:"-"`
    NewMetadataDate string `json:"-"`
    // OutputDir 默认 "pypi-diff-{newDate}-{oldDate}"，可覆盖
    OutputDir string `json:"-"`
    // CleanupRoot 删除脚本目标根目录，默认 {mirrorRoot}/pypi-{oldDate}
    CleanupRoot string `json:"-"`
}

// SnapshotDiff 扩展 package 维度
type SnapshotDiff struct {
    AddedPackages   []string
    RemovedPackages []string
    Added           []ArtifactRecord
    Changed         []ArtifactChange
    Removed         []ArtifactRecord
    Unchanged       []ArtifactRecord
}

// SyncRunResult 扩展
type SyncRunResult struct {
    // ...既有字段
    DiffReportPath     *string   `json:"diffReportPath,omitempty"`
    CleanupScriptPath  *string   `json:"cleanupScriptPath,omitempty"`
    RemovedPackageCount *int     `json:"removedPackageCount,omitempty"`
    RemovedArtifactCount *int    `json:"removedArtifactCount,omitempty"`
}
```

### 4.2 diff-report.json

```json
{
  "oldSnapshotId": "pypi-2025-07-24",
  "newSnapshotId": "pypi-2025-07-25",
  "generatedAt": "2026-07-27T10:00:00Z",
  "stats": {
    "oldPackages": 350000, "newPackages": 349800,
    "addedPackages": 120, "removedPackages": 320,
    "addedArtifacts": 5600, "changedArtifacts": 180, "removedArtifacts": 4300,
    "removedArtifactsSkipped": 12
  },
  "removedPackages": ["oldpkg1", "oldpkg2", "..."],
  "addedPackages": ["newpkg1", "..."],
  "addedArtifacts": [
    {"package": "numpy", "filename": "numpy-2.1.0-cp312-cp312-manylinux_2_17_x86_64.whl",
     "relativePath": "packages/ab/cd/ef/numpy-2.1.0-....whl",
     "url": "https://pypi.tuna.tsinghua.edu.cn/packages/ab/cd/ef/...whl",
     "hash": "sha256:...", "source": "html"}
  ],
  "changedArtifacts": [
    {"previous": {"relativePath": "..."}, "current": {"relativePath": "..."}}
  ],
  "removedArtifacts": [
    {"package": "sharedpkg", "filename": "sharedpkg-0.1.tar.gz",
     "relativePath": "packages/aa/bb/cc/sharedpkg-0.1.tar.gz", "reason": "artifact-removed"}
  ]
}
```

- `addedArtifacts` 满足需求 2（下载 URL 可查）。
- `removedArtifacts` 包含**全部**删除的 artifacts（删除包的文件 + 共有包中删除的文件），每条带 `reason`（`package-removed` / `artifact-removed`）区分；该数组即删除脚本与 `removed-artifacts.jsonl` 的数据源。被过滤规则或非 `packages/` 路径剔除的条目不进脚本，统计见 `stats.removedArtifactsSkipped`。

### 4.3 删除脚本（cleanup-{old}-to-{new}.sh）

```bash
#!/usr/bin/env bash
# mirror-sync incremental-download 生成的删除脚本（不自动执行）
# 用途: 清理 2025-07-24 -> 2025-07-25 快照对比中已删除的内容
# 目标根目录: CLEANUP_ROOT（下方变量，可修改后执行以重定向删除目标）
# 生成时间: 2026-07-27T10:00:00Z
# 统计: 删除包 320 个 / 删除文件 4,300 个
# 注意: 执行前请人工审查；可先运行 bash -n 本脚本做语法检查
CLEANUP_ROOT="/data/mirror/pypi/pypi-2025-07-24"

# ---- removed packages (320) ----
rm -f -- "${CLEANUP_ROOT}/packages/aa/bb/cc/oldpkg1-1.0.tar.gz"
rm -f -- "${CLEANUP_ROOT}/packages/de/f0/12/oldpkg1-1.0-py3-none-any.whl"
# ...

# ---- removed artifacts of existing packages (4,300) ----
rm -f -- "${CLEANUP_ROOT}/packages/aa/bb/cc/sharedpkg-0.1.tar.gz"
# ...

# 如需清理空目录（人工执行）:
# find "${CLEANUP_ROOT}/packages" -type d -empty -delete
```

- 每行 `rm -f -- "${CLEANUP_ROOT}/<relativePath>"`：`CLEANUP_ROOT` 为脚本内变量（可改以重定向删除目标），相对路径单独转义（`"`、`$`、`` ` ``、`\` 加反斜杠）。
- `removedPackages` 的文件展开：查 old manifest 中该包全部 artifacts（过过滤）。

### 4.4 removed-artifacts.jsonl

```jsonl
{"package":"oldpkg1","filename":"oldpkg1-1.0.tar.gz","relativePath":"packages/aa/bb/cc/oldpkg1-1.0.tar.gz","reason":"package-removed"}
{"package":"sharedpkg","filename":"sharedpkg-0.1.tar.gz","relativePath":"packages/aa/bb/cc/sharedpkg-0.1.tar.gz","reason":"artifact-removed"}
```

**删除包的文件展开语义（已确认）**：`removedPackages`（old 有、new 无的包）的删除目标 = **新 metadata 中移除的该包 artifacts**，即该包在旧快照中的全部 artifacts（old manifest 中按包名过滤、过过滤规则）。每条具体到 `RelativePath` 指向的 `packages/` 按 hash 分片后的**文件级路径**，脚本用 `rm -f` 逐文件删除，**不做目录级删除**（`rm -rf` 或 `rmdir` 一律不自动执行）。

## 5. 实现要点

### 5.1 Diff 增强（`internal/core/planner`）

- `DiffSnapshotManifests` 保持 artifact 维度逻辑不变。
- 新增 package 维度对比（基于 `oldManifest.Packages` / `newManifest.Packages` 的 Name 集合）：
  - `addedPackages = new - old`，`removedPackages = old - new`
- 挂到 `SnapshotDiff` 新字段，`runner` 侧组装。

### 5.2 下载复用（`internal/runner/sync.go`）

- `BuildDownloadPlan(BuildDownloadPlanOptions{Diff: &diff})` 逻辑不变（added + changed）。
- `ExecuteDownloadPlan` 复用：流式下载、断点续传、重试、404 分类、hash 校验均已具备。
- 输出路径：`filepath.Join(outputRoot, artifact.RelativePath)`，`RelativePath` 源自 `ResolveArtifactPath`，天然满足 `packages/xx/xx/`。
- state：复用 `downloader.NewStateStore` 写 `failed.jsonl` / `not-found.jsonl`；增量场景量小，不做按包 checkpoint（全量任务才有），但保留失败明细。

### 5.3 删除脚本生成（新增 `internal/core/cleanup` 或 `internal/pypi` 下的小模块）

- 输入：`removedPackages []string`（+ old manifest 查文件）、`removedArtifacts []ArtifactRecord`、`cleanupRoot`、filter。
- 输出：bash 脚本内容 + `removed-artifacts.jsonl`。
- 转义函数：`shellQuote(s string)`（双引号包裹 + 转义），单元测试覆盖特殊字符（`'`、`+`、空格、`!`、`$`、反引号）。
- 路径安全：仅接受 `packages/` 前缀的相对路径；拒绝绝对路径与 `..` 段（防御 `ResolveArtifactPath` 的回退分支产生的异常路径）。

### 5.4 配置与 CLI/TUI

- `config.NormalizeConfig`：`IncrementalDownload.OutputDir` 默认 `FallbackIncrementalOutputDir(newDate, oldDate)` = `pypi-diff-{newDate}-{oldDate}`；`CleanupRoot` 默认 `BuildMirrorOutputRoot(mirrorRoot, oldDate)`。
- CLI（`cmd/mirror-sync/main.go`）：
  ```
  mirror-sync incremental -a 2025-07-24 -b 2025-07-25
    [--output-dir pypi-diff-2025-07-25-2025-07-24]
    [--cleanup-root /data/mirror/pypi/pypi-2025-07-24]
  ```
  - 原 `--output-date/-o` 移除：若传入则报错并提示改用 `--output-dir`。
- TUI：
  - `taskDefs` 中 incremental-download 字段改为 `oldMetadataDate / newMetadataDate / outputDir`（`outputDir` 只读展示推导值）。
  - 确认页新增"Cleanup Root"一行 + 确认交互（决策 4 文案）。
  - 运行界面展示 diff 统计（added/removed 包数、added/removed 文件数）。

## 6. 异常场景

| 场景 | 行为 |
|---|---|
| 旧/新快照 manifest 缺失 | `loadManifestByDate` 现场构建（`WriteOutputs=false`），与现有行为一致 |
| 某快照包目录无 index.html | 视为空包，artifact 数 0；包名仍参与 package 维度 diff |
| 新增文件已存在（size>0） | `ExecuteDownloadPlan` 文件级跳过 |
| 下载 404 | 记 `not-found.jsonl`，不重试 |
| 下载 hash 校验失败 | 重试 2 次后记 `failed.jsonl` |
| 删除脚本路径含特殊字符 | 统一 shell 转义；`packages/` 前缀校验 |
| TUI 用户取消确认 | 不写删除脚本，diff 报告与下载照常完成 |
| 快照巨大（~50 万包） | diff 阶段两份 manifest 全量驻留内存（既有行为，~1-2GB）；后续可优化为 artifacts.jsonl 双指针归并（见 §8） |

## 7. 测试计划

1. **package 维度 diff**：新增/删除/不变包名的集合差集单测。
2. **过滤一致性**：删除脚本仅含过滤后 artifacts。
3. **shell 转义**：特殊字符路径生成合法 bash（`bash -n` 校验 + 单测断言转义结果）。
4. **diff-report 完整性**：addedArtifacts 含 URL/hash；removedPackages 与 removedArtifacts 分类正确。
5. **端到端（小 fixture）**：构造两份微型 snapshot → 运行 runner → 断言下载文件数、diff-report、cleanup 脚本内容、不执行。

## 8. 已知限制与后续优化

- 内存：对比阶段全量加载两份 manifest（既有行为）。后续可用 `manifests/artifacts.jsonl`（按 RelativePath 排序）双指针流式归并，O(1) 内存。
- `pypi-diff-*` 为增量文件集合而非完整镜像；合并进主镜像建议 `rsync`。
- 删除脚本不自动清理空目录（`packages/` 分片目录）；提供注释提示，人工决定。
- 删除包时以 metadata 为准；若旧镜像中存在 metadata 之外的残留文件，不在脚本覆盖范围（人工处理）。

## 9. 实施计划

| 阶段 | 内容 |
|---|---|
| A. 类型与配置 | `IncrementalDownloadTaskConfig` 改造、`SnapshotDiff`/`SyncRunResult` 扩展、`NormalizeConfig`、CLI flags、TUI fields |
| B. Diff 与报告 | planner package 维度 diff、`diff-report.json` 生成与落盘 |
| C. 下载 | `runIncrementalDownload` 重构：输出目录 `pypi-diff-*`、复用 `ExecuteDownloadPlan` + state store |
| D. 删除脚本 | cleanup 模块（转义/过滤/展开 removedPackages 文件）、`removed-artifacts.jsonl`、TUI 确认交互 |
| E. 收尾 | CLI summary 输出、单测、端到端 fixture 验证、文档与 TUI 文案更新 |
