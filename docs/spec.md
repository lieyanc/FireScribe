# FireScribe 初版技术 Spec

更新时间：2026-06-29

## 目标

构建一个面向个人和小型档案整理场景的手写扫描稿数字化工具。后端使用 Go，前端使用 React、TypeScript 和 shadcn/ui，数据使用 SQLite，原始文件单独存储，数据库保留索引、元数据、任务状态、识别结果和人工校对版本。

## 已对齐约束

- 后端使用 Go。
- 前端使用 React、TypeScript 和 shadcn/ui。
- OCR/VLM 初版只对接 OpenAI 兼容格式的 API（base URL、API key、model、图文输入、文本输出），不再设计独立 OCR 中转协议。
- 首批内容以中文手写文章为主，默认是正常文章形态，不优先处理复杂版式、竖排和表格。
- 初版追溯粒度按原始页码处理，不要求 OCR 模型输出行级、段级或区域级坐标，避免增加识别负担。
- provider 返回的原始元数据需要完整保留，包括原始响应、置信度、坐标、版面信息、模型参数等可用字段，供后续增强使用。
- 导出保留格式应多种可选，初版先实现 TXT 和 Markdown，后续扩展 DOCX、PDF 审校版、带批注版本和干净定稿版本。
- 已确认首批 PDF 每页都是单独图片，初版按页码直接提取原始页面图并用于展示和识别，不重新栅格化为 PNG，也不为矢量/文本 PDF 设计复杂回退。
- 每个 page 都需要全链路可追溯（原件 → 页面图 → 识别 run/result → 文本版本；后续再接批注）和详细状态可见。
- FTS5 使用 trigram 分词以支持中文子串检索（默认 unicode61 不切中文）。
- MVP 暂不包含批注系统、存疑标记和项目层级；这些能力后置。

## 非目标

初版不追求：

- 完全自动无人工校对
- 完整排版还原
- 多人协作权限系统
- 云端同步
- 完全离线 OCR/VLM 模式
- 自训练 OCR 模型
- 对所有 OCR/VLM provider 的一次性支持
- MVP 内的批注系统、存疑标记和项目层级

## 推荐技术栈

### 后端

- Go 1.22+
- HTTP API：`net/http` + `chi`
- SQLite：`github.com/mattn/go-sqlite3`，WAL 模式，启用 FTS5/trigram
- SQL 管理：`sqlc`
- 数据迁移：`goose`
- 后台任务：初版使用进程内 worker + SQLite `jobs` 表
- 文件处理：Poppler `pdfimages`/`pdfinfo` 按页提取 PDF 原始图片，Go 图像库生成缩略图

选型理由：`mattn/go-sqlite3` 对 SQLite 原生能力、FTS5 和 trigram 支持更稳；FireScribe 初版优先可靠性，不优先纯 Go 分发。

### 前端

- React
- TypeScript
- Vite
- shadcn/ui
- Tailwind CSS
- TanStack Query：服务端状态、列表、任务轮询
- Zustand 或 React state：校对界面局部状态
- PDF/图片查看：初版直接展示提取出的原始页面图，缩略图单独生成 JPEG

### 存储

```text
data/
  firescribe.db
  originals/
    ab/cd/<sha256>.<ext>
  pages/
    <document_id>/
      page-0001.<ext>   # 按页码提取的原始页面图，不重新编码
      page-0002.<ext>
  thumbs/
    <document_id>/
      page-0001.jpg
  crops/                 # 后续区域批注/切图再启用
    <region_id>.png
  exports/
    <export_id>/
      output.md
      output.txt
```

数据库只存路径、hash、metadata、索引和状态，不存大型原图 blob。

页面图直接按页码提取 PDF 中的原始图片，保留原始格式与原始数据，不重新栅格化为 PNG。缩略图是唯一会重新编码的派生物（小尺寸 JPEG）。

## 后端模块

```text
cmd/firescribe-server/
internal/api/
internal/app/
internal/db/
internal/storage/
internal/importer/
internal/pageproc/
internal/recognizer/
internal/review/
internal/exporter/
internal/jobs/
internal/config/
web/
```

模块职责：

