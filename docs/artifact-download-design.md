# PyPI 包文件下载任务设计文档

> 更新日期：2026-07-26
> 设计决策依据用户讨论确认。

## 1. 概述

本文档规划"基于已有元数据快照，从 PyPI 镜像源下载实际包文件"的任务。这是 mirror-sync 工具中**数据量最大、耗时最长、最需要健壮性**的一环。

### 1.1 目标

1. **从元数据到文件**：读取已下载的元数据快照（`simple/{pkg}/index_v1.json` / `index.html`），解析出所有 artifact 的 URL 和 hash，下载到镜像目录。
2. **路径保真**：本地保存路径必须与远程 URL 的 `packages/` 相对路径一致，确保目录结构可直接发布为镜像站。
3. **海量稳定**：支持断点续传（按包粒度 checkpoint）、失败重试、暂停恢复。
4. **可观测**：实时进度（按包计数）、速率、ETA、失败明细。

### 1.2 范围

| 项目 | 范围 |
|------|------|
| 输入 | 元数据快照目录（`snapshots/pypi-{date}/simple/`） |
| 输出 | 镜像目录（`pypi-{date}/packages/...`），路径与 URL 相对路径保持一致 |
| 数据量预估 | ~50 万包，过滤后 ~200-300 万 artifact 文件，~1-3 TB（常用平台） |
| 过滤 | 默认只保留常用平台，按平台/架构/包名 include/exclude |
| 校验 | SHA256 hash 校验（元数据提供时） |

---

## 2. 关键设计决策

根据讨论确认以下五项决策：

### 决策 1：TUI 选择元数据快照界面

在 TUI 的 `artifact-download` 任务配置流程中，增加一个**快照选择步骤**：

```
TUI 任务配置流程：
  Provider (pypi) → Task (artifact-download) → 选择快照 → Config → Confirm → Running
                                                    ↓
                                            ┌──────────────────────────┐
                                            │  可用元数据快照列表       │
                                            │                          │
                                            │  ○ 2026-07-25 (今日)     │
                                            │    350,000 包            │
                                            │    4,200,000 文件        │
                                            │    已生成 manifest       │
                                            │                          │
                                            │  ○ 2026-07-24            │
                                            │    348,000 包            │
                                            │    4,150,000 文件        │
                                            │                          │
                                            │  ○ 2026-07-23            │
                                            │    345,000 包            │
                                            │    已生成 manifest       │
                                            └──────────────────────────┘
```

**实现要点**：
- 扫描 `{metadataRoot}/snapshots/` 下所有 `pypi-{date}` 目录
- 读取每个快照的 `stats.json`（若有）获取包数和文件数
- 按日期降序排列，最新在前
- 默认选中最新快照
- 选中后，`MetadataDate` 字段自动填入对应日期

CLI 模式也支持直接 `--metadata-date` 指定，无需此选择界面。

### 决策 2：Checkpoint 按包粒度

**不以文件为单位**记录 checkpoint，而是以**包（package）为单位**：

```
一个包的所有 artifact（过滤后）全部下载完成 → 标记该包完成
```

```
checkpoint.jsonl 内容示例（每行一个已完成包）：
{"package": "numpy", "completedAt": "2026-07-26T10:30:00Z", "files": 8, "bytes": 52428800}
{"package": "pandas", "completedAt": "2026-07-26T10:32:15Z", "files": 12, "bytes": 104857600}
```

**恢复逻辑**：
1. 读取 `checkpoint.jsonl` → `completedPackages map[string]struct{}`
2. 遍历元数据中的包，若包名在 completedPackages 中 → 跳过整个包
3. 未完成的包，重新处理其所有 artifact（但已存在的单个文件通过 `os.Stat` 跳过下载体，不浪费带宽）

**设计理由**：
- Checkpoint 文件小（~50 万行 vs ~500 万行）
- 包是自然的原子单位，语义清晰
- 避免部分下载的包留下不完整的文件集合

**文件级跳过**仍通过 `os.Stat(entry.DestinationPath)` 判断，每个文件下载前检查：
- 文件已存在且 size > 0 → 跳过 HTTP 请求
- 这确保同一个包中断后重跑时不会重复下载已完成的文件

