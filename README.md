# FireScribe

FireScribe 是一个面向扫描手写文章的转录、审校和归档工作台。当前实现覆盖完整初版闭环：导入 PDF/图片、原件哈希归档、页面登记和缩略图、OpenAI 兼容 OCR/VLM 适配器、页面级候选稿、左图右文校对、SQLite FTS5 中文子串检索、Markdown/TXT 导出、页级批注/存疑、多结果差异视图和失败任务重试。

## 文档

- [Initial idea](docs/idea.md)
- [Initial technical spec](docs/spec.md)

## 运行

依赖：

- Go 1.26+
- Node.js 24+
- Poppler `pdftoppm`（PDF 按页光栅化,poppler-utils 提供）
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

- 导入支持一次上传多个文件：多张图片按顺序合成一个文档的多页;PDF 用 `pdftoppm` 按页光栅化;TIFF/BMP/GIF 会转码为 JPEG。
- 每次识别是一个 run,按页跟踪状态与错误（`GET /api/recognition-runs/{id}/pages`）,进度字段 `done_pages/total_pages/failed_pages` 可轮询。
- 部分页失败时 run 标记 `partial`,可通过 `POST /api/recognition-runs/{id}/retry` 只重跑未成功的页;进行中的 run 可 `POST .../cancel` 取消,同一文档同时只允许一个活跃 run（重复启动返回 409）。
- 识别输出会检查 `finish_reason`,被 `max_tokens` 截断或返回空文本都会判为该页失败,不会静默存入截断文本。
- 服务重启会把中断的 run/页标记为失败并复位文档状态,随后可整体重试。

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
npm run stage:web
```

后端集成测试覆盖：导入图片、mock 识别、生成候选稿、保存定稿、中文 trigram 搜索、Markdown 导出、批注创建和解决。