- `api`：HTTP handler、请求响应 DTO。
- `app`：应用服务，编排业务流程。
- `db`：SQLite 查询、migration、事务。
- `storage`：原件、页面图、缩略图、导出文件的路径和读写。
- `importer`：PDF/图片导入、hash、去重、文档创建。
- `pageproc`：按页码提取 PDF 原始页面图、登记图片页、生成缩略图。
- `recognizer`：OpenAI 兼容 OCR/VLM API 适配器。
- `review`：候选稿、人工文本版本、定稿状态。
- `exporter`：TXT、Markdown、后续 DOCX 导出。
- `jobs`：任务队列、重试、日志。

## 核心数据模型

设计原则：数据库保持**可靠简洁**——表只存必要字段，表之间关系靠显式外键表达，跨表的聚合状态用视图计算，而不是在行里维护容易不一致的冗余计数器。

### 外键与资产回收策略

- M0 起开启 SQLite 外键约束：每个连接初始化执行 `PRAGMA foreign_keys=ON`、`PRAGMA busy_timeout=5000`，并启用 WAL。
- migration 中为 `documents`、`pages`、`recognition_runs`、`recognition_results`、`text_versions`、`document_assets`、`document_tags` 写显式 `REFERENCES`。
- 删除文档时级联删除页面、识别运行、识别结果、文本版本和标签关联；`assets` 不级联删除，避免误删被其他文档复用的文件。
- 未被任何表引用的 asset 由后续 `asset_gc` 任务回收，MVP 可以先只登记为孤立资产、不立刻物理删除文件。

### documents

文档级元数据。

