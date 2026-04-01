# go-rag

基于 `gin + gorm + mysql + Eino + Qdrant` 的 Go RAG MVP，包含知识库管理、文本文档/PDF 导入与切分入库、向量检索，以及 OpenAI 兼容的 `POST /v1/chat/completions` 接口。聊天模型和 embedding 模型现已分离配置，compose 默认内置本地 `all-MiniLM-L6-v2` embedding 服务。

## 架构

- `cmd/server`: 服务启动入口
- `internal/config`: 环境变量配置加载
- `internal/entity`: MySQL 元数据模型
- `internal/store`: MySQL 与 Qdrant 基础接入
- `internal/service`: 知识库、文档入库、RAG Chat 核心逻辑
- `internal/handler`: 内部 API 与 OpenAI 兼容接口
- `internal/llm`: Eino OpenAI ChatModel 与 Embedder 初始化
- `internal/ingest`: 简单文本切分器

RAG 主链路：

1. 文本文档或 PDF 通过内部 API 导入 MySQL。
2. 触发索引时将文本切分、调用 Eino OpenAI Embedder 生成向量。
3. 向量写入 Qdrant，元数据写入 `document_chunks`。
4. 聊天请求进入 `POST /v1/chat/completions`。
5. 服务按知识库检索 Qdrant 相似 chunk，用 Eino `Chain` 组织检索后的提示构造与生成；`stream=true` 时通过 SSE 按 chunk 向外刷出。

## 评测与观测

RAG 系统的质量保障采用三层闭环：

- `RAGAS` 用于离线评测，衡量检索命中、上下文相关性与回答质量
- `DeepEval` 用于 CI 门禁，将核心评测指标转成可执行的 fail/pass gate
- `Phoenix` 用于生产环境 tracing 与 observability，在真实流量下持续追踪链路表现，并在异常时下钻到 `span` 级别定位问题

这三层分别覆盖“离线打分、上线前拦截、上线后追踪”，避免质量问题只在线上暴露。

当前代码已接入 Phoenix OTLP tracing，覆盖：

- HTTP 根请求 span
- chat 主链路中的知识库选择、query rewrite、query embedding、Qdrant 检索、rerank、prompt 组装
- ingest 主链路中的 PDF 文本抽取、切分、chunk embedding、Qdrant collection / upsert
- knowledge base 与 Qdrant collection 生命周期操作

## MySQL 元数据表

- `knowledge_bases`
- `documents`
- `document_chunks`

## 环境变量

参考 [`.env.example`](/Users/dianwang-mac/Documents/workspace/go-rag/.env.example)：

- `APP_PORT`: HTTP 端口，默认 `8080`
- `MYSQL_DSN`: MySQL 连接串，必填
- `ADMIN_API_KEY`: `/api/*` 管理面鉴权 key，必填
- `QDRANT_HOST`: Qdrant 主机，默认 `127.0.0.1`
- `QDRANT_GRPC_PORT`: Qdrant gRPC 端口，默认 `6334`
- `OPENAI_BASE_URL`: 聊天上游的 OpenAI 兼容地址，默认 `https://api.openai.com/v1`
- `OPENAI_API_KEY`: 聊天上游 API Key，必填
- `OPENAI_CHAT_MODEL`: 默认聊天模型，默认 `gpt-4o-mini`
- `EMBEDDING_BASE_URL`: embedding 上游的 OpenAI 兼容地址；默认推荐 `http://127.0.0.1:6008/v1`
- `EMBEDDING_API_KEY`: embedding 上游 API Key；本地 TEI 服务通常可使用占位值 `-`
- `EMBEDDING_MODEL`: 默认向量模型，建议与本地服务加载的模型 ID 保持一致，默认 `sentence-transformers/all-MiniLM-L6-v2`
- `EMBEDDING_MODEL_ID`: 仅 compose 的 embedding 容器使用，默认 `sentence-transformers/all-MiniLM-L6-v2`
- `CHUNK_SIZE`: 切块大小，默认 `800`
- `CHUNK_OVERLAP`: 切块重叠，默认 `120`
- `PHOENIX_TRACING_ENABLED`: 是否启用 Phoenix tracing；默认在未配置时关闭
- `PHOENIX_OTLP_ENDPOINT`: Phoenix OTLP HTTP endpoint；建议使用 `http://127.0.0.1:6006/v1/traces`
- `PHOENIX_BASE_URL`: Phoenix REST API base URL；`evalctl export-trace` 未显式设置时会尝试从 `PHOENIX_OTLP_ENDPOINT` 推导
- `PHOENIX_PROJECT_NAME`: Phoenix project 名称；启用 tracing 时必填
- `PHOENIX_API_KEY`: Phoenix API Key；本地未开启鉴权时可留空
- `PHOENIX_EVENT_BODY_LIMIT`: trace 中大文本字段的截断上限，默认 `8192`

