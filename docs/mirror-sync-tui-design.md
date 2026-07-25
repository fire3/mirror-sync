# mirror-sync TUI 设计文档

## 1. 背景与目标

本项目用于管理多类镜像源的同步，当前优先支持：

1. `PyPI mirror`
2. 后续可扩展到 `Ubuntu mirror`、其他 HTTP/文件清单型镜像

设计目标：

1. 不依赖 `apt-mirror` 这类一体化镜像工具，下载、比对、重试、断点续传都由我们自行控制。
2. 将“元数据同步”和“镜像文件下载”分离为两个可配置区域。
3. 支持基于两份元数据快照进行增量下载。
4. 提供带状态监控、进度展示、配置入口的全屏 TUI。
5. 优先采用 `TypeScript` 实现，方便开发 TUI 和后续扩展。


## 2. 用户需求细化

结合当前需求，建议将功能拆为四条主链路：

1. **配置管理**
   - 配置元数据备份区
   - 配置镜像数据下载区
   - 配置镜像源、并发、重试、限速、过滤规则

2. **元数据同步**
   - 获取 PyPI 全量包列表
   - 下载每个包的 `index.html`
   - 尝试下载 `PEP 691 JSON` 元数据，如 `index_v1.json`
   - 结果写入元数据备份区的快照目录

3. **镜像数据下载**
   - 从元数据中解析包文件链接
   - 按 URL 中的相对路径保存到镜像下载区
   - 目录结构必须与镜像 URL 相对路径保持一致，便于后续直接发布为镜像站

4. **增量比较**
   - 对比两份元数据快照
   - 找出新增文件、变更文件、缺失文件
   - 仅调度增量下载任务


## 3. 从现有 Python 脚本得到的启发

仓库中已有两个参考脚本：

1. `scripts/fetch_all_packages_metadata.py`
   - 已覆盖全量包列表获取
   - 已覆盖 `index.html` / `index_v1.json` 下载
   - 适合作为“PyPI 元数据抓取器”的原型

2. `scripts/missing_files_downloader.py`
   - 已覆盖简单索引解析
   - 已覆盖基于两份元数据目录的比较下载
   - 已覆盖断点续传、临时文件、下载进度、checkpoint、404 记录
   - 适合作为“下载执行器”和“增量同步器”的原型

建议后续 TS 实现保留这两类能力，但不要直接把 UI 逻辑写进下载逻辑中，而是拆成：

1. `core engine`
2. `provider`
3. `job scheduler`
4. `state store`
5. `TUI app`


## 4. 总体架构

建议采用分层架构：

```text
TUI App (TypeScript)
  -> Job API / Command Bus
    -> Sync Engine
      -> Provider: PyPI / Ubuntu
      -> Metadata Snapshot Store
      -> Diff Planner
      -> Download Executor
      -> Checkpoint / State Store
```

### 4.1 模块划分

1. **app/tui**
   - 全屏终端 UI
   - 展示配置、任务、日志、监控、统计
   - 发送用户操作到核心调度层

2. **core/jobs**
   - 任务编排
   - 定义任务状态机：`pending/running/paused/completed/failed/cancelled`
   - 将一个同步任务拆分为多个阶段

3. **core/providers**
   - 每种镜像类型一个 provider
   - 当前优先实现 `pypi`
   - 后续扩展 `ubuntu`

4. **core/planner**
   - 根据元数据快照生成待下载清单
   - 负责增量对比与文件过滤

5. **core/downloader**
   - HTTP 下载
   - 并发控制
   - 限速
   - 重试
   - 断点续传
   - 原子落盘

6. **core/storage**
   - 配置存储
   - 任务状态存储
   - snapshot manifest 存储
   - checkpoint / not-found / failed-task 存储


## 5. 推荐技术选型

### 5.1 语言与运行时

1. `TypeScript`
2. `Node.js 22+`

原因：

