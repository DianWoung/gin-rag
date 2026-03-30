#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_BASE_URL="${APP_BASE_URL:-http://127.0.0.1:8080}"
PHOENIX_BASE_URL="${PHOENIX_BASE_URL:-http://127.0.0.1:6006}"
PHOENIX_PROJECT_NAME="${PHOENIX_PROJECT_NAME:-go-rag}"
PHOENIX_API_KEY="${PHOENIX_API_KEY:-}"
PHOENIX_SPAN_LIMIT="${PHOENIX_SPAN_LIMIT:-1000}"
ADMIN_API_KEY="${ADMIN_API_KEY:-change-me}"
OPENAI_BASE_URL="${OPENAI_BASE_URL:-https://api.openai.com/v1}"
OPENAI_CHAT_MODEL="${OPENAI_CHAT_MODEL:-gpt-4o-mini}"
MYSQL_DSN="${MYSQL_DSN:-rag:rag@tcp(127.0.0.1:3306)/go_rag?charset=utf8mb4&parseTime=True&loc=Local}"
EMBEDDING_BASE_URL="${EMBEDDING_BASE_URL:-http://127.0.0.1:6008/v1}"
EMBEDDING_API_KEY="${EMBEDDING_API_KEY:--}"
EMBEDDING_MODEL="${EMBEDDING_MODEL:-sentence-transformers/all-MiniLM-L6-v2}"
SMOKE_AUTO_START_STACK="${SMOKE_AUTO_START_STACK:-1}"
SMOKE_KEEP_RESOURCES="${SMOKE_KEEP_RESOURCES:-0}"
SMOKE_RUN_ID="${SMOKE_RUN_ID:-$(date +%s)}"

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  echo "OPENAI_API_KEY is required" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

compose_cmd=(
  docker compose
  -f docker-compose.yml
  -f docker-compose.phoenix.yml
)

kb_id=""
doc_id=""

log() {
  printf '[smoke] %s\n' "$*"
}

cleanup() {
  if [[ "$SMOKE_KEEP_RESOURCES" == "1" ]]; then
    return
  fi

  local auth_header
  auth_header="Authorization: Bearer ${ADMIN_API_KEY}"

  if [[ -n "$doc_id" ]]; then
    curl -fsS \
      -H "$auth_header" \
      -X DELETE \
      "${APP_BASE_URL}/api/documents/${doc_id}" >/dev/null 2>&1 || true
  fi

  if [[ -n "$kb_id" ]]; then
    curl -fsS \
      -H "$auth_header" \
      -X DELETE \
      "${APP_BASE_URL}/api/knowledge-bases/${kb_id}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

wait_for_http() {
  local url="$1"
  local name="$2"
  local attempts="${3:-60}"
  local sleep_secs="${4:-2}"

  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$sleep_secs"
  done

  echo "timed out waiting for ${name} at ${url}" >&2
  exit 1
}

find_trace_id() {
  local question="$1"
  local project
  project="$(jq -rn --arg value "$PHOENIX_PROJECT_NAME" '$value|@uri')"

  for ((i = 1; i <= 30; i++)); do
    local response trace
    response="$(
      curl -fsS \
        -H "Authorization: Bearer ${ADMIN_API_KEY}" \
        "${APP_BASE_URL}/api/debug/phoenix/spans?project=${project}&limit=${PHOENIX_SPAN_LIMIT}&phoenix_base=$(jq -rn --arg value "$PHOENIX_BASE_URL" '$value|@uri')&phoenix_api_key=$(jq -rn --arg value "$PHOENIX_API_KEY" '$value|@uri')"
    )"

    trace="$(
      jq -r --arg question "$question" '
        [
          .data[]
          | select(.name == "service.chat.completion")
          | select((.attributes["rag.question"] // "") == $question)
          | {
              trace_id: .context.trace_id,
              start_time: .start_time
            }
        ]
        | sort_by(.start_time)
        | last
        | .trace_id // empty
      ' <<<"$response"
    )"

    if [[ -n "$trace" ]]; then
      printf '%s\n' "$trace"
      return 0
    fi

    sleep 2
  done

  return 1
}

if [[ "$SMOKE_AUTO_START_STACK" == "1" ]]; then
  log "starting docker compose stack"
  OPENAI_API_KEY="$OPENAI_API_KEY" \
  OPENAI_BASE_URL="$OPENAI_BASE_URL" \
  OPENAI_CHAT_MODEL="$OPENAI_CHAT_MODEL" \
  ADMIN_API_KEY="$ADMIN_API_KEY" \
  EMBEDDING_BASE_URL="$EMBEDDING_BASE_URL" \
  EMBEDDING_API_KEY="$EMBEDDING_API_KEY" \
  EMBEDDING_MODEL="$EMBEDDING_MODEL" \
  PHOENIX_BASE_URL="$PHOENIX_BASE_URL" \
  PHOENIX_PROJECT_NAME="$PHOENIX_PROJECT_NAME" \
  PHOENIX_API_KEY="$PHOENIX_API_KEY" \
  "${compose_cmd[@]}" up -d --build
fi

log "waiting for app"
wait_for_http "${APP_BASE_URL}/healthz" "app"

log "waiting for phoenix"
wait_for_http "${PHOENIX_BASE_URL}" "phoenix"

token="smoke-ok-${SMOKE_RUN_ID}"
kb_name="smoke-kb-${SMOKE_RUN_ID}"
doc_title="smoke-${SMOKE_RUN_ID}.txt"
question="这次 smoke test 的唯一口令是什么？只返回口令。run_id=${SMOKE_RUN_ID}"
content="这是 go-rag 的本地 smoke test 文档。run_id=${SMOKE_RUN_ID}。唯一口令是 ${token}。如果用户问口令，只返回 ${token}。"

auth_header="Authorization: Bearer ${ADMIN_API_KEY}"

log "creating knowledge base ${kb_name}"
kb_response="$(
  curl -fsS \
    -H "$auth_header" \
    -H 'Content-Type: application/json' \
    -X POST \
    -d "$(jq -nc --arg name "$kb_name" --arg description "phoenix smoke test ${SMOKE_RUN_ID}" '{name: $name, description: $description}')" \
    "${APP_BASE_URL}/api/knowledge-bases"
)"
kb_id="$(jq -er '.id' <<<"$kb_response")"

