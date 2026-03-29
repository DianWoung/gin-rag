const $ = (id) => document.getElementById(id);

const ui = {
  apiBase: $("apiBase"),
  phoenixBase: $("phoenixBase"),
  phoenixProject: $("phoenixProject"),
  adminKey: $("adminKey"),
  chatModel: $("chatModel"),
  mysqlDsn: $("mysqlDsn"),
  kbName: $("kbName"),
  textTitle: $("textTitle"),
  textContent: $("textContent"),
  pdfFile: $("pdfFile"),
  chatQuestion: $("chatQuestion"),
  kbId: $("kbId"),
  docId: $("docId"),
  traceId: $("traceId"),
  evalCmd: $("evalCmd"),
  jsonOutput: $("jsonOutput"),
  streamOutput: $("streamOutput"),
  traceOutput: $("traceOutput"),
  logOutput: $("logOutput"),
};

const state = {
  kbId: null,
  docId: null,
  traceId: null,
};

function nowIso() {
  return new Date().toISOString().replace("T", " ").slice(0, 19);
}

function trimSlash(v) {
  return (v || "").trim().replace(/\/+$/, "");
}

function apiBase() {
  return trimSlash(ui.apiBase.value);
}

function phoenixBase() {
  return trimSlash(ui.phoenixBase.value);
}

function adminHeaders() {
  const key = (ui.adminKey.value || "").trim();
  if (!key) {
    return {};
  }
  return { Authorization: `Bearer ${key}` };
}

function write(panel, payload) {
  panel.textContent =
    typeof payload === "string" ? payload : JSON.stringify(payload, null, 2);
}

function log(message) {
  ui.logOutput.textContent += `[${nowIso()}] ${message}\n`;
  ui.logOutput.scrollTop = ui.logOutput.scrollHeight;
}

function syncIds() {
  ui.kbId.textContent = state.kbId ?? "-";
  ui.docId.textContent = state.docId ?? "-";
  ui.traceId.textContent = state.traceId ?? "-";
  ui.evalCmd.value = buildEvalCommand();
}

function setTab(name) {
  document.querySelectorAll(".tab").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.tab === name);
  });
  document.querySelectorAll(".output").forEach((panel) => {
    panel.classList.toggle("active", panel.id === `${name}Output`);
  });
}

async function requestJSON(url, options = {}) {
  const response = await fetch(url, options);
  const text = await response.text();
  let body;
  try {
    body = text ? JSON.parse(text) : {};
  } catch {
    body = { raw: text };
  }
  if (!response.ok) {
    throw new Error(
      `HTTP ${response.status}: ${body?.error?.message || body?.message || text}`
    );
  }
  return body;
}

function requireKb() {
  if (!state.kbId) {
    throw new Error("knowledge base id is empty, create one first");
  }
}

function requireDoc() {
  if (!state.docId) {
    throw new Error("document id is empty, import one first");
  }
}

function requireTrace() {
  if (!state.traceId) {
    throw new Error("trace id is empty, fetch latest trace first");
  }
}

function buildEvalCommand() {
  const traceId = state.traceId || "<TRACE_ID>";
  return `MYSQL_DSN='${ui.mysqlDsn.value}' PHOENIX_BASE_URL='${phoenixBase()}' PHOENIX_PROJECT_NAME='${ui.phoenixProject.value.trim() || "go-rag"}' OPENAI_API_KEY='<OPENAI_API_KEY>' OPENAI_BASE_URL='<OPENAI_BASE_URL>' OPENAI_CHAT_MODEL='${ui.chatModel.value.trim() || "deepseek-chat"}' go run ./cmd/evalctl run-trace ${traceId}`;
}

async function createKb() {
  const name = ui.kbName.value.trim();
  if (!name) {
    throw new Error("knowledge base name is required");
  }
  const body = await requestJSON(`${apiBase()}/api/knowledge-bases`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...adminHeaders(),
    },
    body: JSON.stringify({
      name,
      description: "created from web debug console",
    }),
  });
  state.kbId = body.id;
  syncIds();
  write(ui.jsonOutput, body);
  setTab("json");
  log(`Created knowledge base #${body.id}`);
}

async function importText() {
  requireKb();
  const payload = {
    knowledge_base_id: state.kbId,
    title: ui.textTitle.value.trim() || "debug.txt",
    content: ui.textContent.value,
  };
  const body = await requestJSON(`${apiBase()}/api/documents/import-text`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...adminHeaders(),
    },
    body: JSON.stringify(payload),
  });
  state.docId = body.id;
  syncIds();
  write(ui.jsonOutput, body);
  setTab("json");
  log(`Imported text document #${body.id}`);
}

async function importPdf() {
  requireKb();
  const file = ui.pdfFile.files?.[0];
  if (!file) {
    throw new Error("select a PDF file first");
  }
  const form = new FormData();
  form.append("knowledge_base_id", String(state.kbId));
  form.append("title", file.name);
  form.append("file", file, file.name);

  const body = await requestJSON(`${apiBase()}/api/documents/import-pdf`, {
    method: "POST",
    headers: {
      ...adminHeaders(),
    },
    body: form,
  });
  state.docId = body.id;
  syncIds();
  write(ui.jsonOutput, body);
  setTab("json");
  log(`Imported PDF document #${body.id}`);
}