### 决策 3：默认过滤规则

只保留常用平台，等价于现有的 `DefaultFilterOptions`：

```
IncludeSource       = true   # .tar.gz, .zip, .tar.bz2, .tar.xz, .tgz
IncludePlatformAny  = true   # py3-none-any
IncludeLinuxAmd64   = true   # manylinux* x86_64, linux x86_64
IncludeWindowsAmd64 = true   # win_amd64
ExcludeMusllinux    = true   # 排除 musllinux 标签
ExcludeMacos        = true   # 排除 macOS
ExcludeArm          = true   # 排除 ARM (armv7, aarch64/arm64)
IncludePackages     = []     # 空 = 全部
ExcludePackages     = []     # 空 = 不排除
```

可在 TUI 配置页或 CLI 参数中覆盖这些过滤规则。

### 决策 4：并发与速率

- **默认并发数**：16（当前值，保持不变）
- **速率限制**：不额外增加默认限速。保留可选的 `--rate-limit` 参数供需要时使用
- **重试**：默认 2 次，超时 60 秒

16 并发对于包下载场景是合理的——PyPI 包文件从几 KB 到几百 MB 不等，16 个 worker 既能充分利用带宽，又不会对上游造成过大压力。

### 决策 5：输出目录与路径保真

- **输出根目录**：`{mirrorRoot}/pypi-{outputDate}/`（保持现有设计）
- **packages 相对路径**：必须与元数据 URL 解析出的 `packages/...` 路径完全一致

```
远程 URL:  https://pypi.tuna.tsinghua.edu.cn/packages/ab/cd/ef/numpy-1.26.0.whl
                   ↓ 解析出 relativePath = "packages/ab/cd/ef/numpy-1.26.0.whl"
本地存储:  {mirrorRoot}/pypi-{outputDate}/packages/ab/cd/ef/numpy-1.26.0.whl
```

这一策略由 `pypi.ResolveArtifactPath()`（`internal/pypi/path.go`）保证，下载器直接使用解析出的 `RelativePath`，不自行拼接路径。

---

## 3. 数据流

```
┌──────────────────────────────────────────────────────────────┐
│              TUI: 选择元数据快照                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ 扫描 snapshots/ 目录 → 展示快照列表 → 用户选择       │   │
│  └──────────────────────────────────────────────────────┘   │
│                              ↓                              │
│               metadataDate = "2026-07-25"                    │
│                              ↓                              │
│  Stage 1: Build / Load Manifest                             │
│    ├── 检查 manifests/artifacts.jsonl 是否存在               │
│    ├── 存在 → 直接加载（避免重新遍历 50 万目录）             │
│    └── 不存在 → BuildManifestFromSnapshot → 写入 manifests/  │
│                              ↓                              │
│  Stage 2: Build Plan (按包)                                  │
│    ├── 加载 artifacts.jsonl                                  │
│    ├── 应用过滤器 → 只保留常用平台                           │
│    ├── 加载 checkpoint.jsonl → 跳过已完成的包                │
│    ├── 对未完成的包，检查每个文件的 DestinationPath 是否存在 │
│    └── 输出 download-plan.json（包列表 + 待下载文件）        │
│                              ↓                              │
│  Stage 3: Download (按包串行，包内文件并发)                  │
│    ├── 逐包处理：                                            │
│    │   ├── 包的多个 artifact 并发下载（16 workers 共享）     │
│    │   ├── 每个文件：下载 → 校验 hash → rename               │
│    │   ├── 包内全部成功后 → 追加一条 checkpoint              │
│    │   └── 包内有失败 → 记录 failed，该包不标记完成          │
│    ├── 实时进度（按包计数：完成包数 / 总包数）               │
│    └── 生成 run-summary.json                                 │
└──────────────────────────────────────────────────────────────┘
```

### 3.1 关键差异：按包处理 vs 按文件处理

传统按文件处理：
```
并发池处理 artifact 队列：文件A完成→checkpoint写一行→文件B完成→checkpoint写一行...
中断后丢失最后 N 个未 flush 的 checkpoint
```