1. TS 对 TUI 生态友好
2. 异步 I/O、并发调度、事件驱动比较适合下载器
3. 后续如果要加 Web 管理面板，也能复用较多核心代码

### 5.2 TUI 框架建议

建议优先选 **`Ink`**，必要时配合少量终端组件库。

原因：

1. 组件化思维清晰，适合复杂页面拆分
2. 适合状态驱动 UI，和任务监控模型天然匹配
3. TS 支持较好，开发体验优于手写 ANSI

备选方案：

1. `neo-blessed`
   - 更接近传统终端控件
   - 表格、列表、焦点管理更原生
   - 但组件组织和状态管理通常比 Ink 更重

结论：

1. **首选**：`Ink + React state/event model`
2. **当需要复杂表格或树控件时**：引入少量补充组件，必要时在局部自绘

### 5.3 网络与解析

1. HTTP：优先 Node 原生 `fetch` / `undici`
2. HTML 解析：`cheerio` 或流式解析器
3. URL 处理：原生 `URL`
4. 配置文件：`YAML` 或 `TOML`
5. 状态存储：优先 `SQLite`，也可先以 `JSONL + 文件快照` 起步


## 6. 目录与数据布局设计

## 6.1 配置示例

```yaml
profiles:
  - name: pypi-tsinghua
    type: pypi
    enabled: true
    source:
      baseUrl: https://pypi.tuna.tsinghua.edu.cn
      simpleUrl: https://pypi.tuna.tsinghua.edu.cn/simple/
    storage:
      metadataRoot: /data/meta
      mirrorRoot: /data/mirror
    sync:
      concurrency: 32
      retry: 3
      timeoutSec: 60
      rateLimitMbps: 0
      userAgent: mirror-sync/0.1
    filters:
      includeSource: true
      includePlatformAny: true
      includeLinuxAmd64: true
      includeWindowsAmd64: false
      excludeMusllinux: true
      excludeMacos: true
      excludeArm: true
```

## 6.2 建议目录布局

```text
/data/meta/
  pypi/
    snapshots/
      2026-07-25T04-30-00Z/
        package-list.txt
        simple/
          a/
          b/
          ...
        manifests/
          packages.jsonl
          artifacts.jsonl
        stats.json
    current -> snapshots/2026-07-25T04-30-00Z

/data/mirror/
  pypi/
    packages/
      xx/
      yy/
```

说明：

1. **元数据备份区**保存“快照”，不能只保留一份 current。
2. **镜像下载区**保存最终文件，路径必须与 URL 相对路径一致。
3. `current` 只是一个逻辑指针，便于 TUI 默认读取最近一次快照。


## 7. PyPI Provider 设计

## 7.1 阶段一：同步包列表

入口：

1. 请求 `/simple/`
2. 解析全部包名
3. 生成 `package-list.txt`

输出：

1. `package-list.txt`
2. 统计信息：总包数、拉取时间、源地址、耗时

## 7.2 阶段二：同步每个包的元数据

对每个包名：

1. 下载 `simple/<package>/index.html`
2. 尝试以 `Accept: application/vnd.pypi.simple.v1+json` 获取 `index_v1.json`
3. 保存到：

```text
<snapshot>/simple/<package>/index.html
<snapshot>/simple/<package>/index_v1.json
```

建议额外生成标准化产物：

```text
<snapshot>/normalized/<package>.json
```

其中至少包含：

1. 包名
2. 文件名
3. 下载 URL
4. hash
5. requires-python
6. yanked
7. 上传时间（若可获取）

## 7.3 阶段三：生成快照清单

将每个包的元数据归一化为全局清单：

1. `packages.jsonl`：包级别信息
2. `artifacts.jsonl`：文件级别信息

`artifacts.jsonl` 建议字段：