async function indexDoc() {
  requireDoc();
  const body = await requestJSON(
    `${apiBase()}/api/documents/${state.docId}/index`,
    {
      method: "POST",
      headers: {
        ...adminHeaders(),
      },
    }
  );
  write(ui.jsonOutput, body);
  setTab("json");
  log(`Indexed document #${state.docId}`);
}

async function listDocs() {
  requireKb();
  const body = await requestJSON(
    `${apiBase()}/api/documents?knowledge_base_id=${state.kbId}`,
    {
      headers: {
        ...adminHeaders(),
      },
    }
  );
  write(ui.jsonOutput, body);
  setTab("json");
  log(`Listed documents for KB #${state.kbId}`);
}

async function chat(stream) {
  requireKb();
  const question = ui.chatQuestion.value.trim();
  if (!question) {
    throw new Error("question is required");
  }
  const payload = {
    model: ui.chatModel.value.trim() || "deepseek-chat",
    knowledge_base_id: state.kbId,
    temperature: 0.2,
    stream,
    messages: [{ role: "user", content: question }],
  };

  if (!stream) {
    const body = await requestJSON(`${apiBase()}/v1/chat/completions`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    write(ui.jsonOutput, body);
    setTab("json");
    log("Completed non-stream chat");
    return;
  }

  ui.streamOutput.textContent = "";
  const response = await fetch(`${apiBase()}/v1/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!response.ok || !response.body) {
    throw new Error(`stream request failed: HTTP ${response.status}`);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    ui.streamOutput.textContent += decoder.decode(value, { stream: true });
    ui.streamOutput.scrollTop = ui.streamOutput.scrollHeight;
  }
  setTab("stream");
  log("Completed stream chat");
}

async function getLatestTrace() {
  const project = ui.phoenixProject.value.trim() || "go-rag";
  const body = await requestJSON(
    `${phoenixBase()}/v1/projects/${encodeURIComponent(project)}/spans?limit=300`
  );
  const chatRoots = (body.data || [])
    .filter((span) => span.name === "http.v1.chat_completions")
    .sort(
      (a, b) =>
        new Date(b.start_time || 0).getTime() - new Date(a.start_time || 0).getTime()
    );
  if (chatRoots.length === 0) {
    throw new Error("no chat traces found in phoenix project");
  }
  state.traceId = chatRoots[0]?.context?.trace_id || null;
  syncIds();
  write(ui.traceOutput, {
    latest_chat_trace: state.traceId,
    span_id: chatRoots[0]?.context?.span_id,
    start_time: chatRoots[0]?.start_time,
  });
  setTab("trace");
  log(`Captured latest trace: ${state.traceId}`);
}

async function loadTraceDetail() {
  requireTrace();
  const project = ui.phoenixProject.value.trim() || "go-rag";
  const body = await requestJSON(
    `${phoenixBase()}/v1/projects/${encodeURIComponent(project)}/spans?limit=1000`
  );
  const spans = (body.data || []).filter(
    (span) => span?.context?.trace_id === state.traceId
  );
  if (spans.length === 0) {
    throw new Error(`trace ${state.traceId} not found in project ${project}`);
  }

  const promptSpan = spans.find((span) => span.name === "service.chat.rag_prompt");
  const completionSpan =
    spans.find((span) => span.name === "service.chat.completion") ||
    spans.find((span) => span.name === "service.chat.completion_stream");

  const details = {
    trace_id: state.traceId,
    span_count: spans.length,
    question: completionSpan?.attributes?.["rag.question"],
    answer: completionSpan?.attributes?.["rag.answer"],
    collection_name: completionSpan?.attributes?.["rag.collection_name"],
    prompt: promptSpan?.attributes?.["rag.prompt"],
    prompt_messages_json: promptSpan?.attributes?.["rag.prompt_messages_json"],
    retrieved_chunks: promptSpan?.attributes?.["rag.retrieved_chunks"],
    spans,
  };
  write(ui.traceOutput, details);
  setTab("trace");
  log(`Loaded trace detail for ${state.traceId}`);
}

async function runAction(fn) {
  try {
    await fn();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    write(ui.jsonOutput, { error: message });
    setTab("json");
    log(`Error: ${message}`);
  }
}

function initDefaults() {
  const host = window.location.hostname || "127.0.0.1";
  ui.apiBase.value = `${window.location.protocol}//${window.location.host}`;
  ui.phoenixBase.value = `${window.location.protocol}//${host}:6006`;
  syncIds();
  write(ui.logOutput, "");
  log("Console ready");
}

function bindEvents() {
  $("createKbBtn").addEventListener("click", () => runAction(createKb));
  $("importTextBtn").addEventListener("click", () => runAction(importText));
  $("importPdfBtn").addEventListener("click", () => runAction(importPdf));
  $("indexDocBtn").addEventListener("click", () => runAction(indexDoc));
  $("listDocsBtn").addEventListener("click", () => runAction(listDocs));
  $("chatBtn").addEventListener("click", () => runAction(() => chat(false)));
  $("chatStreamBtn").addEventListener("click", () => runAction(() => chat(true)));
  $("latestTraceBtn").addEventListener("click", () => runAction(getLatestTrace));
  $("traceDetailBtn").addEventListener("click", () => runAction(loadTraceDetail));
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => setTab(tab.dataset.tab));
  });
}

initDefaults();
bindEvents();