本设计按包处理（包间串行，包内并发）：
```
包1: [文件1, 文件2, 文件3] → 全部完成 → checkpoint写一行(包1)
包2: [文件4, 文件5]       → 全部完成 → checkpoint写一行(包2)
...
中断后：最多丢失当前正在处理的 1 个包
```

**为什么包间串行？**
- 保证 checkpoint 原子性：一个包要么全部完成，要么全部未完成
- 简化恢复逻辑：不需要跟踪部分完成的包
- 方便 TUI 展示：按包计数进度清晰

**包内如何并发？**
- 所有包共享 16 个 worker 的并发池
- worker 从包队列中取一个包，处理该包的所有文件（包内串行或小并发），完成后取下一个包
- 这种设计下，包的 artifact 数量成为单个 worker 的负载单元

如果包的 artifact 数量差异很大（如大包有 50 个文件，小包只有 1 个），可以使用**包内并发**：一个包被分配给一个 worker 后，该 worker 开子 goroutine 并发下载包内的多个文件。

### 3.2 包内并发下载

```
每个 worker 处理一个包时：
  1. 获取该包的所有待下载文件列表
  2. 使用 semaphore（限 `min(files, 4)` 并发）下载文件
  3. 所有文件完成后，标记包完成
```

这样即使一个大包有 50 个文件，也能较快完成，不会拖慢整体进度。

---

## 4. 状态文件布局

```
{mirrorRoot}/pypi-{outputDate}/
  packages/                          # 下载的包文件
    ab/cd/ef/numpy-1.26.0-...        # 路径与 URL 相对路径一致
    ...
  state/
    checkpoint.jsonl                 # 已完成包列表（追加写）
    failed.jsonl                     # 失败文件记录
    not-found.jsonl                  # 404 记录
```

### 4.1 checkpoint.jsonl

```jsonl
{"package":"numpy","completedAt":"2026-07-26T10:30:00Z","files":8,"bytes":52428800}
{"package":"pandas","completedAt":"2026-07-26T10:32:15Z","files":12,"bytes":104857600}
```

- 每行一条，追加写
- 每次完成一个包的所有文件后追加
- 重启时全量读入内存 → `map[string]struct{}`
- 文件不超过 50 万行，内存占用 ~20 MB

### 4.2 failed.jsonl

```jsonl
{"package":"badpkg","filename":"badpkg-1.0.tar.gz","relativePath":"packages/xx/yy/zz/badpkg-1.0.tar.gz","error":"HTTP 500","attemptedAt":"2026-07-26T10:35:00Z"}
```

### 4.3 not-found.jsonl

```jsonl
{"package":"oldpkg","filename":"oldpkg-0.1.tar.gz","relativePath":"packages/aa/bb/cc/oldpkg-0.1.tar.gz","url":"https://pypi.tuna.tsinghua.edu.cn/packages/aa/bb/cc/oldpkg-0.1.tar.gz","attemptedAt":"2026-07-26T10:36:00Z"}
```

---

## 5. TUI 交互设计

### 5.1 快照选择界面

在 `Provider → Task → Config` 之间新增一步 `Select Snapshot`：

```
┌─────────────────────────────────────────────────────┐
│  选择元数据快照                                      │
│                                                     │
│  metadata root: /data/meta/pypi/snapshots/          │
│                                                     │
│  可用快照:                                          │
│  ┌─────────────────────────────────────────────┐   │
│  │ ● 2026-07-25 (今日)                         │   │
│  │   包数: 350,000  文件: 4,200,000  有manifest │   │
│  │                                             │   │
│  │ ○ 2026-07-24                                │   │
│  │   包数: 348,000  文件: 4,150,000  有manifest │   │
│  │                                             │   │
│  │ ○ 2026-07-23                                │   │
│  │   包数: 345,000  文件: 4,100,000  无manifest │   │
│  └─────────────────────────────────────────────┘   │
│                                                     │
│  提示：选择后基于该快照构建下载计划并下载包文件       │
│                                                     │
│         [取消]              [下一步: 配置过滤]       │
└─────────────────────────────────────────────────────┘
```

**实现说明**：
- 扫描 `{metadataRoot}/snapshots/` 下 `pypi-*` 目录
- 读取各目录的 `stats.json`（若存在）获取统计信息
- 使用 TUI 已有的 bubbles `list` 组件实现选择列表
- 选中后自动填充 `ArtifactDownloadTaskConfig.MetadataDate`