```json
{
  "package": "numpy",
  "filename": "numpy-2.0.0-cp312-cp312-manylinux_x86_64.whl",
  "relativePath": "packages/ab/cd/xxxx/numpy-2.0.0-cp312-cp312-manylinux_x86_64.whl",
  "url": "https://.../packages/ab/cd/xxxx/numpy-2.0.0-cp312-cp312-manylinux_x86_64.whl",
  "hash": "sha256:...",
  "source": "html|json",
  "snapshotId": "2026-07-25T04-30-00Z"
}
```

这样增量比较就不必每次重新遍历所有 HTML 文件。


## 8. 相对路径保真策略

这是本项目里最关键的一条约束。

原则：

1. 对于包文件下载，**本地保存路径必须等于 URL 的相对路径**。
2. 若 HTML 中给出的链接是相对路径，如 `../../packages/...`，要解析为：

```text
packages/...
```

3. 若给出绝对 URL，则从 URL path 中抽取 `packages/...` 开始的部分。
4. 若来源不是 `packages/...` 结构，需要记录告警并走 provider 规则兜底。

建议抽象函数：

```ts
resolveArtifactPath(packageName, simplePageUrl, href) => {
  remoteUrl: string;
  relativePath: string;
}
```

后续所有下载器只认 `relativePath`，不再自行拼路径。


## 9. 增量同步设计

## 9.1 两份元数据快照比较

输入：

1. `old snapshot`
2. `new snapshot`

比较单位：

1. 优先按 `relativePath`
2. 辅助按 `filename + hash`

输出：

1. `added`
2. `changed`
3. `removed`
4. `unchanged`

推荐规则：

1. `relativePath` 不存在于 old，则视为新增
2. `relativePath` 相同但 hash 不同，则视为变化
3. `relativePath` 在 old 存在但 new 不存在，可记为 removed，但默认不删除本地镜像文件

## 9.2 下载计划

计划阶段只生成任务，不直接下载：

1. 读取 `artifacts.jsonl`
2. 应用过滤规则
3. 与 `checkpoint`、`not-found`、本地已存在文件进行交叉判断
4. 输出 `download-plan.jsonl`

计划和执行分离的好处：

1. TUI 可以先让用户预览
2. 可以暂停、恢复
3. 失败任务可以单独重放


## 10. 下载执行器设计

## 10.1 核心能力

1. 并发下载
2. 断点续传
3. 原子写入：先写 `.tmp`，完成后 rename
4. 重试与退避
5. 404 单独记录
6. checkpoint 持久化
7. 可暂停 / 可恢复 / 可取消

## 10.2 状态文件建议

```text
<mirrorRoot>/state/
  jobs/
    <jobId>.json
  checkpoint/
    <jobId>.checkpoint.txt
  failed/
    <jobId>.failed.jsonl
  not-found/
    <jobId>.404.jsonl
  logs/
    <jobId>.log
```

## 10.3 下载任务状态机

```text
queued -> resolving -> downloading -> verifying -> completed
                                 \-> retrying
                                 \-> failed
                                 \-> not_found
                                 \-> cancelled
```

## 10.4 校验策略

第一阶段可采用：

1. 文件存在且大小大于 0 则可跳过
2. 若元数据带 hash，则下载完成后做 hash 校验

第二阶段可增强：

1. 强制 hash 校验
2. 校验失败自动重试


## 11. 镜像任务抽象设计

为了让 `PyPI` 和未来的 `Ubuntu mirror` 共用一套 TUI，建议把“镜像任务”抽象成统一模型。

## 11.1 统一任务模型

一个镜像任务建议包含：

1. `profile`
2. `providerType`
3. `jobType`
4. `stage`
5. `progress`
6. `health`
7. `resource usage`
8. `result summary`

建议类型：

