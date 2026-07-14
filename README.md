# FireScribe

FireScribe 是一个面向扫描手写文章的转录、审校和归档工作台。当前实现覆盖完整初版闭环：导入 PDF/图片、原件哈希归档、页面登记和缩略图、OpenAI 兼容 OCR/VLM 适配器、页面级候选稿、左图右文校对、SQLite FTS5 中文子串检索、Markdown/TXT/DOCX/PDF 导出、页级/区域批注与存疑、多结果差异视图、低置信复核队列以及可取消/重试的后台任务。

## 文档

- [Initial idea](docs/idea.md)
- [Initial technical spec](docs/spec.md)

## 运行

依赖：

- Go 1.26+
- Node.js 24+
- Poppler `pdfimages`、`pdfinfo`、`pdftoppm`（优先直提每页唯一嵌入图；复杂 PDF 回退按页光栅化，均由 poppler-utils 提供）
- 中文 TrueType 字体（服务器生成中文 PDF 使用；Linux 推荐安装 `fonts-droid-fallback`，也可用 `FIRESCRIBE_PDF_FONT=/path/to/font.ttf` 指定字体）
- 内置纯 Go SQLite，支持 FTS5/trigram；Makefile/CI 统一带 `sqlite_fts5` tag

安装前端依赖并构建：

```bash
npm --prefix web install
npm --prefix web run build
```

启动后端和已构建前端：

```bash
go run -tags sqlite_fts5 ./cmd/firescribe-server
```