### 5.2 运行界面增强

运行时进度展示增强为双维度：

```
┌─────────────────────────────────────────────────────┐
│  [PyPI] 下载包文件         2026-07-25               │
│                                                     │
│  包进度: ████████████░░░░░░  42.5%                  │
│  已完成包: 148,552 / 349,200                        │
│  文件进度: ████████████░░░░░░  43.1%                │
│  已完成文件: 925,800 / 2,148,000                    │
│                                                     │
│  当前包: scipy-1.14.0 (12 个文件中的 8 个)          │
│  速率: 85.3 MB/s | ETA: 1h 52m                      │
│  活跃: 12 workers                                   │
│                                                     │
│  失败: 38 | 404: 112 | 重试: 5                      │
│                                                     │
│  [Pause] [Cancel]                                   │
└─────────────────────────────────────────────────────┘
```

---

## 6. CLI 交互

### 6.1 使用示例

```bash
# 基于 2026-07-25 的元数据下载包
mirror-sync artifact-download -m 2026-07-25

# 覆盖默认过滤规则
mirror-sync artifact-download -m 2026-07-25 \
  --include-source --include-linux-amd64 \
  --exclude-macos --exclude-arm

# 如果元数据快照还没有 manifest，先构建再下载
mirror-sync artifact-download -m 2026-07-25
```

### 6.2 输出示例

```
[Prepare] PyPI / 按元数据快照下载包
[Load Manifest] Loading artifacts from snapshot 2026-07-25
  → 350,000 packages, 4,200,000 artifacts
[Build Plan] Filtering artifacts (默认常用平台)...
  → 349,200 packages, 2,150,000 artifacts after filter
  → Loading checkpoint: 120,000 packages already completed
  → 229,200 packages to process
[Download] Processing packages...
  → Package scipy-1.14.0 8/12 files  [85.3 MB/s]
  → Package numpy-1.26.0 5/5 files  [complete ✓]
  → Package pandas-2.1.0 12/12 files [complete ✓]
  → Progress: 148,552/229,200 packages (64.8%)  ETA: 1h 52m
  → Failed: 38 | 404: 112 | Retry: 5
[Finalize] Completed: 229,200 packages
  → Succeeded: 229,050 | Failed: 38 | 404: 112
  → Output: /data/mirror/pypi-2026-07-25/packages/
```

### 6.3 CLI 参数变化

`artifact-download` 子命令不再需要 `--output-date`（默认与 `--metadata-date` 相同），同时从 CLI 移除过滤参数（保持简洁，过滤在 TUI 中配置或通过 config 文件设置）：

```bash
mirror-sync artifact-download -m 2026-07-25
```

高级过滤参数保留但不需要在 CLI 暴露，通过 `~/.mirror-sync/config.json` 配置：

```json
{
  "base": { ... },
  "selectedTask": "artifact-download",
  "pypi": {
    "artifactDownload": {
      "metadataDate": "2026-07-25",
      "outputDate": "2026-07-25"
    },
    "filter": {
      "includeSource": true,
      "includePlatformAny": true,
      "includeLinuxAmd64": true,
      "includeWindowsAmd64": true,
      "excludeMusllinux": true,
      "excludeMacos": true,
      "excludeArm": true
    }
  }
}
```

---

## 7. Checkpoint 按包粒度的实现设计

### 7.1 数据结构

```go
// PackageCheckpoint tracks completion per-package.
type PackageCheckpoint struct {
    Package     string    `json:"package"`
    CompletedAt time.Time `json:"completedAt"`
    Files       int       `json:"files"`
    Bytes       int64     `json:"bytes"`
}

// CheckpointStore manages checkpoint persistence.
type CheckpointStore struct {
    mu         sync.Mutex
    path       string
    completed  map[string]struct{}  // set of completed package names
    buf        *bufio.Writer
    file       *os.File
}
```

### 7.2 核心逻辑