兼容说明：

- 旧的 `OPENAI_EMBEDDING_MODEL` 仍可继续使用一段时间；当 `EMBEDDING_MODEL` 未设置时会回退到它
- 若未设置 `EMBEDDING_BASE_URL` / `EMBEDDING_API_KEY`，embedding 侧会继续复用聊天侧的 `OPENAI_BASE_URL` / `OPENAI_API_KEY`
- 新部署建议直接改用 `EMBEDDING_*` 三个变量，避免 chat 和 embedding 配置继续耦合
- 即使 embedding 已切到本地服务，`app` 仍然要求 `OPENAI_API_KEY`，因为聊天侧没有改成本地模型
- `/api/*` 管理接口需要 `Authorization: Bearer <ADMIN_API_KEY>` 或 `X-API-Key: <ADMIN_API_KEY>`；`/v1/chat/completions` 不使用这条管理鉴权
- Phoenix tracing 启用后会自动把 `PHOENIX_PROJECT_NAME` 写入 OpenInference resource attribute，并用 `authorization: Bearer <PHOENIX_API_KEY>` 向 OTLP endpoint 发送 trace

## Phoenix Trace Export

当前仓库已经提供从 Phoenix trace 到 replay / score 的最小闭环。

### 1. 导出 trace 为样本

```bash
PHOENIX_BASE_URL=http://127.0.0.1:6006 \
PHOENIX_PROJECT_NAME=go-rag \
go run ./cmd/evalctl export-trace <trace_id>
```

如果你的 Phoenix 开启了鉴权，再额外提供：

```bash
PHOENIX_API_KEY=your-phoenix-api-key
```

`export-trace` 会：

- 从 Phoenix 项目里拉取对应 trace 的 spans
- 识别 `http.v1.chat_completions` trace
- 抽取 question、prompt、answer、retrieved chunks、知识库信息、模型配置
- 对新导出的样本额外保存结构化 prompt messages，避免多段 system prompt 在 replay 时被错误拆分
- 把样本持久化到 MySQL 的 `sample_records`
- 输出标准化 JSON，包含 `sample_id`

### 2. 用当前模型配置回放样本

```bash
MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/go_rag?charset=utf8mb4&parseTime=True&loc=Local' \
OPENAI_API_KEY=sk-xxxx \
OPENAI_BASE_URL=https://api.openai.com/v1 \
OPENAI_CHAT_MODEL=gpt-4o-mini \
go run ./cmd/evalctl replay-sample <sample_id>
```

`replay-sample` 会：

- 从 MySQL 读取导出的样本
- 优先复用样本里保存的结构化 prompt messages；旧样本再回退到扁平 prompt 文本解析
- 重新调用当前 chat model 生成答案
- 把回放结果持久化到 `replay_run_records`

### 3. 给 captured / replay 两个目标打分

```bash
MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/go_rag?charset=utf8mb4&parseTime=True&loc=Local' \
go run ./cmd/evalctl score-sample <sample_id>
```

`score-sample` 会：

- 读取导出样本
- 读取该样本最新一次 replay 结果
- 产出 `captured` 与 `replay` 两组 metric
- 把结果持久化到 `evaluation_result_records`