```ts
type ProviderType = 'pypi' | 'ubuntu';

type JobType =
  | 'metadata-sync'
  | 'manifest-build'
  | 'snapshot-compare'
  | 'download-plan'
  | 'artifact-download'
  | 'verification';

type JobHealth = 'healthy' | 'warning' | 'degraded' | 'failed';

interface MirrorJob {
  id: string;
  profileName: string;
  providerType: ProviderType;
  jobType: JobType;
  stage: string;
  status: 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled';
  startedAt?: string;
  finishedAt?: string;
  progress: {
    current: number;
    total: number;
    unit: 'packages' | 'files' | 'bytes' | 'requests';
    percent: number;
  };
  throughput?: {
    currentBytesPerSec: number;
    avgBytesPerSec: number;
    currentItemsPerSec: number;
  };
  health: JobHealth;
  stats: Record<string, number>;
}
```

这样 TUI 的任务列表、状态栏、详情页都可以围绕统一字段构建。

## 11.2 通用阶段模型

不同镜像任务虽然内部逻辑不同，但在 TUI 层建议统一呈现为：

1. `Prepare`
2. `Scan Metadata`
3. `Build Manifest`
4. `Compare / Plan`
5. `Download`
6. `Verify`
7. `Finalize`

说明：

1. `PyPI` 的“拉取 simple 元数据”映射到 `Scan Metadata`
2. `Ubuntu` 未来的 `Packages.gz / Release / InRelease` 抓取也可以映射到 `Scan Metadata`
3. 用户在 TUI 里看到的是统一阶段名，详情页里再显示 provider 特定子阶段

## 11.3 通用任务卡片

Dashboard 和任务列表中，每个任务建议固定展示这些信息：

1. provider 类型
2. profile 名称
3. 当前阶段
4. 总进度
5. 当前速率
6. 最近错误数
7. 当前健康状态
8. 是否可暂停 / 恢复 / 重试

建议展示样式：

```text
[PyPI] pypi-tsinghua
Stage: Download Artifacts (sub-stage: packages/a...z)
Progress: 18,420 / 95,203 files (19.3%)
Speed: 84.2 MB/s | ETA: 2h 12m
Health: warning | 404: 12 | Retry: 37 | Active Workers: 32
```

## 11.4 通用任务详情页

所有 provider 共用一套详情布局：

1. **Overview**
   - jobId
   - profile
   - provider
   - 当前阶段
   - 起止时间

2. **Progress**
   - 总进度
   - 阶段进度
   - 当前速率 / 平均速率 / ETA

3. **Stats**
   - 请求数
   - 成功数
   - 跳过数
   - 失败数
   - 404 数
   - 重试数

4. **Resources**
   - CPU
   - 内存
   - 元数据区磁盘占用
   - 镜像区磁盘占用
   - 网络吞吐

5. **Recent Events**
   - 最近成功事件
   - 最近错误事件
   - 最近状态切换

6. **Actions**
   - 暂停
   - 恢复
   - 取消
   - 重试失败项
   - 导出日志


## 12. TUI 设计

## 12.1 页面结构

建议采用三栏或两栏布局，顶部和底部保留状态区：

```text
+----------------------------------------------------------------------------------+
| mirror-sync | Profile: pypi-tsinghua | Running Job: job-20260725-001 | F1 Help   |
+----------------------+-----------------------------------------------------------+
| Navigation           | Main Panel                                                |
| - Dashboard          |                                                           |
| - Profiles           |                                                           |
| - Snapshots          |                                                           |
| - Plans              |                                                           |
| - Transfers          |                                                           |
| - Logs               |                                                           |
| - Settings           |                                                           |
+----------------------+-----------------------------------------------------------+
| Status: Running | Metadata 62% | Download 18% | 82 MB/s | Errors 3 | Disk 71%    |
+----------------------------------------------------------------------------------+
```

## 12.2 页面规划

### A. Dashboard

展示：

1. 当前 profile
2. provider 分类的任务统计
3. 最近一次快照时间
4. 当前运行任务及阶段
5. 元数据区 / 镜像区磁盘使用率
6. 实时下载速率
7. 成功 / 跳过 / 失败 / 404 统计
8. 活跃 worker 数
9. 健康告警汇总