默认地址：[http://localhost:8080](http://localhost:8080)

开发期也可以用：

```bash
make server
npm --prefix web run dev
```

Vite 开发服务会把 `/api` 代理到 `localhost:8080`。

## OCR 配置

服务启动时会读取根目录的 `config.json`。默认配置模板内置在程序中并随二进制打包；如果文件不存在，会自动释放一份默认配置；如果后续版本新增配置项，启动时也会把缺失项补齐到现有 `config.json`。`config.json` 可能包含密钥，默认不提交；可参考 `config.example.json`。

默认使用 mock OCR，便于导入、校对、搜索、导出全流程本地验证。要接入 OpenAI 兼容视觉模型，修改：

```json
{
  "use_mock_ocr": false,
  "openai": {
    "base_url": "https://api.openai.com/v1",
    "api_key": "your-api-key",
    "model": "your-vision-model"
  }
}
```

常用配置项：

- `addr`：监听地址，默认 `:8080`
- `data_dir`：数据目录，默认 `data`
- `database_path`：数据库路径，默认 `data/firescribe.db`
- `web_dir`：静态前端目录，默认 `web/dist`
- `prompt_path`：转录 prompt 文件，默认 `prompts/vlm_transcribe_page_v1.txt`
- `request_timeout_seconds`：OCR 单次请求超时秒数，默认 `120`
- `pdf_render_dpi`：PDF 导入光栅化 DPI，默认 `200`
- `update`：OTA 配置，包含 `enabled`、`channel`、`check_interval`、`proxy_base_url`、`repo`
- `openai`：OpenAI 兼容 OCR/VLM 配置，包含 `base_url`、`api_key`、`model`、`prompt_version`、`temperature`、`max_tokens`（默认 `32768`,输出超限会被判定为截断失败）、`max_image_edge`（上送前长边像素上限,默认 `2048`,`0` 关闭缩放）、`retry_attempts`（429/5xx/网络错误的重试次数,默认 `3`）

以上 OCR 相关配置也可以在网页设置页中修改（`GET/PUT /api/settings`,修改会热生效并写回 `config.json`;远程访问修改需配置 `update.admin_token`）。

## 识别管线

- 导入支持一次上传多个文件：多张图片按顺序合成一个文档的多页；PDF 在每页恰好包含一张可提取嵌入图时用 `pdfimages` 保留原格式和原始字节，不满足条件或缺少直提工具时自动回退 `pdftoppm` 按页光栅化；TIFF/BMP/GIF 会转码为 JPEG。大 PDF 会先用 `pdfinfo` 预探测页数，再按页调用 Poppler 并先完整写入 staging；只有整份 PDF 提取成功后才登记页面，因此直提中途失败可安全回退光栅化，不会留下半导入页面，任务进度也保持单调前进。工具不可用时仍保留兼容的批量回退并随着实际发现页面动态增长总数。
- 每次识别是一个 run,按页跟踪状态与错误（`GET /api/recognition-runs/{id}/pages`）,进度字段 `done_pages/total_pages/failed_pages` 可轮询。
- 部分页失败时 run 标记 `partial`,可通过 `POST /api/recognition-runs/{id}/retry` 只重跑未成功的页;进行中的 run 可 `POST .../cancel` 取消,同一文档同时只允许一个活跃 run（重复启动返回 409）。
- 识别输出会检查 `finish_reason`,被 `max_tokens` 截断或返回空文本都会判为该页失败,不会静默存入截断文本。
- 服务重启会把中断的 run/页标记为失败并复位文档状态,随后可整体重试。
- OpenAI 兼容响应中的显式 `confidence`/`score`、平均 logprob 或 token logprobs 会被归一化为页级置信度；无法可靠推导时保持为空，不伪造分数。

## 校对与版本

- OCR 结果可以明确保存为候选稿，并记录对应的 `source_result_id`。
- 每次人工保存和定稿都会创建独立文本版本；版本面板可查看正文、来源和上游版本，比较差异并恢复为新的人工草稿。
- `/review-queue` 汇总低于所选置信度阈值的未校对页面，以及仍有 open 存疑标记的页面。若 provider raw response 含 token logprobs 或常见 word/line/paragraph 置信结构，会列出可点击的低置信片段并按 UTF-16 范围直接定位编辑器；只有整页分数时保持页级回退，不伪造片段精度。
- 页级、文本范围和原图区域批注均支持解决、忽略与重新打开；还可先选择正文再框选原图，创建 `text_region_link` 双锚点批注（正文 UTF-16 范围、锚点文字与原图像素矩形），从文字或图框任一侧都能同时定位另一侧。

## 页图处理与预览

- 文档详情页可异步执行自动裁边、背景/阴影归一化、小角度倾斜矫正、对比度增强和基础文本带切分；每项处理都以配置快照写入 `page_processing_runs/results`。
- `pages.image_asset_id` 指向的导入页图始终不可变。处理结果以独立 `enhanced_page` 资产保存，失败、取消或重试都不会覆盖原图；任务页可查看进度并重试失败页。
- 原图/增强图可逐页对照预览，检测到的区域会持久化为 `page_segments`，为后续区域 OCR、版面切分和坐标联动保留稳定基础。
- 启动识别时可明确选择 `image_source=original|enhanced`。识别 run 和 result metadata 会记录实际资产、来源及处理结果 ID；选择增强图时缺少成功处理结果会明确报错，不会悄悄回退原图。

## Prompt 版本库

- 设置页可创建不可变 Prompt 快照、查看完整 SHA-256 与历史内容，并显式切换当前激活版本。
- 激活版本会同步更新 Prompt 文件和运行时配置；每次识别继续记录版本、哈希、参数、请求标识和原始响应。
- 兼容直接编辑 `prompt_path`：下次读取设置时会自动形成新快照，而不是覆盖已有历史。

## 插件化识别器与候选合并

- 设置页可维护多个数据型 Recognizer Profile；内置 allow-list 只支持 `openai-compatible` 与 `mock` 驱动，不加载动态库、不执行 Profile 指定的本地命令或任意代码。
- OpenAI-compatible Profile 可分别保存 `base_url`、API Key、模型、参数 JSON 和默认 Prompt 版本。数据库中的 `api_key`/`secret` 字段保持为空，实际凭据写入数据目录下权限为 `0600` 的 `secrets.json`；升级时已有数据库明文会自动迁出并清空。列表/API 响应和 run/experiment 快照只返回 secret-set 状态，不回显密钥明文。
- 另可创建 data-only Provider Adapter。当前只开放内置 `generic-http-json` 引擎：配置 HTTPS endpoint、模型、`none`/Bearer/`X-API-Key` 认证、超时、固定 JSON 字段路径和静态 JSON 字段；请求统一包含 Prompt、页面图的 base64/data URL 和文档/页元数据，响应按配置路径规范化出正文、置信度与 provider metadata，同时完整保留原始 JSON。Adapter manifest 不能引用本地文件、命令、模板脚本或动态代码。
- Provider Adapter 密钥只写不回显，也不会进入 run snapshot。其增删改与 Profile/Prompt/Settings 一样受 `update.admin_token`（未配置时仅 localhost）保护。Endpoint 必须是无 userinfo/query/fragment 的 HTTPS URL；服务端拒绝 localhost、私网/保留 IP，并在连接时再次检查 DNS 结果、禁用代理和重定向。该限制只是基础 SSRF 防线，仍应只配置由部署者信任的 OCR/VLM endpoint。
- 启动单次识别时可选择 Profile 或 Provider Adapter，并组合不可变 Prompt 版本与原图/增强图；旧版空请求仍兼容，优先使用默认 Profile，未配置时继续使用全局 Settings 和当前激活 Prompt。
- 每个 recognition run 固化 driver、Profile/Adapter ID、脱敏配置、Prompt 正文/ID/SHA-256、有界作者 prompt 上下文和图像来源。失败页重试从该不可变快照重建 base URL、模型、参数、Prompt 和作者上下文；OpenAI/Profile 与 Provider Adapter 只从现存记录取当前密钥，记录已删除或密钥缺失时明确失败，不会静默改用默认 Profile。Mock 可在 Profile 删除后仅凭快照复现。
- 文档详情页可创建同页多 Variant 的 Prompt/Profile/Adapter A/B 实验。创建时每个 Variant 都固化脱敏配置、Prompt 和作者上下文，排队或重试不会受后续 Profile/Adapter 修改影响，只读取当前同一记录的密钥；一个 `recognition_experiment` 后台任务按顺序启动各 Variant，复用现有同文档单活跃 run 约束。实验会在子 run 启动时以同一数据库事务保存关联 run ID，并分别保留历史 `run_ids` 与当前尝试 `current_run_ids`：前者用于完整 provenance，后者用于本次平均置信度、耗时和人工编辑距离，重试不会重复累计旧尝试。最后可显式选择 winner；重启会同步终结 job、experiment 和 variant。单次 recognition run API 保持兼容。
- 校对页“分歧”面板可选择两个或更多 `recognition_result` 调用专用 `vlm_merge_candidates_v1` 保守合并。服务端只接受在来源候选中逐行原样可见的文本，检测到扩写或新行会拒绝且不创建版本；成功后保存 candidate `TextVersion`，并在 `candidate_merges` 中记录全部来源 result ID、Profile/driver、Prompt 版本/哈希、原始响应和自动推导的逐行来源血缘，低置信片段仍能映射到合并稿。
- 分歧面板也支持按行或按段对齐两个识别结果，并为每一段人工选择来源；组合候选稿会逐段保存来源 result ID、顺序、来源 UTF-16 范围和输出 UTF-16 范围。版本历史会识别两类 candidate merge，展示完整来源、逐段血缘、Prompt/哈希，并可折叠查看 raw response。

## 项目与合集

- `/projects` 可创建项目，将多份文档加入同一合集并手动调整顺序；删除项目不会删除原文档。
- 项目导出使用持久化后台任务，按照项目顺序一次性生成合法的 Markdown、TXT、DOCX 或 PDF 文件。
- 项目导出与单文档导出共用“当前稿/最终稿、页码、批注、存疑标记”选项，并支持任务状态轮询、失败重试与完成后下载。

## 作者笔迹档案与训练数据

- `/authors` 可建立作者档案，维护作者说明、专有词、常见误识别写法与权重，并关联已有文档；原有 `documents.author` 字符串继续兼容，首次关联会在作者为空时自动回填名称。
- 人工稿和定稿会自动积累“识别原文 → 校对结果”样本；关联已有文档时会回填历史版本，也可在档案页手动重新同步。
- 识别前会把该作者的高权重词表和有界历史纠错样例追加为提示上下文，并把档案 ID、实际使用的词条/样例和上下文 SHA-256 固化到 run 配置快照，后续编辑档案不会改变历史记录。
- 档案页可下载 JSONL 训练/评测数据；每条记录包含原图页面 URL 与 asset ID、文档/页定位、识别 provider/model/prompt version、源 result、校对前后文本和版本 ID。
- 档案页同时按 Provider/模型/Prompt 汇总精确字符错误率（CER）、编辑距离、替换/漏识别/猜补，并展示按日趋势和高频错误；历史 correction 会补算缓存，新校对样本自动进入统计。

## 识别评测

- `/evaluation` 以每页最新最终定稿作为人工真值，并沿 `base_version_id` 找到实际校对所用候选稿（合并候选单独归因），计算字符错误率、替换/漏字/猜补、漏行/猜补行/乱序、低置信片段命中率、导入到候选耗时、候选到定稿墙钟时间和活跃小时确认页数。
- 默认只统计带“基准”或 `benchmark` 标签的文档，便于按 idea 建议维护 20–50 页真实扫描基准集；也可切换查看全部已有定稿的页面。逐页结果和模型/Prompt 聚合都可直接回到校对页复查。
- 校对页会把可见且最近 60 秒内有键盘、指针或滚动操作的时间累计到 `review_activity_sessions`，评测据此给出人工活跃校对时长与每活跃小时确认页数；“候选到定稿”仍单列为包含排队/等待的墙钟时间。最多评估最近 200 个样本，较大语料建议固定基准集保证可比性。

## 后台任务

- 文档导入、页图处理、单文档/项目导出、全文索引重建、OCR 识别和 Prompt/Profile A/B 实验均会写入 `jobs`，记录尝试次数、进度、阶段消息、错误与结果；`job_events` 另外保留排队、每次尝试、阶段进度、成功、失败、取消和重试的时间线。
- `/jobs` 页面支持查看进度与事件日志、取消运行中的任务、重试未超过最大次数的失败任务，并可手动触发全文索引重建。
- 导出拥有独立状态记录；前端等待任务完成后再下载文件。

## 导出

- 支持 Markdown、TXT、DOCX 和 PDF 审校版；PDF 会把原始页图、转录文本和所选审校记录排入同一文件。
- 可选择“当前稿”或“仅最终定稿”。仅最终定稿模式会跳过尚未定稿的页面，且在没有任何定稿页面时明确失败。
- 可独立选择保留原始页码、页级/区域批注和仍处于 open 状态的存疑标记；这些选项会持久化到异步 export job，失败重试不会改变导出快照。
- 每次单文档或项目导出都会逐页保存实际采用的 document/page、精确 `text_version_id`/kind、批注及其渲染方式和项目顺序；文档/项目详情可查看历史并检查任一次导出快照。PDF 中的区域和双锚点批注会直接叠加橙色框与编号。
- DOCX 由服务端直接生成标准 OOXML；PDF 由纯 Go 渲染器生成，不依赖桌面 Office/LibreOffice。中文 PDF 需要服务器可读取 Unicode `.ttf` 字体；常见 Linux、Windows 字体路径会自动探测，也可通过 `FIRESCRIBE_PDF_FONT` 显式配置。渲染器会对 CJK 与拉丁/数字分段回退，避免轻量中文字体缺字导致页码方框。

## OTA 与 CI

`/system` 页面提供版本信息、手动检查、下载、应用和忽略更新。`stable` 通道检查最新正式 release，下载完成后自动替换并重启；`dev` 通道跟踪固定的 `dev` prerelease，下载完成后等待用户确认重启。

`.github/workflows/cross-compile.yml` 会在 push 到 `master`/`main` 或推送 `v*` tag 时构建前端、嵌入静态资源、运行 Go 测试、交叉编译并发布：

- `master`/`main`：刷新固定 `dev` prerelease，并附带 `version.json`
- `vX.Y.Z` tag：发布正式 release

本地构建并运行：

```bash
scripts/dev-build-run.sh
```

下载 CI 最新构建：

```bash
scripts/fetch-latest-build.sh --download-only
```

## 验证

```bash
go test -tags sqlite_fts5 ./...
npm --prefix web run build
npm --prefix web run test
npm run stage:web
```

后端集成测试覆盖：图片/PDF 导入与页级进度、mock 识别、生成候选稿、保存定稿、片段低置信解析、作者 CER 与全局基准评测、中文 trigram 搜索、Markdown/TXT/DOCX/PDF 导出及精确快照、当前稿/最终定稿选择、页级/区域批注与存疑标记、Prompt 快照激活、Provider Adapter SSRF/密钥隔离与响应规范化、不可变重试快照、A/B 实验、job 事件、项目排序和跨文档合并导出。
