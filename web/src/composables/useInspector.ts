import { onBeforeUnmount, onMounted, ref } from "vue";
import { apiCancel, apiChat, apiMetrics, apiSession } from "../api";
import type { ChatMessage, ContextSnapshot, MetricsInfo, RuntimeEvent, SessionInfo } from "../types";

function msgId(): string {
  return Math.random().toString(36).slice(2, 10);
}

function eventClass(type: string): string {
  if (type.startsWith("tool.batch")) return "batch";
  if (type.startsWith("tool.")) return "tool";
  if (type.startsWith("llm.stream")) return "stream";
  if (type.startsWith("llm.")) return "llm";
  if (type.includes("failed") || type.includes("denied") || type.includes("loop")) return "err";
  return "";
}

function shortType(type: string): string {
  return type
    .replace(/^tool\./, "T.")
    .replace(/^llm\.stream_/, "L.stream.")
    .replace(/^llm\./, "L.")
    .replace(/^context\./, "C.")
    .replace(/^agent\./, "A.")
    .replace(/^permission\./, "P.");
}

function compact(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") {
    try {
      return JSON.stringify(JSON.parse(v));
    } catch {
      return v.slice(0, 80);
    }
  }
  return JSON.stringify(v);
}

function previewData(data?: Record<string, unknown>): string {
  if (!data) return "";
  if (data.text != null && data.total_len != null) {
    return String(data.text).slice(0, 40);
  }
  if (data.tool) {
    const args = data.arguments ? " " + compact(data.arguments) : "";
    return String(data.tool) + args;
  }
  if (data.summary) return String(data.summary);
  if (data.provider && data.model) return `${data.provider} ${data.model}`;
  if (data.total_tokens != null) return `tokens≈${data.total_tokens}`;
  if (data.to) return `→ ${data.to}`;
  if (data.error) return String(data.error);
  return "";
}

function fmtTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return (
    d.toLocaleTimeString(undefined, { hour12: false }) +
    "." +
    String(d.getMilliseconds()).padStart(3, "0")
  );
}