建议补充两个视图切换：

1. **按 provider 分组**
   - PyPI
   - Ubuntu

2. **按任务状态分组**
   - Running
   - Paused
   - Failed
   - Completed

### B. Profiles

用于配置镜像源与同步参数：

1. source URL
2. metadataRoot
3. mirrorRoot
4. 并发数
5. timeout / retry
6. 过滤规则
7. 快照保留策略
8. provider 专属参数

操作：

1. 新建 profile
2. 编辑 profile
3. 启用 / 禁用 profile
4. 测试连接

建议配置页拆为两个层级：

1. **通用配置**
   - 名称
   - provider 类型
   - metadataRoot
   - mirrorRoot
   - 并发
   - retry
   - timeout

2. **provider 专属配置**
   - PyPI: simple URL、JSON metadata 开关、wheel 过滤规则
   - Ubuntu: release、component、arch、是否保留 source package

### C. Snapshots

展示：

1. 历史元数据快照列表
2. 快照大小
3. 包数量
4. 文件数量
5. 创建时间

操作：

1. 选择 old/new 快照
2. 生成 diff
3. 查看快照详情
4. 标记为 current

建议快照详情页提供：

1. provider 类型
2. manifest 版本
3. 包总数 / 文件总数
4. 生成耗时
5. 失败包数
6. 可直接发起 compare

### D. Plans

展示本次增量计划：

1. 新增文件数
2. 变化文件数
3. 计划下载总大小
4. 按包名 / 后缀 / 平台过滤
5. 风险提示，如磁盘空间不足、404 历史偏高、计划量异常

操作：

1. 审核计划
2. 启动下载
3. 导出计划
4. 仅下载新增
5. 仅下载变化
6. 按规则剔除部分文件

### E. Transfers

展示下载执行中的实时状态：

1. 总进度条
2. 当前阶段进度条
3. 当前速度、平均速度、ETA
4. 活跃下载列表
5. 最近错误
6. 每线程 / 每 worker 状态
7. 当前 provider 子阶段说明

操作：

1. 暂停
2. 恢复
3. 取消
4. 重试失败项

建议把 Transfers 拆成三个子页签：

1. `Overview`
2. `Workers`
3. `Failures`

### F. Logs

展示：

1. 结构化事件日志
2. 错误日志
3. 告警日志

支持过滤：

1. jobId
2. 级别
3. provider
4. 包名
5. 路径前缀

### G. Settings

全局设置：

1. 默认并发
2. 默认日志级别
3. UI 刷新频率
4. 默认排序方式
5. 是否开机自动加载最近 profile


## 13. PyPI 任务的 TUI 细化

这里专门补充 `PyPI` 在 TUI 中需要看到的更细粒度内容。

## 13.1 PyPI 任务类型

建议在 TUI 中将 PyPI 任务拆成四类入口动作：

1. **同步包列表**
   - 只抓 `/simple/`
   - 适合日常快速刷新

2. **同步元数据快照**
   - 拉取包列表
   - 拉取每个包的 `index.html` / `index_v1.json`
   - 生成 manifest

3. **比较两个快照**
   - 生成 diff
   - 展示新增/变化/删除统计

4. **执行镜像文件下载**
   - 基于计划下载 artifacts
   - 支持过滤、暂停、恢复、重试

这样用户在 TUI 里可以明确区分：

1. “我只是想更新元数据”
2. “我想基于新旧快照做增量下载”

## 13.2 PyPI 专属阶段展示

对 `同步元数据快照` 任务，建议展示以下子阶段：

1. `Fetch /simple/`
2. `Parse Package List`
3. `Download Package HTML Metadata`
4. `Download Package JSON Metadata`
5. `Normalize Package Metadata`
6. `Build Snapshot Manifest`
7. `Finalize Snapshot`

对 `执行镜像文件下载` 任务，建议展示以下子阶段：