当前内置的 `score` 是最小可运行实现，使用启发式规则计算：

- `rewrite_fidelity`
- `retrieval_precision_at_k`
- `retrieval_relevance`
- `grounded_answer`
- `citation_correctness`
- `abstention_quality`

它适合先打通完整流程和回归验证，不等价于正式的 `EinoExt evaluator` 或 LLM-as-a-judge 评测。后续如果接上更完整的 evaluator，可以继续复用现有 `sample -> replay -> score` 这条链路。

### 4. 一条命令串起完整流程

如果你已经拿到 Phoenix 里的 `trace_id`，也可以直接：

```bash
MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/go_rag?charset=utf8mb4&parseTime=True&loc=Local' \
PHOENIX_BASE_URL=http://127.0.0.1:6006 \
PHOENIX_PROJECT_NAME=go-rag \
OPENAI_API_KEY=sk-xxxx \
OPENAI_BASE_URL=https://api.openai.com/v1 \
OPENAI_CHAT_MODEL=gpt-4o-mini \
go run ./cmd/evalctl run-trace <trace_id>
```

`run-trace` 会顺序执行：

- `export-trace`
- `replay-sample`
- `score-sample`

并一次性输出 `sample_id`、`replay`、原始 `results` 和按 `captured/replay` 聚合后的 `summary`。

### 5. 比较一组样本（用于 chunk 策略 A/B）

当你固定了一批 PDF 问答样本，想比较不同 chunk 参数的效果时，可以把这批样本的 `sample_id` 一次汇总：

```bash
MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/go_rag?charset=utf8mb4&parseTime=True&loc=Local' \
go run ./cmd/evalctl compare-samples <sample_id_1> <sample_id_2> <sample_id_3>
```

`compare-samples` 会输出：

- 每个样本的 `question / original_query / rewritten_query / chunk_count`
- 每个样本在 `captured/replay` 上的关键指标（当前聚焦：`retrieval_precision_at_k`、`grounded_answer`）
- 跨样本均值（按 `target + metric` 聚合）

这样你可以只改一个参数（例如 `CHUNK_SIZE` 或 `CHUNK_OVERLAP`），再对比同一批样本的均值变化，避免只看单条 case。

## 本地 Smoke Test

如果你想把“启动服务 -> 建库 -> 导文档 -> 提问 -> 反查 Phoenix trace -> `run-trace` 回放打分”一次串起来，可以直接跑：

```bash
OPENAI_API_KEY=sk-xxxx \
ADMIN_API_KEY=change-me \
bash ./scripts/smoke_phoenix.sh
```

这个脚本会默认：

- 使用 `docker compose -f docker-compose.yml -f docker-compose.phoenix.yml up -d --build`
- 等待 `app` 和 `phoenix` 就绪
- 创建临时知识库和文档
- 发起一次带唯一 token 的问答
- 从 `/api/debug/phoenix/spans` 里按问题内容反查最新 trace id
- 调用 `go run ./cmd/evalctl run-trace <trace_id>`
- 输出最终 JSON，包括 `trace_id`、`sample_id`、回答内容和评测结果

常用覆盖项：

- `APP_BASE_URL`: 默认 `http://127.0.0.1:8080`
- `PHOENIX_BASE_URL`: 默认 `http://127.0.0.1:6006`
- `PHOENIX_PROJECT_NAME`: 默认 `go-rag`
- `MYSQL_DSN`: 默认 `rag:rag@tcp(127.0.0.1:3306)/go_rag?...`
- `SMOKE_AUTO_START_STACK=0`: 跳过 compose 启动，直接打现有本地服务
- `SMOKE_KEEP_RESOURCES=1`: 运行后保留知识库和文档，方便手工检查

脚本依赖本机有 `jq`、`curl`、`docker compose` 和可用的 `OPENAI_API_KEY`。

## 启动

### 本地

```bash
cp .env.example .env
go mod tidy
go run ./cmd/server
```

说明：