log "importing text document"
doc_response="$(
  curl -fsS \
    -H "$auth_header" \
    -H 'Content-Type: application/json' \
    -X POST \
    -d "$(jq -nc --argjson knowledge_base_id "$kb_id" --arg title "$doc_title" --arg content "$content" '{knowledge_base_id: $knowledge_base_id, title: $title, content: $content}')" \
    "${APP_BASE_URL}/api/documents/import-text"
)"
doc_id="$(jq -er '.id' <<<"$doc_response")"

log "indexing document ${doc_id}"
curl -fsS \
  -H "$auth_header" \
  -X POST \
  "${APP_BASE_URL}/api/documents/${doc_id}/index" >/dev/null

log "asking chat question"
chat_response="$(
  curl -fsS \
    -H 'Content-Type: application/json' \
    -X POST \
    -d "$(jq -nc \
      --arg model "$OPENAI_CHAT_MODEL" \
      --argjson knowledge_base_id "$kb_id" \
      --arg question "$question" \
      '{model: $model, knowledge_base_id: $knowledge_base_id, stream: false, messages: [{role: "user", content: $question}]}' \
    )" \
    "${APP_BASE_URL}/v1/chat/completions"
)"
answer="$(jq -er '.choices[0].message.content' <<<"$chat_response")"

if [[ "$answer" != *"$token"* ]]; then
  echo "smoke answer mismatch: expected token ${token}, got: ${answer}" >&2
  exit 1
fi

log "waiting for phoenix trace"
trace_id="$(find_trace_id "$question")" || {
  echo "failed to locate phoenix trace for smoke question" >&2
  exit 1
}

log "running evalctl run-trace ${trace_id}"
run_trace_output="$(
  MYSQL_DSN="$MYSQL_DSN" \
  OPENAI_API_KEY="$OPENAI_API_KEY" \
  OPENAI_BASE_URL="$OPENAI_BASE_URL" \
  OPENAI_CHAT_MODEL="$OPENAI_CHAT_MODEL" \
  EMBEDDING_BASE_URL="$EMBEDDING_BASE_URL" \
  EMBEDDING_API_KEY="$EMBEDDING_API_KEY" \
  EMBEDDING_MODEL="$EMBEDDING_MODEL" \
  PHOENIX_BASE_URL="$PHOENIX_BASE_URL" \
  PHOENIX_PROJECT_NAME="$PHOENIX_PROJECT_NAME" \
  PHOENIX_API_KEY="$PHOENIX_API_KEY" \
  go run ./cmd/evalctl run-trace "$trace_id"
)"

sample_id="$(jq -er '.sample_id' <<<"$run_trace_output")"

log "smoke test complete"
jq -n \
  --arg run_id "$SMOKE_RUN_ID" \
  --arg knowledge_base_id "$kb_id" \
  --arg document_id "$doc_id" \
  --arg trace_id "$trace_id" \
  --arg sample_id "$sample_id" \
  --arg answer "$answer" \
  --argjson eval "$(jq '.' <<<"$run_trace_output")" \
  '{
    run_id: $run_id,
    knowledge_base_id: ($knowledge_base_id | tonumber),
    document_id: ($document_id | tonumber),
    trace_id: $trace_id,
    sample_id: $sample_id,
    answer: $answer,
    eval: $eval
  }'