1. `Load Manifest`
2. `Resolve Artifact Paths`
3. `Apply Filters`
4. `Build Download Plan`
5. `Check Existing Files / Checkpoint`
6. `Download Artifacts`
7. `Verify Hash`
8. `Finalize Download`

## 13.3 PyPI 专属统计项

Dashboard 或详情页里，PyPI 任务建议额外显示：

1. 包总数
2. 已完成 metadata 包数
3. 缺失 `index.html` 的包数
4. 不支持 JSON metadata 的包数
5. 解析出的 artifact 总数
6. 过滤后 artifact 数
7. 新增 artifact 数
8. 变化 artifact 数
9. 已下载字节数
10. hash 校验失败数

## 13.4 PyPI 专属过滤器

在 Plans 或 Profiles 页里，建议直接暴露这些可配置项：

1. 是否下载 source package
2. 是否下载 `py3-none-any`
3. 是否保留 Linux amd64 wheel
4. 是否保留 Windows amd64 wheel
5. 是否排除 MacOS wheel
6. 是否排除 ARM wheel
7. 是否排除 musllinux
8. 是否按包名 include / exclude
9. 是否按文件扩展名 include / exclude
10. 是否按文件大小阈值过滤

## 13.5 PyPI 专属异常提示

PyPI 页面里建议增加专门的异常摘要区：

1. `/simple/` 页面拉取失败
2. 包目录 HTML 解析失败
3. JSON metadata 不可用
4. 相对路径无法归一化
5. hash 缺失
6. 下载 URL 跳转异常
7. 404 比例异常升高

这类异常需要支持“一键查看样本”，不要只显示计数。


## 14. TUI 交互建议

### 14.1 快捷键

1. `Tab`：切换主面板
2. `j/k` 或方向键：上下移动
3. `Enter`：进入详情 / 执行动作
4. `s`：开始同步
5. `p`：暂停/恢复
6. `r`：重试失败项
7. `d`：生成 diff
8. `/`：搜索
9. `q`：退出

### 14.2 交互原则

1. 大任务都要有“预览 -> 确认 -> 执行”
2. 删除类操作必须二次确认
3. 错误要有“可重试”和“可定位上下文”
4. 实时进度不要只显示百分比，还要显示：
   - 当前阶段
   - 速度
   - 剩余数
   - ETA


## 15. 面向未来 Ubuntu 的共用与差异

为了避免未来接入 `Ubuntu mirror` 时推翻 TUI，建议提前明确哪些区域共用，哪些区域 provider 专属。

## 15.1 可以共用的区域

1. Dashboard
2. Profiles 的通用配置页
3. Snapshots 列表
4. Plans 列表
5. Transfers 监控
6. Logs
7. Settings
8. 任务状态栏
9. 资源监控

## 15.2 provider 专属的区域

1. Profile 的专属配置表单
2. Snapshot 详情里的 provider 统计项
3. Plan 详情里的过滤器
4. Transfers 详情里的子阶段说明
5. 错误诊断面板

## 15.3 TUI 中建议的 provider 适配方式

建议每个 provider 提供一份 UI 元数据，而不是把判断逻辑散落在页面里：

```ts
interface ProviderUiSpec {
  providerType: 'pypi' | 'ubuntu';
  displayName: string;
  commonUnits: Array<'packages' | 'files' | 'bytes' | 'requests'>;
  stageLabels: Record<string, string>;
  statsSchema: Array<{ key: string; label: string }>;
  filterSchema: Array<{ key: string; label: string; type: string }>;
  warningTypes: Array<{ key: string; label: string }>;
}
```

这样 Ubuntu 接入时只需要补自己的：

1. stage label
2. stats schema
3. filter schema
4. warning 类型

页面组件本身不需要重写。


## 16. 任务编排建议

一个 PyPI 全流程任务建议拆为：

