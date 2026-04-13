const $ = (id) => document.getElementById(id);

const ui = {
  apiBase: $("apiBase"),
  phoenixBase: $("phoenixBase"),
  phoenixProject: $("phoenixProject"),
  adminKey: $("adminKey"),
  chatKey: $("chatKey"),
  chatModel: $("chatModel"),
  chatMode: $("chatMode"),
  agentMaxSteps: $("agentMaxSteps"),
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
  qualityOutput: $("qualityOutput"),
  agentTraceOutput: $("agentTraceOutput"),
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

function chatHeaders() {
  const key = (ui.chatKey.value || "").trim();
  if (!key) {
    return {};
  }
  return { Authorization: `Bearer ${key}` };
}

function write(panel, payload) {
  panel.textContent =
    typeof payload === "string" ? payload : JSON.stringify(payload, null, 2);
}

function renderAgentTrace(steps, source = "unknown") {
  if (!Array.isArray(steps) || steps.length === 0) {
    ui.agentTraceOutput.textContent = "No agent steps yet.";
    return;
  }
  const lines = [
    `source: ${source}`,
    "step | retrieved | action   | query",
    "-----+----------+----------+------------------------------",
  ];
  for (const step of steps) {
    const stepNo = String(step?.step ?? "-").padEnd(4, " ");
    const retrieved = String(step?.retrieved_count ?? "-").padEnd(8, " ");
    const action = String(step?.action || "search").padEnd(8, " ");
    const query = String(step?.query || "");
    lines.push(`${stepNo} | ${retrieved} | ${action} | ${query}`);
  }
  ui.agentTraceOutput.textContent = lines.join("\n");
}

function parseAgentStepLine(line) {
  const match = line.match(
    /^\[agent\]\s*step\s+(\d+)\/\d+\s+query=(?:"([^"]*)"|([^\n]+?))\s+retrieved=(\d+)$/i
  );
  if (!match) {
    return null;
  }
  return {
    step: Number.parseInt(match[1], 10),
    query: (match[2] || match[3] || "").trim(),
    retrieved_count: Number.parseInt(match[4], 10),
    action: "search",
  };
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

function tokenSet(text) {
  const normalized = String(text || "")
    .toLowerCase()
    .replace(/[,.。，\n\t]/g, " ");
  const parts = normalized.split(/\s+/).filter(Boolean);
  return new Set(parts);
}

function overlapScore(left, right) {
  const leftTokens = tokenSet(left);
  if (leftTokens.size === 0) {
    return 0;
  }
  const rightTokens = tokenSet(right);
  let matched = 0;
  for (const token of leftTokens) {
    if (rightTokens.has(token)) {
      matched += 1;
    }
  }
  return matched / leftTokens.size;
}

function symmetricTokenOverlapScore(left, right) {
  const leftTokens = tokenSet(left);
  const rightTokens = tokenSet(right);
  if (leftTokens.size === 0 || rightTokens.size === 0) {
    return 0;
  }
  let matched = 0;
  for (const token of leftTokens) {
    if (rightTokens.has(token)) {
      matched += 1;
    }
  }
  return (2 * matched) / (leftTokens.size + rightTokens.size);
}

function asString(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return "";
}

function parseRetrievedChunks(raw) {
  if (Array.isArray(raw)) {
    return raw.map((item) => asString(item).trim()).filter(Boolean);
  }
  const text = asString(raw).trim();
  if (!text) {
    return [];
  }
  if (text.startsWith("[") && text.endsWith("]")) {
    try {
      const parsed = JSON.parse(text);
      if (Array.isArray(parsed)) {
        return parsed.map((item) => asString(item).trim()).filter(Boolean);
      }
    } catch {
      // ignore and continue with plain split
    }
  }
  if (text.includes("\n---\n")) {
    return text
      .split("\n---\n")
      .map((item) => item.trim())
      .filter(Boolean);
  }
  return [text];
}

function scoreQueryQuality(promptSpan, completionSpan) {
  const attrs = promptSpan?.attributes || {};
  const question = asString(completionSpan?.attributes?.["rag.question"]).trim();
  const originalQuery = asString(attrs["rag.query.original"]).trim() || question;
  const rewrittenQuery =
    asString(attrs["rag.query.rewritten"]).trim() || originalQuery;
  const chunks = parseRetrievedChunks(attrs["rag.retrieved_chunks"]);

  const metrics = [];
  if (!originalQuery || !rewrittenQuery) {
    metrics.push({
      metric: "rewrite_fidelity",
      status: "skipped",
      score: 0,
      summary: "original or rewritten query is empty",
    });
  } else {
    const score = symmetricTokenOverlapScore(originalQuery, rewrittenQuery);
    metrics.push({
      metric: "rewrite_fidelity",
      status: "scored",
      score,
      summary: `query token overlap ${score.toFixed(2)}`,
    });
  }

  if (chunks.length === 0) {
    metrics.push({
      metric: "retrieval_precision_at_k",
      status: "skipped",
      score: 0,
      summary: "no retrieved chunks captured",
    });
  } else if (!rewrittenQuery) {
    metrics.push({
      metric: "retrieval_precision_at_k",
      status: "skipped",
      score: 0,
      summary: "query is empty",
    });
  } else {
    const k = Math.min(4, chunks.length);
    let relevant = 0;
    for (let i = 0; i < k; i += 1) {
      if (overlapScore(rewrittenQuery, chunks[i]) >= 0.1) {
        relevant += 1;
      }
    }
    const score = relevant / k;
    metrics.push({
      metric: "retrieval_precision_at_k",
      status: "scored",
      score,
      summary: `${relevant}/${k} chunks above overlap 0.10`,
    });
  }

  const scored = metrics.filter((item) => item.status === "scored");
  const average =
    scored.length === 0
      ? 0
      : scored.reduce((sum, item) => sum + item.score, 0) / scored.length;

  return {
    original_query: originalQuery,
    rewritten_query: rewrittenQuery,
    chunk_count: chunks.length,
    average_score: average,
    metrics,
  };
}

function renderQueryQuality(quality) {
  write(ui.qualityOutput, quality);
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

async function listChunks() {
  requireDoc();
  const body = await requestJSON(`${apiBase()}/api/documents/${state.docId}/chunks`, {
    headers: {
      ...adminHeaders(),
    },
  });
  write(ui.jsonOutput, body);
  setTab("json");
  log(`Listed chunks for document #${state.docId}`);
}

async function chat(stream) {
  requireKb();
  const question = ui.chatQuestion.value.trim();
  if (!question) {
    throw new Error("question is required");
  }
  const mode = (ui.chatMode.value || "rag").trim();
  const maxSteps = Number.parseInt(ui.agentMaxSteps.value, 10);
  const payload = {
    model: ui.chatModel.value.trim() || "deepseek-chat",
    knowledge_base_id: state.kbId,
    mode,
    max_steps: Number.isFinite(maxSteps) ? maxSteps : 3,
    temperature: 0.2,
    stream,
    messages: [{ role: "user", content: question }],
  };

  if (!stream) {
    const body = await requestJSON(`${apiBase()}/v1/chat/completions`, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...chatHeaders() },
      body: JSON.stringify(payload),
    });
    write(ui.jsonOutput, body);
    setTab("json");
    const metadataSteps = body?.metadata?.agent?.steps;
    if (Array.isArray(metadataSteps) && metadataSteps.length) {
      renderAgentTrace(metadataSteps, "metadata");
      log(`Agent steps: ${metadataSteps.length}`);
    } else {
      renderAgentTrace([], "metadata");
    }
    log("Completed non-stream chat");
    return;
  }

  ui.streamOutput.textContent = "";
  const response = await fetch(`${apiBase()}/v1/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...chatHeaders() },
    body: JSON.stringify(payload),
  });
  if (!response.ok || !response.body) {
    throw new Error(`stream request failed: HTTP ${response.status}`);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let finalText = "";
  const stepLogs = [];
  const stepRows = [];

  function renderStreamPanel() {
    const header = stepLogs.length
      ? `# Agent Steps\n${stepLogs.join("\n")}\n\n`
      : "";
    ui.streamOutput.textContent = `${header}# Assistant\n${finalText}`;
    ui.streamOutput.scrollTop = ui.streamOutput.scrollHeight;
  }

  function consumeSSELine(line) {
    if (!line.startsWith("data:")) {
      return;
    }
    const payloadText = line.slice(5).trim();
    if (!payloadText || payloadText === "[DONE]") {
      return;
    }
    let obj;
    try {
      obj = JSON.parse(payloadText);
    } catch {
      return;
    }
    const delta = obj?.choices?.[0]?.delta?.content || "";
    if (!delta) {
      return;
    }
    if (delta.startsWith("[agent] step")) {
      stepLogs.push(delta.trim());
      const row = parseAgentStepLine(delta.trim());
      if (row) {
        stepRows.push(row);
      }
    } else {
      finalText += delta;
    }
  }

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let idx = buffer.indexOf("\n");
    while (idx >= 0) {
      const line = buffer.slice(0, idx).trimEnd();
      buffer = buffer.slice(idx + 1);
      consumeSSELine(line);
      idx = buffer.indexOf("\n");
    }
    renderStreamPanel();
  }
  if (buffer) {
    consumeSSELine(buffer.trim());
    renderStreamPanel();
  }
  setTab("stream");
  if (stepLogs.length) {
    renderAgentTrace(stepRows, "stream");
    log(`Stream agent steps: ${stepLogs.length}`);
  } else {
    renderAgentTrace([], "stream");
  }
  log("Completed stream chat");
}