```sql
CREATE TABLE documents (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  page_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

### assets

原始文件和派生文件登记。

```sql
CREATE TABLE assets (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  original_name TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  byte_size INTEGER NOT NULL,
  storage_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(kind, sha256)
);
```

`kind` 可取：`original`、`page_image`、`thumbnail`、`export`；`crop` 后续配合区域批注再启用。

### document_assets

文档和资产关联。

```sql
CREATE TABLE document_assets (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  role TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (document_id, asset_id, role)
);
```

### pages

页面索引。

```sql
CREATE TABLE pages (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_no INTEGER NOT NULL,
  image_asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  thumb_asset_id TEXT REFERENCES assets(id) ON DELETE SET NULL,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(document_id, page_no)
);
```

### recognition_runs

一次识别运行。可以是整文档、单页或批量。

```sql
CREATE TABLE recognition_runs (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  prompt_version TEXT NOT NULL DEFAULT '',
  config_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL
);
```

### recognition_results

单页识别结果。

```sql
CREATE TABLE recognition_results (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES recognition_runs(id) ON DELETE CASCADE,
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  confidence REAL,
  raw_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(run_id, page_id)
);
```

### text_versions

候选稿、人工稿、定稿。

```sql
CREATE TABLE text_versions (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  base_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  source_result_id TEXT REFERENCES recognition_results(id) ON DELETE SET NULL,
  text TEXT NOT NULL,
  status TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT 'system',
  created_at TEXT NOT NULL
);
```

`kind` 可取：`raw_selected`、`candidate`、`manual`、`final`。

### annotations（M5 后续）

批注和存疑项。MVP 暂不实现，数据模型先作为后续扩展参考。

```sql
CREATE TABLE annotations (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
  text_version_id TEXT REFERENCES text_versions(id) ON DELETE SET NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  body TEXT NOT NULL,
  anchor_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

`anchor_json` 用于保存文本范围或页面区域，例如：

```json
{
  "type": "page_region",
  "x": 120,
  "y": 340,
  "width": 500,
  "height": 80
}
```

坐标使用页面图像像素坐标，并在 `pages` 表保存宽高。后续如有多尺寸展示图，可以补充归一化坐标。

### jobs

后台任务。

```sql
CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
```

### tags

```sql
CREATE TABLE tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  color TEXT NOT NULL DEFAULT ''
);

CREATE TABLE document_tags (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (document_id, tag_id)
);
```

### FTS

初版可对最终文本和候选文本建立全文索引。

```sql
CREATE VIRTUAL TABLE text_search USING fts5(
  document_id UNINDEXED,
  page_id UNINDEXED,
  text_version_id UNINDEXED,
  title,
  body,
  tokenize = 'trigram'
);
```

分词说明：SQLite FTS5 默认的 `unicode61` **不切中文**，一整句无空格的中文会被当成单个 token，`MATCH` 搜不到子串。改用 `trigram`（SQLite 3.34+ 自带）按 3 字符子串建索引，可支持中文子串检索，索引约为原文的 3 倍，个人文档量级可接受。注意 trigram 对 1~2 字短词召回差，单字/双字查询必要时用 `LIKE` 兜底；若后续需要词级召回再评估 jieba/ICU 分词。

FTS 更新策略：

- 保存 `final` 版本时更新索引。
- 若没有 `final`，可索引最新 `candidate`，但搜索结果标记为未定稿。

## 页面全链路追踪与详细状态

每一个 page 都要能从原始件一路追到定稿和导出，并且前端能看到详细状态。

### 全链路定义

对任意 page，链路为：

```text
document
  ├─ original asset (原件, sha256)
  └─ page
       ├─ page_image asset (提取自 PDF / 登记的图片)
       ├─ thumb asset
       ├─ recognition_result × N ──> recognition_run (provider / model / prompt_version / config / 原始返回)
       └─ text_version × N (candidate / manual / final；base_version_id + source_result_id 保留血缘)
```

任何一环都能回到 page 和 document：原件与页面图通过 `document_assets`/`pages` 关联，识别结果带所属 run 的 provider/model/prompt，文本版本带来源 result 和 base version 血缘。M5 批注启用后，批注再通过 `page_id`/`text_version_id` 接入同一链路。

### 追踪索引

为按页 / 按文档聚合查询建索引：

```sql
CREATE INDEX idx_recognition_results_page ON recognition_results(page_id);
CREATE INDEX idx_text_versions_page       ON text_versions(page_id);
CREATE INDEX idx_recognition_runs_doc     ON recognition_runs(document_id);
CREATE INDEX idx_text_versions_doc        ON text_versions(document_id);
```

M5 批注启用后补充 `idx_annotations_page`。

### 详细状态显示

`pages.status` 只表达粗粒度生命周期（`new`/`extracted`/`recognized`/...）。页面详情（识别次数、最近一次 provider/model、最高置信度、是否有 candidate/manual/final）用只读视图聚合，不在 `pages` 上维护冗余计数器，避免写路径复杂化：

```sql
CREATE VIEW page_details AS
SELECT
  p.id            AS page_id,
  p.document_id,
  p.page_no,
  p.status        AS page_status,
  p.width,
  p.height,
  (SELECT COUNT(*)          FROM recognition_results r WHERE r.page_id = p.id)         AS recognition_count,
  (SELECT MAX(r.confidence) FROM recognition_results r WHERE r.page_id = p.id)         AS best_confidence,
  (SELECT run.provider FROM recognition_results r
     JOIN recognition_runs run ON run.id = r.run_id
     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1)                         AS last_provider,
  (SELECT run.model FROM recognition_results r
     JOIN recognition_runs run ON run.id = r.run_id
     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1)                         AS last_model,
  (SELECT MAX(r.created_at)  FROM recognition_results r WHERE r.page_id = p.id)         AS last_recognized_at,
  EXISTS(SELECT 1 FROM text_versions v WHERE v.page_id = p.id AND v.kind = 'candidate') AS has_candidate,
  EXISTS(SELECT 1 FROM text_versions v WHERE v.page_id = p.id AND v.kind = 'manual')    AS has_manual,
  EXISTS(SELECT 1 FROM text_versions v WHERE v.page_id = p.id AND v.kind = 'final')     AS has_final,
  p.updated_at
FROM pages p;
```

前端文档详情页 / 校对页直接读这个视图即可得到一页的完整状态面板。数千页内 SQLite 即时计算足够，后续如有性能需要再考虑缓存或物化。

## 状态枚举

### document status

- `new`
- `importing`
- `ready`
- `recognizing`
- `reviewing`
- `finalized`
- `failed`

### page status

- `new`
- `extracted`
- `recognized`
- `reviewing`
- `verified`
- `failed`

M5 存疑标记启用后可增加 `uncertain`。

### job status

- `queued`
- `running`
- `succeeded`
- `failed`
- `canceled`

### annotation status（M5 后续）

- `open`
- `resolved`
- `ignored`

## Recognizer 接口

```go
type PageInput struct {
    DocumentID string
    PageID     string
    PageNo     int
    ImagePath  string
    Width      int
    Height     int
}

type RecognitionResult struct {
    Text       string
    Confidence *float64
    RawJSON    []byte
    Metadata   map[string]any
}

type Recognizer interface {
    Name() string
    Provider() string
    Model() string
    RecognizePage(ctx context.Context, input PageInput) (RecognitionResult, error)
}
```

识别器配置建议放在 `config.yaml` 或环境变量中，密钥不入库、不提交。初版只实现 OpenAI 兼容适配器，不直接绑定某个 OCR/VLM 厂商 SDK。配置至少包含 `base_url`、`api_key_env`、`model`、`prompt_version`、超时和模型参数；请求把页面原始图作为图文输入发送，响应至少规范化出文本、可选置信度和原始 JSON，其他字段完整保存在 raw metadata 中。

## 任务类型

初版任务：

- `import_document`
- `extract_pages`
- `generate_thumbnails`
- `recognize_page`
- `recognize_document`
- `build_candidate_text`
- `export_document`
- `rebuild_search_index`

任务执行原则：

- 每个任务可重试。
- 长任务写入进度和错误。
- provider 原始失败信息记录到 job `last_error`，不污染人工文本。
- OCR/VLM 调用失败不影响已有结果。

## API 初稿

### 文档库

```text
GET    /api/documents
POST   /api/documents/import
GET    /api/documents/{documentID}
PATCH  /api/documents/{documentID}
DELETE /api/documents/{documentID}
```

### 页面

```text
GET /api/documents/{documentID}/pages
GET /api/pages/{pageID}
GET /api/pages/{pageID}/image
GET /api/pages/{pageID}/thumbnail
```

### 识别

```text
POST /api/documents/{documentID}/recognition-runs
GET  /api/documents/{documentID}/recognition-runs
GET  /api/recognition-runs/{runID}
GET  /api/pages/{pageID}/recognition-results
```

### 文本版本

```text
GET  /api/pages/{pageID}/text-versions
POST /api/pages/{pageID}/text-versions
GET  /api/documents/{documentID}/final-text
```

### 批注（M5 后续）

MVP 暂不实现批注 API。

```text
GET   /api/documents/{documentID}/annotations
POST  /api/documents/{documentID}/annotations
PATCH /api/annotations/{annotationID}
```

### 搜索

```text
GET /api/search?q=...
```

### 导出

```text
POST /api/documents/{documentID}/exports
GET  /api/exports/{exportID}
GET  /api/exports/{exportID}/download
```

### 任务

```text
GET  /api/jobs
GET  /api/jobs/{jobID}
POST /api/jobs/{jobID}/cancel
```

## 前端页面

### 文档库页

功能：

- 文档表格
- 搜索
- 状态筛选
- 标签筛选
- 导入入口
- 处理进度

### 文档详情页

功能：

- 基本信息
- 页列表
- 识别运行列表
- 导出入口

### 校对页

布局：

```text
+---------------------------------------------------------+
| 文档 / 页码 / 状态 / 保存 / 导出                         |
+-----------------------------+---------------------------+
|                             | OCR/VLM 结果 tabs         |
| 页面图像查看器              +---------------------------+
|                             | 文本编辑器                |
|                             |                           |
+-----------------------------+---------------------------+
| 识别详情 / 任务状态                                      |
+---------------------------------------------------------+
```

初版交互：

- 上一页 / 下一页
- 保存当前页人工文本
- 标记已确认
- 切换不同识别结果
- 从识别结果复制为候选稿

建议快捷键：

- `Ctrl+S` 保存
- `J` 下一页
- `K` 上一页
- `V` 标记已确认

### 任务页

功能：

- 查看任务队列
- 查看失败原因
- 重试失败任务

## 识别结果合并策略

MVP：

- 不做复杂自动合并。
- 每页展示多个识别结果。
- 用户选择一个结果作为候选稿，或从空白开始编辑。
- 初版以原始页码作为追溯单位，不要求模型提供行/段/区域坐标。

v0.2：

- 对多个结果做文本 diff。
- 高亮不同模型之间的分歧。
- 允许逐段选择来源。

v0.3：

- 使用 LLM 做保守合并。
- prompt 明确要求只合并可见文本，不扩写、不补写。
- 合并结果必须保留来源 result id 和 prompt version。

## Prompt 版本管理

VLM 调用必须记录：

- provider
- model
- prompt version
- prompt hash
- input image id
- parameters
- raw response
- OpenAI 兼容 API 返回的 response id、request id 或 trace id（如果提供）

初版 prompt 可以放在代码或 `prompts/` 目录，命名例如：

```text
prompts/
  vlm_transcribe_page_v1.txt
  vlm_merge_candidates_v1.txt
```

## 错误处理

- 导入失败：文档状态为 `failed`，保留 job 错误。
- 单页识别失败：页面状态为 `failed`，不影响其他页。
- provider 限流：job 延迟重试。
- 文件重复导入：通过 sha256 识别，可创建新文档引用同一 asset，也可提示用户已存在。
- 人工保存失败：前端必须保留未提交编辑内容，避免丢稿。

## 安全与隐私

- 默认单人使用，暂不设计远程共享能力。
- 识别时页面原始图会发送到配置的 OpenAI 兼容 API；除此之外不做额外同步或上传。
- API key 使用环境变量或配置文件，加入 `.gitignore`。
- 导出文件放在 `data/exports`，由用户主动下载或打开。
- 后续如支持远程访问，需要补认证和访问控制。

## 测试策略

后端：

- storage 路径和 hash 去重测试
- migration 测试
- repository 测试
- recognizer mock 测试
- job 重试测试
- export snapshot 测试

前端：

- 文档列表渲染
- 校对页保存流程
- 识别结果切换

端到端：

- 导入一个小 PDF
- 提取页面图
- mock OCR/VLM 生成结果
- 保存人工定稿
- 搜索命中
- 导出 Markdown/TXT

## 里程碑

### M0：项目骨架

- Go server 启动
- SQLite migration
- React + shadcn/ui 前端
- 应用配置和数据目录

### M1：文档导入

- PDF/图片导入
- 原件 hash 存储
- 文档和页面表
- 页面图和缩略图

### M2：识别管线

- recognizer 接口
- 1 个 OpenAI 兼容 OCR/VLM 适配器
- 至少 1 个模型配置
- jobs 表和 worker
- recognition runs/results 保存

### M3：校对闭环

- 左图右文校对页
- 识别结果切换
- 候选稿和人工版本保存
- 页面状态更新

### M4：检索与导出

- FTS5 索引
- 搜索页面
- Markdown/TXT 导出

### M5：批注与差异

- 页级批注
- 文本存疑标记
- 多模型结果 diff
- 失败任务重试 UI

## 开放问题

- 是否需要为某个作者建立术语表、常见字词表或人名地名表？
- 默认导出配置如何排序：页码默认开还是关，后续存疑标记、批注、合并导出默认如何处理？

## 已关闭问题

- 初版形态：先按单人 Web 应用推进，后续如有需要再封装桌面应用。
- 离线模式：不需要完全离线 OCR/VLM。
- OCR/VLM 接入：初版只支持 OpenAI 兼容 API 格式。
- 数据库选型：使用 `github.com/mattn/go-sqlite3`、`sqlc` 和 `goose`；外键和资产回收由应用管理。
- 批注范围：MVP 暂不做批注系统和存疑标记，后置到 M5。
- 页面处理：首批 PDF 每页都是单独图片，按页码提取原始图即可，不做复杂渲染回退。
- 项目层级：后置，MVP 只做文档和标签。
- 页面追溯：v1 按原始页码追踪，不强制行/段/区域坐标。
- 导出能力：支持多种可选格式，初版先做 TXT 和 Markdown。