1. `FetchPackageList`
2. `FetchPackageMetadata`
3. `BuildSnapshotManifest`
4. `CompareSnapshots`
5. `BuildDownloadPlan`
6. `DownloadArtifacts`
7. `VerifyArtifacts`
8. `FinalizeJob`

这样在 TUI 中可以精确展示阶段进度，而不是只有一个大进度条。


## 17. 与 Ubuntu mirror 的兼容预留

虽然当前先做 PyPI，但架构要预留 `provider` 抽象：

```ts
interface MirrorProvider {
  type: 'pypi' | 'ubuntu';
  fetchMetadata(profile): AsyncGenerator<JobEvent>;
  buildManifest(snapshotPath): Promise<SnapshotManifest>;
  compareSnapshots(oldSnapshot, newSnapshot): Promise<DiffPlan>;
  buildDownloadPlan(diffPlan, profile): Promise<DownloadPlan>;
}
```

这样后续 Ubuntu 只需要替换：

1. 元数据抓取方式
2. manifest 生成规则
3. URL 相对路径解析规则

下载执行器与 TUI 大部分都可以复用。


## 18. 建议的最小可行版本

### Phase 1

1. 完成 PyPI profile 配置
2. 完成元数据快照抓取
3. 完成 snapshot manifest 生成
4. 完成两份快照 diff
5. 完成 artifact 增量下载
6. TUI 先实现：
   - Dashboard
   - Profiles
   - Snapshots
   - Transfers

### Phase 2

1. 增加计划预览页
2. 增加失败任务重放
3. 增加 hash 校验
4. 增加速率限制与磁盘阈值告警
5. 增加更细粒度过滤器

### Phase 3

1. 增加 Ubuntu provider
2. 增加任务调度与定时运行
3. 增加导出报表


## 19. 风险与关键决策

需要尽早定下来的点：

1. **状态存储方式**
   - 建议：`SQLite + JSONL 日志`
   - 原因：任务恢复、筛选、统计会简单很多

2. **过滤规则是否内建**
   - 当前 Python 脚本里已有“只保留 source/any/linux amd64/windows amd64”这类经验
   - 建议将其做成 profile 配置，而不是写死

3. **metadata snapshot 的粒度**
   - 建议保留完整快照，而不是只保留 diff
   - 这样更适合审计、回滚、重新生成计划

4. **下载与解析是否解耦**
   - 建议完全解耦
   - 解析阶段只产出 manifest / plan
   - 下载阶段只消费 plan


## 20. 推荐落地实现顺序

1. 先用 TS 重写 `PyPI metadata fetcher`
2. 再实现 `manifest builder`
3. 再实现 `snapshot diff planner`
4. 再实现 `artifact downloader`
5. 最后接上 `Ink TUI`

原因：

1. 先把核心数据模型做稳
2. UI 可以更轻松地基于事件和状态来展示
3. 也便于保留 CLI 入口，后续可无 UI 批量运行


## 21. 建议的仓库结构

```text
src/
  app/
    tui/
      screens/
      components/
  core/
    jobs/
    planner/
    downloader/
    storage/
    events/
  providers/
    pypi/
      fetch-package-list.ts
      fetch-package-metadata.ts
      build-manifest.ts
      compare-snapshots.ts
    ubuntu/
  shared/
    types/
    utils/
docs/
  mirror-sync-tui-design.md
scripts/
  fetch_all_packages_metadata.py
  missing_files_downloader.py
```


## 22. 结论

这个工具最适合做成：

1. **一个可无头运行的同步引擎**
2. **一个基于 TypeScript 的全屏 TUI 管理界面**

其中 PyPI 首版的关键不是“先把所有 UI 都做满”，而是先把下面三件事做扎实：

1. 元数据快照化
2. 基于 snapshot manifest 的增量比较
3. 基于 URL 相对路径保真的 artifact 下载

这三层稳住之后，TUI 只是在上面做可视化、配置和控制，会非常顺。
