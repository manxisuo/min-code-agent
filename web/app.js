/* MinCode Inspector — local web UI */
const $ = (id) => document.getElementById(id);

const state = {
  es: null,
  events: [],
  lastErrorShown: "",
  pollTimer: null,
  finalShown: false,
};

function fmtTime(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return d.toLocaleTimeString(undefined, { hour12: false }) +
    "." + String(d.getMilliseconds()).padStart(3, "0");
}

function esc(s) {
  return String(s ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function addMessage(role, text) {
  const box = $("messages");
  const el = document.createElement("div");
  el.className = "msg " + role;
  el.innerHTML = `<div class="role">${esc(role)}</div><div class="body">${esc(text)}</div>`;
  box.appendChild(el);
  box.scrollTop = box.scrollHeight;
  return el;
}

function previewData(data) {
  if (!data || typeof data !== "object") return "";
  if (data.tool) {
    const args = data.arguments ? compact(data.arguments) : "";
    return data.tool + (args ? " " + args : "");
  }
  if (data.summary) return data.summary;
  if (data.provider && data.model) return `${data.provider} ${data.model}`;
  if (data.total_tokens != null) return `tokens≈${data.total_tokens}`;
  if (data.error) return String(data.error);
  return "";
}

function compact(jsonStr) {
  try {
    const o = JSON.parse(jsonStr);
    return JSON.stringify(o);
  } catch {
    return String(jsonStr).slice(0, 80);
  }
}

function eventClass(type) {
  if (type.startsWith("tool.batch")) return "batch";
  if (type.startsWith("tool.")) return "tool";
  if (type.startsWith("llm.")) return "llm";
  if (type.includes("failed") || type.includes("denied") || type.includes("loop")) return "err";
  return "";
}

function shortType(type) {
  return type.replace(/^tool\./, "T.").replace(/^llm\./, "L.")
    .replace(/^context\./, "C.").replace(/^agent\./, "A.")
    .replace(/^permission\./, "P.");
}

function pushTimeline(evt) {
  state.events.push(evt);
  if (state.events.length > 300) state.events.shift();
  const box = $("timeline");
  const el = document.createElement("div");
  el.className = "tl-item " + eventClass(evt.type);
  const d = previewData(evt.data);
  el.innerHTML = `<span class="t">${esc(fmtTime(evt.time))}</span>` +
    `<span class="ty">${esc(shortType(evt.type))}</span>` +
    (d ? `<span class="d">${esc(d)}</span>` : "");
  box.appendChild(el);
  box.scrollTop = box.scrollHeight;
  $("tlCount").textContent = `${state.events.length} events`;
}

function setState(stateName) {
  const pill = $("statePill");
  pill.textContent = stateName || "IDLE";
  pill.dataset.state = stateName || "IDLE";
  const running = ["BUILDING_CONTEXT", "CALLING_LLM", "PROCESSING_RESPONSE", "EXECUTING_TOOL"].includes(stateName);
  $("btnCancel").disabled = !running;
  $("btnSend").disabled = running;
}

function renderMetrics(m) {
  $("mLlm").textContent = m.llm_calls ?? 0;
  $("mTokens").textContent = m.total_tokens ?? 0;
  $("mTime").textContent = `${m.llm_duration_ms ?? 0}ms`;
  $("mPar").textContent = m.parallel_batches ?? 0;
  $("mErr").textContent = m.errors ?? 0;
}

function renderContext(snap) {
  const box = $("contextBox");
  if (!snap || !Array.isArray(snap.items) || snap.items.length === 0) {
    box.innerHTML = `<div class="empty">尚无快照</div>`;
    return;
  }
  const items = snap.items;
  const maxTok = Math.max(1, ...items.map((i) => i.token_count || 0));
  const rows = items.map((i) => {
    const tok = i.token_count || 0;
    const pct = Math.max(2, Math.round((tok / maxTok) * 100));
    const cls = i.excluded ? "excluded" : "";
    return `<div class="ctx-row ${cls}">
      <div class="src">${esc(i.source)}</div>
      <div class="ctx-bar"><i style="width:${pct}%"></i></div>
      <div class="tok">${tok}</div>
    </div>`;
  }).join("");
  const total = (snap.total_tokens || 0) + (snap.tool_tokens || 0);
  box.innerHTML = rows + `<div class="ctx-total">Total <b>${total}</b> / budget ${snap.budget ?? "-"}
    · step ${snap.step ?? "-"} · incl ${snap.included_count ?? 0} · excl ${snap.excluded_count ?? 0}</div>`;
}

async function fetchJSON(url, opts) {
  const res = await fetch(url, opts);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function addSystemOnce(text) {
  if (state.lastErrorShown === text) return;
  if (text.startsWith("error:") || text.startsWith("cancelled") || text.startsWith("cancel ")) {
    state.lastErrorShown = text;
  }
  addMessage("system", text);
}

function clearPollTimer() {
  if (state.pollTimer) {
    clearInterval(state.pollTimer);
    state.pollTimer = null;
  }
}

function handleSessionPayload(s) {
  $("sessionMeta").textContent =
    `${s.session_id} · ${s.provider}/${s.model} · ${s.workspace}`;
  setState(s.state);
  if (s.snapshot) renderContext(s.snapshot);

  if (s.turn && s.turn.final) {
    const box = $("messages");
    const exists = [...box.querySelectorAll(".msg.assistant")]
      .some((el) => el.textContent.includes(s.turn.final.slice(0, 40)));
    if (!exists) {
      addMessage("assistant", s.turn.final);
    }
  }
  if (s.last_error) addSystemOnce("error: " + s.last_error);
  if (!s.running) clearPollTimer();
}

async function refreshSession() {
  const s = await fetchJSON("/api/session");
  handleSessionPayload(s);
}

async function refreshMetrics() {
  const m = await fetchJSON("/api/metrics");
  renderMetrics(m);
}

function connectSSE() {
  if (state.es) state.es.close();
  const es = new EventSource("/api/events");
  state.es = es;

  es.addEventListener("hello", () => {
    addMessage("system", "SSE connected");
  });

  es.addEventListener("runtime", (ev) => {
    let evt;
    try {
      evt = JSON.parse(ev.data);
    } catch {
      return;
    }
    pushTimeline(evt);

    if (evt.type === "llm.request_finished") {
      refreshMetrics().catch(() => {});
      refreshSession().catch(() => {});
    }
    if (evt.type === "context.built") {
      refreshSession().catch(() => {});
    }
    if (evt.type === "tool.batch_started" && evt.data && Array.isArray(evt.data.tools)) {
      addSystemOnce(`parallel batch (${evt.data.size}): ${evt.data.tools.join(", ")}`);
    }
    if (evt.type === "agent.state_changed") {
      const to = evt.data && evt.data.to;
      setState(to);
      if (to === "CANCELLED") {
        addSystemOnce("cancelled");
        clearPollTimer();
      }
      if (to === "FINISHED" || to === "FAILED") {
        clearPollTimer();
      }
    }
  });

  es.onerror = () => {
    // Browser will retry EventSource automatically.
  };
}

async function sendChat(message) {
  // New turn: allow the next error/cancel line to show once.
  state.lastErrorShown = "";
  addMessage("user", message);
  await fetchJSON("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message }),
  });
  setState("BUILDING_CONTEXT");
  clearPollTimer();
  const started = Date.now();
  state.pollTimer = setInterval(async () => {
    try {
      const s = await fetchJSON("/api/session");
      handleSessionPayload(s);
      if (!s.running) clearPollTimer();
      if (!s.running && Date.now() - started > 10 * 60 * 1000) clearPollTimer();
    } catch {
      /* keep polling */
    }
  }, 400);
}

function boot() {
  $("chatForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const ta = $("input");
    const message = ta.value.trim();
    if (!message) return;
    ta.value = "";
    try {
      await sendChat(message);
    } catch (err) {
      addMessage("system", String(err.message || err));
    }
  });

  $("btnCancel").addEventListener("click", async () => {
    try {
      const r = await fetchJSON("/api/cancel", { method: "POST" });
      state.lastErrorShown = "";
      addSystemOnce(r.cancelled ? "cancel requested" : "nothing to cancel");
    } catch (err) {
      addSystemOnce(String(err.message || err));
    }
  });

  connectSSE();
  refreshSession().catch(() => addMessage("system", "无法连接 API — 请先运行 mincode web"));
  refreshMetrics().catch(() => {});
  setInterval(() => {
    refreshSession().catch(() => {});
    refreshMetrics().catch(() => {});
  }, 3000);
}

boot();
