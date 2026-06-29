# FireScribe

FireScribe 是一个面向扫描手写文章的转录、审校和归档工作台。当前实现覆盖完整初版闭环：导入 PDF/图片、原件哈希归档、页面登记和缩略图、OpenAI 兼容 OCR/VLM 适配器、页面级候选稿、左图右文校对、SQLite FTS5 中文子串检索、Markdown/TXT 导出、页级批注/存疑、多结果差异视图和失败任务重试。

## 文档

- [Initial idea](docs/idea.md)
- [Initial technical spec](docs/spec.md)

## 运行

依赖：

- Go 1.22+
- Node.js 20+
- Poppler `pdfimages`（PDF 按原始页图提取）
- SQLite FTS5/trigram；Go 命令需要带 `sqlite_fts5` tag

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

默认没有 API key 时使用 mock OCR，便于导入、校对、搜索、导出全流程本地验证。要接入 OpenAI 兼容视觉模型，设置：

```bash
export FIRESCRIBE_USE_MOCK_OCR=false
export FIRESCRIBE_OPENAI_BASE_URL=https://api.openai.com/v1
export FIRESCRIBE_OPENAI_MODEL=your-vision-model
export OPENAI_API_KEY=your-api-key
go run -tags sqlite_fts5 ./cmd/firescribe-server
```

可选环境变量：

- `FIRESCRIBE_ADDR`：监听地址，默认 `:8080`
- `FIRESCRIBE_DATA_DIR`：数据目录，默认 `data`
- `FIRESCRIBE_DB_PATH`：数据库路径，默认 `data/firescribe.db`
- `FIRESCRIBE_WEB_DIR`：静态前端目录，默认 `web/dist`
- `FIRESCRIBE_PROMPT_PATH`：转录 prompt 文件，默认 `prompts/vlm_transcribe_page_v1.txt`

## 验证

```bash
go test -tags sqlite_fts5 ./...
npm --prefix web run build
```

后端集成测试覆盖：导入图片、mock 识别、生成候选稿、保存定稿、中文 trigram 搜索、Markdown 导出、批注创建和解决。