export function useInspector() {
  const messages = ref<ChatMessage[]>([]);
  const events = ref<RuntimeEvent[]>([]);
  const session = ref<SessionInfo | null>(null);
  const metrics = ref<MetricsInfo>({});
  const snapshot = ref<ContextSnapshot | null>(null);
  const input = ref("");
  const connected = ref(false);

  const lastErrorShown = ref("");
  let es: EventSource | null = null;
  let pollTimer: number | null = null;
  let refreshTimer: number | null = null;
  let streamMsgId: string | null = null;
  let streamFlush: number | null = null;
  let streamPending = "";

  function addMessage(role: ChatMessage["role"], text: string) {
    messages.value.push({ id: msgId(), role, text });
  }

  function addSystemOnce(text: string) {
    if (lastErrorShown.value === text) return;
    if (text.startsWith("error:") || text.startsWith("cancelled") || text.startsWith("cancel ")) {
      lastErrorShown.value = text;
    }
    addMessage("system", text);
  }

  function clearPollTimer() {
    if (pollTimer != null) {
      window.clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function appendAssistantOnce(final: string) {
    if (!final) return;
    const last = [...messages.value].reverse().find((m) => m.role === "assistant");
    if (last) {
      const lastText = last.text.trim();
      const fin = final.trim();
      if (lastText === fin) return;
      if (fin.startsWith(lastText.slice(0, Math.min(40, lastText.length))) && fin.length >= lastText.length) {
        last.text = fin;
        return;
      }
      if (lastText.includes(fin.slice(0, Math.min(40, fin.length)))) return;
    }
    addMessage("assistant", final);
  }

  function flushStreamDelta() {
    if (!streamPending) return;
    const piece = streamPending;
    streamPending = "";
    if (!streamMsgId) {
      const id = msgId();
      streamMsgId = id;
      messages.value.push({ id, role: "assistant", text: piece });
    } else {
      const m = messages.value.find((x) => x.id === streamMsgId);
      if (m) m.text += piece;
    }
  }

  function scheduleStreamFlush() {
    if (streamFlush != null) return;
    streamFlush = window.setTimeout(() => {
      streamFlush = null;
      flushStreamDelta();
    }, 40);
  }

  function applySession(s: SessionInfo) {
    session.value = s;
    if (s.snapshot) snapshot.value = s.snapshot;
    if (s.turn?.final) appendAssistantOnce(s.turn.final);
    if (s.last_error) addSystemOnce("error: " + s.last_error);
    if (!s.running) clearPollTimer();
  }

  function pushEvent(evt: RuntimeEvent) {
    events.value.push(evt);
    if (events.value.length > 300) events.value.splice(0, events.value.length - 300);

    if (evt.type === "llm.stream_delta") {
      const t = evt.data?.text;
      if (typeof t === "string" && t) {
        streamPending += t;
        scheduleStreamFlush();
      }
    }
    if (evt.type === "llm.request_finished" || evt.type === "llm.request_failed") {
      flushStreamDelta();
      streamMsgId = null;
    }

    if (evt.type === "agent.state_changed") {
      const to = String(evt.data?.to || "");
      if (to === "CANCELLED") {
        flushStreamDelta();
        addSystemOnce("cancelled");
        clearPollTimer();
      }
      if (to === "FINISHED" || to === "FAILED") {
        flushStreamDelta();
        clearPollTimer();
      }
    }
    if (evt.type === "llm.request_finished" || evt.type === "context.built") {
      void refreshSession();
      void refreshMetrics();
    }
    if (evt.type === "tool.batch_started") {
      const tools = evt.data?.tools;
      const size = evt.data?.size;
      if (Array.isArray(tools)) {
        addSystemOnce(`parallel batch (${size}): ${tools.join(", ")}`);
      }
    }
  }

  async function refreshSession() {
    try {
      applySession(await apiSession());
    } catch {
      /* server may be down */
    }
  }

  async function refreshMetrics() {
    try {
      metrics.value = await apiMetrics();
    } catch {
      /* ignore */
    }
  }

  function connectSSE() {
    if (es) es.close();
    es = new EventSource("/api/events");
    es.addEventListener("hello", () => {
      connected.value = true;
      addSystemOnce("SSE connected");
    });
    es.addEventListener("runtime", (ev) => {
      try {
        pushEvent(JSON.parse((ev as MessageEvent).data) as RuntimeEvent);
      } catch {
        /* skip malformed */
      }
    });
    es.onerror = () => {
      connected.value = false;
    };
  }

  async function send() {
    const message = input.value.trim();
    if (!message) return;
    lastErrorShown.value = "";
    input.value = "";
    addMessage("user", message);
    await apiChat(message);
    session.value = {
      ...(session.value || {
        session_id: "",
        workspace: "",
        provider: "",
        model: "",
        state: "BUILDING_CONTEXT",
        running: true,
      }),
      state: "BUILDING_CONTEXT",
      running: true,
    };
    clearPollTimer();
    pollTimer = window.setInterval(() => {
      void refreshSession();
    }, 400);
  }

  async function cancel() {
    try {
      const r = await apiCancel();
      lastErrorShown.value = "";
      addSystemOnce(r.cancelled ? "cancel requested" : "nothing to cancel");
    } catch (err) {
      addSystemOnce(String((err as Error).message || err));
    }
  }

  function boot() {
    connectSSE();
    void refreshSession().catch(() => addMessage("system", "无法连接 API — 请先运行 mincode web"));
    void refreshMetrics();
    refreshTimer = window.setInterval(() => {
      void refreshSession();
      void refreshMetrics();
    }, 3000);
  }

  onMounted(boot);
  onBeforeUnmount(() => {
    es?.close();
    clearPollTimer();
    if (refreshTimer != null) window.clearInterval(refreshTimer);
    if (streamFlush != null) window.clearTimeout(streamFlush);
  });

  return {
    messages,
    events,
    session,
    metrics,
    snapshot,
    input,
    connected,
    send,
    cancel,
    eventClass,
    shortType,
    previewData,
    fmtTime,
  };
}