- `/api/*` 管理接口必须携带 `Authorization: Bearer <ADMIN_API_KEY>`
- `/v1/chat/completions` 不使用这条管理鉴权

### Docker Compose

```bash
OPENAI_API_KEY=sk-xxxx \
ADMIN_API_KEY=change-me \
docker compose up --build
```

如果你要连 Phoenix 一起拉起来，改用：

```bash
OPENAI_API_KEY=sk-xxxx \
ADMIN_API_KEY=change-me \
docker compose -f docker-compose.yml -f docker-compose.phoenix.yml up --build
```

默认 compose 接线：

- `embedding`: `ghcr.io/huggingface/text-embeddings-inference:cpu-1.9`
- `embedding` 对宿主机暴露 `6008`，对 compose 内 `app` 暴露 `http://embedding:80/v1`
- `app` 默认使用 `EMBEDDING_BASE_URL=http://embedding:80/v1`
- `app` 仍然必须提供 `OPENAI_API_KEY` 和 `ADMIN_API_KEY`

启用 `docker-compose.phoenix.yml` 后，额外会有：

- `phoenix`: `arizephoenix/phoenix:latest`
- 宿主机访问地址：`http://127.0.0.1:6006`
- compose 内 `app` 默认把 trace 发到 `http://phoenix:6006/v1/traces`
- `evalctl` 默认通过 `PHOENIX_BASE_URL=http://127.0.0.1:6006` 读取 traces

如果聊天走云端模型、embedding 走 compose 内置本地服务，通常只需要：

```bash
OPENAI_API_KEY=sk-chat \
ADMIN_API_KEY=change-me \
docker compose up --build
```

如果你不想用 compose 内置 embedding，而是改接外部 OpenAI 兼容 embedding 服务，可显式覆盖：

```bash
OPENAI_API_KEY=sk-chat \
ADMIN_API_KEY=change-me \
EMBEDDING_BASE_URL=http://host.docker.internal:6008/v1 \
EMBEDDING_API_KEY=- \
EMBEDDING_MODEL=sentence-transformers/all-MiniLM-L6-v2 \
docker compose up --build
```

本地直接 `go run` 时，如果 embedding 仍使用 compose 启起来的本地服务：

```bash
ADMIN_API_KEY=change-me \
OPENAI_API_KEY=sk-chat \
EMBEDDING_BASE_URL=http://127.0.0.1:6008/v1 \
EMBEDDING_API_KEY=- \
EMBEDDING_MODEL=sentence-transformers/all-MiniLM-L6-v2 \
go run ./cmd/server
```

`docker-compose.yml` 包含：

- `mysql`
- `qdrant`
- `embedding`
- `app`

## 人工联调页面

服务启动后可直接访问：

- `http://localhost:8080/web/`

这个页面用于人工联调与评测调试，覆盖：

- 创建知识库
- 文本/PDF 导入
- 文档索引
- 非流式/流式聊天
- Phoenix 最新 trace 抓取与详情查看
- 一键生成 `evalctl run-trace` 命令

## Compose 内置 all-MiniLM-L6-v2 embedding 服务

项目内置的 embedding 容器使用 Hugging Face Text Embeddings Inference，加载 `sentence-transformers/all-MiniLM-L6-v2`，并通过 OpenAI-style `/v1/embeddings` 暴露给应用。默认配置如下：

```dotenv
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-your-chat-key
OPENAI_CHAT_MODEL=gpt-4o-mini

EMBEDDING_BASE_URL=http://127.0.0.1:6008/v1
EMBEDDING_API_KEY=-
EMBEDDING_MODEL=sentence-transformers/all-MiniLM-L6-v2
EMBEDDING_MODEL_ID=sentence-transformers/all-MiniLM-L6-v2
```

说明：