```go
// LoadCheckpoint reads the checkpoint file and returns completed packages.
func LoadCheckpoint(path string) (map[string]struct{}, error)

// CompletePackage marks a package as fully downloaded.
// Appends one line to checkpoint.jsonl.
func (cs *CheckpointStore) CompletePackage(pkg PackageCheckpoint) error

// IsCompleted checks if a package is already done.
func (cs *CheckpointStore) IsCompleted(pkgName string) bool

// Close flushes and closes the checkpoint file.
func (cs *CheckpointStore) Close() error
```

### 7.3 集成到下载执行器

```go
func runArtifactDownload(cfg, onEvent, tc) {
    // 1. 加载 manifest（artifacts.jsonl 或重新构建）
    manifest := loadManifest(snapshotRoot)
    
    // 2. 加载 checkpoint
    checkpointPath := filepath.Join(outputRoot, "state", "checkpoint.jsonl")
    completedPkgs, _ := loadCheckpoint(checkpointPath)
    
    // 3. 构建待下载包列表（跳过已完成包）
    pendingPackages := buildPendingPackages(manifest, filter, completedPkgs)
    
    // 4. 逐个包处理
    for _, pkg := range pendingPackages {
        // 检查包内每个文件是否已存在（文件级跳过）
        filesToDownload := filterExistingFiles(pkg.Artifacts, outputRoot)
        
        if len(filesToDownload) == 0 {
            // 所有文件都已存在 → 直接标记完成
            checkpointStore.CompletePackage(pkg.Name, pkg.Stats)
            continue
        }
        
        // 并发下载包内文件
        success := downloadPackageFiles(filesToDownload, opts)
        
        if success {
            // 包内所有文件下载成功 → 标记包完成
            checkpointStore.CompletePackage(pkg.Name, pkg.Stats)
        } else {
            // 有文件失败 → 记录 failed，包不标记完成
            recordFailedFiles(failedFiles)
        }
    }
}
```

---

## 8. 错误处理

### 8.1 中断恢复流程

```
启动
  ↓
加载 checkpoint.jsonl → 得到已完成包集合 C
  ↓
遍历 manifest，对每个包 p：
  if p ∈ C → skip
  else → 检查包的每个文件本地是否存在
         若全部存在 → 自动补记 checkpoint（防止不一致）
         否则 → 加入待下载队列
  ↓
执行下载（仅处理未完成包）
```

### 8.2 异常场景

| 场景 | 行为 |
|------|------|
| 包中部分文件已存在、部分需要下载 | 跳过已存在的，只下载缺失的 |
| 包中所有文件已存在但包不在 checkpoint | 自动补记 checkpoint |
| 下载中断（kill / crash） | 最多丢失当前包的进度，下次从 checkpoint 恢复 |
| 包内有文件 404 | 记录 not-found，包不标记完成 |
| 包内有文件校验失败 | 重试 2 次，仍失败 → 记录 failed，包不标记完成 |

---

## 9. 实现优先级

### Phase 1（核心下载能力）

1. **TUI 快照选择界面** — 扫描 `snapshots/` 目录，列表展示，用户选择
2. **按包粒度的 Checkpoint** — `CheckpointStore` 读写，恢复逻辑
3. **Manifest 预生成与复用** — 优先读 `artifacts.jsonl`，不存在才遍历 `simple/`
4. **失败/404 持久化** — `failed.jsonl` / `not-found.jsonl`
5. **包内并发下载** — 多文件并发处理

### Phase 2（可观测性）

1. 双维度进度（包数 + 文件数）
2. 速率 / ETA 统计
3. TUI 运行界面增强

### Phase 3（高级功能）

1. 重试失败项子任务
2. 磁盘空间检查
3. 下载后全量 hash 校验

---

## 10. 与增量下载的关系

| 场景 | 推荐任务 |
|------|----------|
| **首次同步** | `artifact-download`（基于单日元数据全量下载） |
| **日常增量** | `incremental-download`（比较两日快照，只下新增/变更） |
| **中断恢复** | `artifact-download`（checkpoint 自动跳过已完成包） |
| **重试失败包** | `artifact-download`（未完成的包自动重试） |

两个任务共享同样的下载执行器和 checkpoint 格式，差异仅在于包的来源：
- `artifact-download`：来源于单日 manifest 中的所有包
- `incremental-download`：来源于 diff 中的新增/变更包