async function getLatestTrace() {
  const project = ui.phoenixProject.value.trim() || "go-rag";
  const params = new URLSearchParams({
    project,
    limit: "300",
    phoenix_base: phoenixBase(),
  });
  const body = await requestJSON(
    `${apiBase()}/api/debug/phoenix/spans?${params.toString()}`,
    {
      headers: {
        ...adminHeaders(),
      },
    }
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
  const params = new URLSearchParams({
    project,
    limit: "1000",
    phoenix_base: phoenixBase(),
  });
  const body = await requestJSON(
    `${apiBase()}/api/debug/phoenix/spans?${params.toString()}`,
    {
      headers: {
        ...adminHeaders(),
      },
    }
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
  const queryQuality = scoreQueryQuality(promptSpan, completionSpan);

  const details = {
    trace_id: state.traceId,
    span_count: spans.length,
    question: completionSpan?.attributes?.["rag.question"],
    answer: completionSpan?.attributes?.["rag.answer"],
    collection_name: completionSpan?.attributes?.["rag.collection_name"],
    prompt: promptSpan?.attributes?.["rag.prompt"],
    prompt_messages_json: promptSpan?.attributes?.["rag.prompt_messages_json"],
    retrieved_chunks: promptSpan?.attributes?.["rag.retrieved_chunks"],
    query_quality: queryQuality,
    spans,
  };
  renderQueryQuality(queryQuality);
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
  renderQueryQuality({ message: "Load trace detail first." });
  renderAgentTrace([], "init");
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
  $("listChunksBtn").addEventListener("click", () => runAction(listChunks));
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