- `EMBEDDING_BASE_URL` 指向本地服务的 `/v1` 根路径，实际请求为 `POST /v1/embeddings`
- `EMBEDDING_MODEL_ID` 控制 compose 里 embedding 容器实际加载的模型
- `EMBEDDING_MODEL` 是 app 发给 embedding 服务的模型名；默认与 `EMBEDDING_MODEL_ID` 保持一致
- `EMBEDDING_API_KEY` 是否真正校验取决于本地服务；TEI 本地接法通常可使用占位值 `-`
- 首次启动 embedding 容器会下载模型权重到 compose volume，冷启动时间取决于网络和机器性能
- 如果你的机器资源足够，也可以显式把 `EMBEDDING_MODEL_ID` / `EMBEDDING_MODEL` 改回 `BAAI/bge-m3`

## 内部 API 示例

所有 `/api/*` 示例都需要带上：

```bash
-H 'Authorization: Bearer change-me'
```

创建知识库：

```bash
curl -X POST http://localhost:8080/api/knowledge-bases \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "demo-kb",
    "description": "MVP knowledge base"
  }'
```

导入文本文档：

```bash
curl -X POST http://localhost:8080/api/documents/import-text \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "knowledge_base_id": 1,
    "title": "intro.txt",
    "content": "RAG means retrieval augmented generation."
  }'
```

或 multipart 上传：

```bash
curl -X POST http://localhost:8080/api/documents/import-text \
  -H 'Authorization: Bearer change-me' \
  -F knowledge_base_id=1 \
  -F title=intro.txt \
  -F file=@./intro.txt
```

导入 PDF：

```bash
curl -X POST http://localhost:8080/api/documents/import-pdf \
  -H 'Authorization: Bearer change-me' \
  -F knowledge_base_id=1 \
  -F title=rag-intro.pdf \
  -F file=@./rag-intro.pdf
```

说明：

- PDF 导入会先抽取纯文本，再复用现有 `documents` 存储和后续 `/api/documents/:id/index` 的切分、向量化、Qdrant 入库流程
- PDF HTTP 导入仅支持 multipart 上传，不再支持 `file_path` 这类服务端本地路径读取
- 当前使用 `github.com/ledongthuc/pdf` 做文本提取
- 不包含 OCR，扫描版 PDF 或以图片为主的 PDF 可能提取不到文本
- 多栏、复杂排版、表格类 PDF 的文本顺序可能与视觉顺序不完全一致

触发切分与向量入库：

```bash
curl -X POST http://localhost:8080/api/documents/1/index \
  -H 'Authorization: Bearer change-me'
```

查询文档列表：

```bash
curl 'http://localhost:8080/api/documents?knowledge_base_id=1' \
  -H 'Authorization: Bearer change-me'
```

## OpenAI 兼容接口

当前实现：

- `POST /v1/chat/completions`
- 支持 `model`、`messages`、`temperature`、`stream`
- `stream=false` 返回标准 JSON
- `stream=true` 返回 `text/event-stream`，以 OpenAI chat completions SSE 风格输出 `data: {...}` chunk，最后 `data: [DONE]`
- 额外支持 `knowledge_base_id` 或 `knowledge_base_name` 选择检索范围
- 额外支持 `document_ids` 与 `source_types` 做检索过滤，用于把召回范围收敛到指定文档或文档类型

示例：

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-4o-mini",
    "knowledge_base_id": 1,
    "source_types": ["text"],
    "temperature": 0.2,
    "stream": false,
    "messages": [
      {"role": "user", "content": "RAG 是什么？"}
    ]
  }'
```

如果你只想在一批指定文档内检索，也可以传：

```json
{
  "document_ids": [12, 15, 18]
}
```

流式示例：

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-4o-mini",
    "knowledge_base_id": 1,
    "stream": true,
    "messages": [
      {"role": "user", "content": "RAG 是什么？"}
    ]
  }'
```

## 已知限制

- PDF 仅做文本提取，不支持 OCR
- 索引流程为同步执行
- 流式输出当前聚焦文本 `delta.content`，未实现工具调用等更复杂的 OpenAI stream 字段
- token usage 为近似估算，不是上游精确统计
- 未实现认证、删除、重建索引、复杂过滤
