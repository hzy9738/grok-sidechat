/**
 * 安能助手侧栏 UI → native host → OpenAI-compatible Chat Completions.
 * 主路径：干净对话；设置 / 调试噪音默认收起。
 */
import { renderMarkdown, bindCopyButtons } from "./lib/markdown.js";
import { packBrowserContext, stripMentionTokens, localPathHintFromURL } from "./lib/context.js";
import { titleFromUserText } from "./lib/threads.js";
import { mapHostEventToUI } from "./lib/events.js";
import {
  PAGE_SCOPE_STORE_KEY,
  DEFAULT_THREAD_TITLE,
  derivePageScope,
  emptyScopeRecord,
  filterSessionsForScope,
  readScopeRecord,
  updateScopeRecord,
} from "./lib/page-scope.js";

const $ = (id) => document.getElementById(id);

const state = {
  sessionId: "",
  scopeKey: "",
  scopeRecord: emptyScopeRecord(),
  scopeEpoch: 0,
  windowId: null,
  drafts: new Map(),
  busy: false,
  requestId: "",
  lastPage: null,
  chips: /** @type {any[]} */ ([]),
  assistantEl: null,
  assistantRaw: "",
  thinkingEl: null,
  thinkingDetails: null,
  /** @type {Array<{id:string,title?:string,summary?:string,cwd?:string,updatedAt?:string,numMessages?:number}>} */
  sessions: [],
  sessionQuery: "",
  sessionLoading: false,
  /** 本 turn 是否已通过 live 事件渲染过 thinking/assistant（用于 send_done 补渲染） */
  liveThinkingChars: 0,
  liveAssistantChars: 0,
  /** @type {chrome.runtime.Port|null} */
  port: null,
  mediaRecorder: null,
  mediaStream: null,
  recordingChunks: [],
};

function hideEmpty() {
  const el = $("empty-state");
  if (el) el.hidden = true;
}

/** 空态用蓝白字标，不用官网按钮 Logo。 */
function emptyStateHTML() {
  return `<div class="empty-mark" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="8" fill="#2161D7"/><path d="M8 12.5A2.5 2.5 0 0 1 10.5 10h11A2.5 2.5 0 0 1 24 12.5v7a2.5 2.5 0 0 1-2.5 2.5H16l-3.2 2.4a.8.8 0 0 1-1.3-.6V22H10.5A2.5 2.5 0 0 1 8 19.5v-7Z" fill="#fff"/><circle cx="24.5" cy="8.5" r="3.2" fill="#61C7F2"/></svg></div><p>你好，我是安能助手</p><p class="hint">可以提问，也可以录音、识图或读取 PDF</p>`;
}

function setHostStatus(ok, title) {
  const el = $("host-dot");
  el.className = `host-dot ${ok === true ? "ok" : ok === false ? "bad" : "unknown"}`;
  el.title = title || (ok ? "host ok" : "host issue");
}

function setBusy(busy) {
  state.busy = busy;
  $("btn-send").disabled = busy;
  $("btn-send").hidden = busy;
  $("btn-cancel").disabled = !busy;
  $("btn-cancel").hidden = !busy;
}

function scrollLog() {
  const log = $("log");
  log.scrollTop = log.scrollHeight;
}

function autosizeInput() {
  const ta = $("input");
  ta.style.height = "auto";
  ta.style.height = `${Math.min(160, Math.max(24, ta.scrollHeight))}px`;
}

function renderChips() {
  const root = $("context-chips");
  root.innerHTML = "";
  if (!state.chips.length) {
    root.hidden = true;
    return;
  }
  root.hidden = false;
  for (const chip of state.chips) {
    const el = document.createElement("span");
    el.className = "chip";
    let label = `@${chip.kind}`;
    if (chip.kind === "tab") label = chip.title || chip.url || "@tab";
    else if (chip.kind === "selection") label = `选区 ${(chip.text || "").slice(0, 20)}`;
    else if (chip.kind === "page") label = chip.title || "当前页";
    else if (chip.kind === "pageText") label = "正文";
    else if (chip.kind === "image") label = chip.alt || chip.title || "选中图片";
    else if (chip.kind === "document") label = chip.title || "PDF 文档";
    el.appendChild(document.createTextNode(label));
    const x = document.createElement("button");
    x.type = "button";
    x.className = "x";
    x.textContent = "×";
    x.addEventListener("click", () => {
      state.chips = state.chips.filter((c) => c !== chip);
      renderChips();
    });
    el.appendChild(x);
    root.appendChild(el);
  }
}

function appendUserBubble(text) {
  hideEmpty();
  const log = $("log");
  const wrap = document.createElement("div");
  wrap.className = "msg user";
  const bubble = document.createElement("div");
  bubble.className = "bubble-user";
  bubble.textContent = text;
  wrap.appendChild(bubble);
  log.appendChild(wrap);
  scrollLog();
}

function ensureAssistantBubble() {
  if (state.assistantEl) return state.assistantEl;
  hideEmpty();
  const log = $("log");
  const wrap = document.createElement("div");
  wrap.className = "msg assistant";
  const bubble = document.createElement("div");
  bubble.className = "bubble-assistant";
  const md = document.createElement("div");
  md.className = "md";
  bubble.appendChild(md);
  const actions = document.createElement("div");
  actions.className = "msg-actions";
  const copyBtn = document.createElement("button");
  copyBtn.type = "button";
  copyBtn.className = "btn-copy-msg";
  copyBtn.textContent = "复制";
  copyBtn.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(state.assistantRaw || md.innerText || "");
      copyBtn.textContent = "已复制";
      setTimeout(() => (copyBtn.textContent = "复制"), 1000);
    } catch {
      /* ignore */
    }
  });
  actions.appendChild(copyBtn);
  const speakBtn = document.createElement("button");
  speakBtn.type = "button";
  speakBtn.className = "btn-copy-msg";
  speakBtn.textContent = "朗读";
  speakBtn.addEventListener("click", () => {
    const text = (md.innerText || state.assistantRaw || "").trim();
    if (!text || !window.speechSynthesis) return;
    window.speechSynthesis.cancel();
    const utterance = new SpeechSynthesisUtterance(text);
    utterance.lang = "zh-CN";
    window.speechSynthesis.speak(utterance);
  });
  actions.appendChild(speakBtn);
  wrap.appendChild(bubble);
  wrap.appendChild(actions);
  log.appendChild(wrap);
  state.assistantEl = wrap;
  state.assistantRaw = "";
  scrollLog();
  return wrap;
}

function updateAssistantMarkdown(text, append) {
  const el = ensureAssistantBubble();
  const md = el.querySelector(".md");
  if (append) state.assistantRaw += text;
  else state.assistantRaw = text;
  md.innerHTML = renderMarkdown(state.assistantRaw);
  bindCopyButtons(md);
  scrollLog();
}

function appendThinking(text) {
  hideEmpty();
  const log = $("log");
  if (!state.thinkingEl) {
    const details = document.createElement("details");
    details.className = "thinking";
    // 默认展开，方便看到思考过程（可点标题折叠）
    details.open = true;
    const summary = document.createElement("summary");
    summary.textContent = "思考过程";
    const body = document.createElement("div");
    body.className = "thinking-body";
    details.appendChild(summary);
    details.appendChild(body);
    log.appendChild(details);
    state.thinkingEl = body;
    state.thinkingDetails = details;
  }
  state.thinkingEl.textContent = (state.thinkingEl.textContent || "") + text;
  // 流式思考时保持展开并滚到底部
  if (state.thinkingDetails) state.thinkingDetails.open = true;
  state.thinkingEl.scrollTop = state.thinkingEl.scrollHeight;
  scrollLog();
}

function appendError(text) {
  hideEmpty();
  const log = $("log");
  const div = document.createElement("div");
  div.className = "err-banner";
  div.textContent = text;
  log.appendChild(div);
  scrollLog();
}

async function collectPage(tabId) {
  const res = await chrome.runtime.sendMessage({
    type: "get-page-context",
    tabId,
    windowId: state.windowId,
    includeTabs: true,
  });
  if (!res?.ok) {
    // 不弹大红条打断闲聊；仅当用户明确 @ 时再提示
    return null;
  }
  const browser = res.browser || {};
  browser.localPathHint = localPathHintFromURL(browser.url || "");
  state.lastPage = browser;
  return browser;
}

let scopePersistQueue = Promise.resolve();

function persistScopePatch(patch, expectedScope = state.scopeKey) {
  if (!expectedScope) return Promise.resolve();
  const localStore = updateScopeRecord(
    { [expectedScope]: state.scopeRecord },
    expectedScope,
    patch
  );
  if (state.scopeKey === expectedScope) {
    state.scopeRecord = readScopeRecord(localStore, expectedScope);
  }
  scopePersistQueue = scopePersistQueue.catch(() => {}).then(async () => {
    const data = await chrome.storage.session.get(PAGE_SCOPE_STORE_KEY);
    const nextStore = updateScopeRecord(data[PAGE_SCOPE_STORE_KEY], expectedScope, patch);
    await chrome.storage.session.set({ [PAGE_SCOPE_STORE_KEY]: nextStore });
    if (state.scopeKey === expectedScope) {
      state.scopeRecord = readScopeRecord(nextStore, expectedScope);
    }
  });
  return scopePersistQueue;
}

function closePopovers() {
  $("settings-panel").hidden = true;
  $("attach-menu").hidden = true;
  $("mention-popup").hidden = true;
}

function isDrySessionId(id) {
  return /^dry-session/i.test(String(id || ""));
}

function applySessionId(id, expectedScope = state.scopeKey) {
  if (!id) return;
  if (!expectedScope || state.scopeKey !== expectedScope) return;
  // dry-run 的 session 不写入真实续聊
  if (isDrySessionId(id) && !$("dry-run").checked) return;
  if (isDrySessionId(id)) {
    $("session-id").textContent = "dry-run";
    return;
  }
  state.sessionId = id;
  $("session-id").textContent = state.sessionId;
  persistScopePatch({ activeSessionId: state.sessionId }, expectedScope).catch(() => {});
  renderSessionList();
}

function handleHostEvent(event) {
  const cmds = mapHostEventToUI(event);
  for (const cmd of cmds) {
    switch (cmd.kind) {
      case "hidden":
        break;
      case "thinking":
        appendThinking(cmd.text || "");
        state.liveThinkingChars += (cmd.text || "").length;
        break;
      case "assistant":
        // 新 assistant 段前关闭 thinking 句柄，避免串
        updateAssistantMarkdown(cmd.text || "", !!cmd.streaming);
        state.liveAssistantChars += (cmd.text || "").length;
        break;
      case "error":
        appendError(cmd.text || "error");
        break;
      case "session":
        applySessionId(cmd.sessionId);
        break;
      default:
        break;
    }
  }
}

/** 统一处理 background → sidepanel 消息（Port 与 runtime 双通道）。 */
function onBackgroundMessage(msg) {
  if (!msg || typeof msg !== "object") return;
  if (msg.type === "host-event") {
    // 只接收当前页面当前 turn 的事件，避免切页后旧流继续写入新页面。
    if (!state.requestId || msg.requestId !== state.requestId) return;
    if (msg.sessionId) applySessionId(msg.sessionId);
    if (msg.event) handleHostEvent(msg.event);
    return;
  }
  if (msg.type === "host-status") {
    setHostStatus(true, `host ${msg.msg?.version || "ok"}`);
    return;
  }
  if (msg.type === "active-page-changed" && msg.page) {
    if (state.windowId != null && msg.page.windowId !== state.windowId) return;
    queuePageActivation(msg.page);
    return;
  }
  if (msg.type === "page-tab-removed") {
    const prefix = `v1|tab:${Number(msg.tabId)}|`;
    for (const key of state.drafts.keys()) {
      if (key.startsWith(prefix)) state.drafts.delete(key);
    }
    return;
  }
  if (msg.type === "prefill-selection" && msg.selection) {
    applyPendingAsk({
      kind: "selection",
      text: msg.selection,
      tabId: msg.tabId,
    }).catch(() => {});
  }
  if (msg.type === "pending-ask" && msg.ask) {
    applyPendingAsk(msg.ask).catch(() => {});
  }
}

/** 消费右键 / 划词带来的待问内容。 */
async function applyPendingAsk(ask) {
  if (!ask || typeof ask !== "object") return;
  const page = await collectPage(ask.tabId);
  if (state.windowId != null && page?.windowId != null && page.windowId !== state.windowId) return;
  if (page) await queuePageActivation(page);
  const input = $("input");
  if (ask.kind === "image") {
    addChip({
      kind: "image",
      src: ask.image?.src || "",
      dataUrl: ask.image?.dataUrl || "",
      alt: ask.image?.alt || "选中图片",
      title: "选中图片",
    });
    if (input && !input.value.trim()) {
      input.value = ask.text || "这张图是什么？请结合当前页说明。";
    }
  } else if (ask.kind === "page") {
    addChip({ kind: "page", title: page?.title || "", url: ask.pageUrl || page?.url || "" });
    if (input) input.value = ask.text || "请总结当前页要点。";
  } else if (ask.kind === "link") {
    addChip({ kind: "page", title: ask.text || "链接", url: ask.pageUrl || ask.text || "" });
    if (input) input.value = `请介绍这个链接：${ask.pageUrl || ask.text || ""}`;
  } else {
    const text = ask.text || ask.selection || "";
    if (text) addChip({ kind: "selection", text });
    if (input) input.value = text ? `请解释这段内容：\n${text}` : input.value;
  }
  autosizeInput();
  input?.focus();
}

chrome.runtime.onMessage.addListener((msg) => {
  onBackgroundMessage(msg);
});

/** 长连接 Port：流式 thinking/text 可靠送达（sendMessage 在侧栏 await 时可能丢）。 */
function connectSidepanelPort() {
  try {
    if (state.port) {
      try {
        state.port.disconnect();
      } catch {
        /* ignore */
      }
    }
    state.port = chrome.runtime.connect({ name: "sidepanel" });
    state.port.onMessage.addListener(onBackgroundMessage);
    state.port.onDisconnect.addListener(() => {
      state.port = null;
      // service worker 休眠后重连
      setTimeout(connectSidepanelPort, 400);
    });
  } catch {
    setTimeout(connectSidepanelPort, 800);
  }
}
connectSidepanelPort();

/**
 * send_done 后仅当 live 完全没收到对应内容时，用 batch events 补一次。
 * 有任何 live 字符就不再 replay，避免与 Port 流叠成「TheThe user user」。
 * @param {any[]} events
 */
function replayEventsIfNeeded(events) {
  if (!Array.isArray(events) || !events.length) return;
  const needThinking = state.liveThinkingChars === 0;
  const needAssistant = state.liveAssistantChars === 0;
  if (!needThinking && !needAssistant) return;

  let think = "";
  let text = "";
  for (const ev of events) {
    if (!ev) continue;
    if (ev.type === "thinking") think += ev.text || "";
    else if (ev.type === "partial" && /thinking/i.test(String(ev.rawType || ""))) think += ev.text || "";
    else if (ev.type === "text") text += ev.text || "";
    else if (
      ev.type === "partial" &&
      ev.rawType !== "host.argv" &&
      ev.rawType !== "stderr" &&
      !/thinking/i.test(String(ev.rawType || ""))
    ) {
      text += ev.text || "";
    }
  }
  if (needThinking && think.trim()) {
    state.thinkingEl = null;
    state.thinkingDetails = null;
    appendThinking(think);
    state.liveThinkingChars = think.length;
  }
  if (needAssistant && text.trim()) {
    state.assistantEl = null;
    state.assistantRaw = "";
    updateAssistantMarkdown(text, false);
    state.liveAssistantChars = text.length;
  }
}

/** 强制解锁 busy（切换会话 / 取消后）。 */
async function forceUnlock(reason) {
  if (state.requestId) {
    try {
      await chrome.runtime.sendMessage({ type: "host-cancel", requestId: state.requestId });
    } catch {
      /* ignore */
    }
  }
  state.busy = false;
  setBusy(false);
  state.requestId = "";
  if (reason) {
    // 不刷错误条，避免切换会话吵；仅 console
    console.debug("[sidepanel] unlock:", reason);
  }
}

function addChip(chip) {
  if (chip.kind !== "tab") {
    state.chips = state.chips.filter((c) => c.kind !== chip.kind);
  }
  state.chips.push(chip);
  renderChips();
}

function setMediaStatus(text) {
  const el = $("media-status");
  if (el) el.textContent = text || "";
}

function fileToDataURL(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error || new Error("读取文件失败"));
    reader.readAsDataURL(file);
  });
}

async function stopRecording() {
  const recorder = state.mediaRecorder;
  if (recorder && recorder.state !== "inactive") recorder.stop();
}

async function startRecording() {
  if (!navigator.mediaDevices?.getUserMedia || !window.MediaRecorder) {
    appendError("当前浏览器不支持录音。");
    return;
  }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const preferred = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"].find(
      (type) => MediaRecorder.isTypeSupported(type)
    );
    const recorder = preferred ? new MediaRecorder(stream, { mimeType: preferred }) : new MediaRecorder(stream);
    state.mediaStream = stream;
    state.mediaRecorder = recorder;
    state.recordingChunks = [];
    recorder.addEventListener("dataavailable", (event) => {
      if (event.data?.size) state.recordingChunks.push(event.data);
    });
    recorder.addEventListener("stop", async () => {
      $("btn-voice")?.classList.remove("recording");
      state.mediaStream?.getTracks?.().forEach((track) => track.stop());
      const chunks = state.recordingChunks.slice();
      const type = recorder.mimeType || chunks[0]?.type || "audio/webm";
      state.mediaRecorder = null;
      state.mediaStream = null;
      state.recordingChunks = [];
      if (!chunks.length) {
        setMediaStatus("");
        return;
      }
      try {
        setMediaStatus("正在转写…");
        const mediaData = await fileToDataURL(new Blob(chunks, { type }));
        const response = await chrome.runtime.sendMessage({
          type: "transcribe-audio",
          mediaData,
          mimeType: type,
          fileName: type.includes("mp4") ? "recording.m4a" : "recording.webm",
          apiBase: $("api-base")?.value.trim() || "",
          apiKey: $("api-key")?.value.trim() || "",
        });
        if (!response?.ok) throw new Error(response?.error || "语音转写失败");
        const input = $("input");
        input.value = [input.value.trim(), response.text.trim()].filter(Boolean).join("\n");
        autosizeInput();
        input.focus();
        setMediaStatus("已转成文字");
      } catch (error) {
        setMediaStatus("");
        appendError(String(error?.message || error));
      }
    });
    recorder.start(500);
    $("btn-voice")?.classList.add("recording");
    setMediaStatus("录音中，再点一次结束");
  } catch (error) {
    setMediaStatus("");
    appendError(`无法使用麦克风：${String(error?.message || error)}`);
  }
}

$("btn-voice")?.addEventListener("click", () => {
  if (state.mediaRecorder?.state === "recording") stopRecording();
  else startRecording();
});

$("btn-ocr")?.addEventListener("click", () => $("ocr-file")?.click());
$("ocr-file")?.addEventListener("change", async (event) => {
  const file = event.target.files?.[0];
  event.target.value = "";
  if (!file) return;
  if (file.size > 6 * 1024 * 1024) {
    appendError("图片不能超过 6 MB。");
    return;
  }
  try {
    setMediaStatus("正在读取图片…");
    const dataUrl = await fileToDataURL(file);
    addChip({ kind: "image", dataUrl, alt: file.name, title: file.name });
    const input = $("input");
    if (!input.value.trim()) input.value = "请识别图片中的文字，并按原有结构整理。";
    autosizeInput();
    input.focus();
    setMediaStatus("图片已附加");
  } catch (error) {
    setMediaStatus("");
    appendError(String(error?.message || error));
  }
});

$("btn-pdf")?.addEventListener("click", () => $("pdf-file")?.click());
$("pdf-file")?.addEventListener("change", async (event) => {
  const file = event.target.files?.[0];
  event.target.value = "";
  if (!file) return;
  if (file.size > 20 * 1024 * 1024) {
    appendError("PDF 不能超过 20 MB。");
    return;
  }
  try {
    setMediaStatus("正在读取 PDF…");
    const mediaData = await fileToDataURL(file);
    const response = await chrome.runtime.sendMessage({ type: "extract-pdf", mediaData });
    if (!response?.ok) throw new Error(response?.error || "PDF 读取失败");
    addChip({ kind: "document", title: file.name, text: response.text || "" });
    const input = $("input");
    if (!input.value.trim()) input.value = "请总结这份 PDF，并列出关键信息。";
    autosizeInput();
    input.focus();
    setMediaStatus("PDF 已附加");
  } catch (error) {
    setMediaStatus("");
    appendError(String(error?.message || error));
  }
});

async function addChipFromKind(kind) {
  const page = (await collectPage()) || {};
  if (kind === "page") addChip({ kind: "page", title: page.title || "", url: page.url || "" });
  else if (kind === "selection") addChip({ kind: "selection", text: page.selection || "(无选区)" });
  else if (kind === "tabs") {
    for (const t of page.tabs || []) {
      addChip({ kind: "tab", title: t.title || "", url: t.url || "", tabId: t.id });
    }
  } else if (kind === "pageText") {
    addChip({ kind: "pageText", text: (page.pageText || "").slice(0, 500) });
  }
}

// Attach / settings — floating popovers, never stay in document flow
$("btn-attach").addEventListener("click", (e) => {
  e.stopPropagation();
  $("settings-panel").hidden = true;
  $("attach-menu").hidden = !$("attach-menu").hidden;
});
document.querySelectorAll("[data-add-chip]").forEach((btn) => {
  btn.addEventListener("click", () => {
    addChipFromKind(btn.getAttribute("data-add-chip"));
    $("attach-menu").hidden = true;
  });
});

$("btn-settings").addEventListener("click", (e) => {
  e.stopPropagation();
  $("attach-menu").hidden = true;
  $("settings-panel").hidden = !$("settings-panel").hidden;
});
$("btn-close-settings")?.addEventListener("click", (e) => {
  e.stopPropagation();
  $("settings-panel").hidden = true;
});

// 点页面任意处关闭浮层（浮层内部 stopPropagation）
document.addEventListener("click", () => closePopovers());
$("settings-panel").addEventListener("click", (e) => e.stopPropagation());
$("attach-menu").addEventListener("click", (e) => e.stopPropagation());
$("composer-box")?.addEventListener?.("click", (e) => {
  // 点输入区不关，但点 ⋯/@ 由各自 handler 处理
  if (e.target.closest("#btn-settings") || e.target.closest("#btn-attach")) return;
});
// composer-box 不存在 id — 用 class
document.querySelector(".composer-box")?.addEventListener("click", (e) => {
  e.stopPropagation();
});

// @ mention popup
const MENTION_ITEMS = [
  { kind: "page", label: "当前页", desc: "标题与 URL" },
  { kind: "selection", label: "选中文本", desc: "页面选区" },
  { kind: "tabs", label: "标签页", desc: "当前窗口" },
  { kind: "pageText", label: "页面正文", desc: "摘录" },
];

function updateMentionPopup() {
  const input = $("input");
  const popup = $("mention-popup");
  const val = input.value;
  const caret = input.selectionStart || 0;
  const before = val.slice(0, caret);
  const at = before.match(/(?:^|\s)@([\w-]*)$/);
  if (!at) {
    popup.hidden = true;
    popup.innerHTML = "";
    return;
  }
  const q = (at[1] || "").toLowerCase();
  const items = MENTION_ITEMS.filter(
    (i) => !q || i.kind.startsWith(q) || i.label.includes(q)
  );
  if (!items.length) {
    popup.hidden = true;
    return;
  }
  popup.hidden = false;
  popup.innerHTML = "";
  items.forEach((item, idx) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = `mention-item${idx === 0 ? " active" : ""}`;
    b.innerHTML = `<strong>${item.label}</strong><div class="muted">${item.desc}</div>`;
    b.addEventListener("click", () => applyMention(item.kind));
    popup.appendChild(b);
  });
}

function applyMention(kind) {
  const input = $("input");
  const val = input.value;
  const caret = input.selectionStart || 0;
  const before = val.slice(0, caret);
  const after = val.slice(caret);
  const replaced = before.replace(/(?:^|\s)@([\w-]*)$/, (m) => {
    const lead = m.startsWith(" ") || m.startsWith("\n") ? m[0] : "";
    const token = kind === "pageText" ? "@body" : `@${kind === "tabs" ? "tabs" : kind}`;
    return `${lead}${token} `;
  });
  input.value = replaced + after;
  $("mention-popup").hidden = true;
  addChipFromKind(kind === "pageText" ? "pageText" : kind);
  input.focus();
  autosizeInput();
}

$("input").addEventListener("focus", () => {
  $("settings-panel").hidden = true;
  $("attach-menu").hidden = true;
});
$("input").addEventListener("input", () => {
  updateMentionPopup();
  autosizeInput();
});
$("input").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    if (!$("mention-popup").hidden) {
      const active = $("mention-popup").querySelector(".mention-item.active");
      if (active) {
        active.click();
        return;
      }
    }
    $("btn-send").click();
  }
});

// ---------- 历史会话（列表 / 搜索 / 选中续聊） ----------

function escapeText(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function clearFeed() {
  const log = $("log");
  log.innerHTML = "";
  const empty = document.createElement("div");
  empty.id = "empty-state";
  empty.className = "empty";
  empty.innerHTML = emptyStateHTML();
  log.appendChild(empty);
  state.assistantEl = null;
  state.thinkingEl = null;
  state.thinkingDetails = null;
  state.assistantRaw = "";
}

let scopeActivationQueue = Promise.resolve();

function queuePageActivation(page) {
  scopeActivationQueue = scopeActivationQueue
    .then(() => activatePageScope(page))
    .catch((error) => console.debug("[sidepanel] page scope activation failed:", error));
  return scopeActivationQueue;
}

async function activatePageScope(page) {
  const nextScope = derivePageScope(page?.tabId, page?.url || "");
  if (!nextScope) return false;
  state.lastPage = page;
  if (nextScope === state.scopeKey) return false;

  const input = $("input");
  if (state.scopeKey) {
    state.drafts.set(state.scopeKey, {
      text: input?.value || "",
      chips: state.chips.slice(),
    });
  }
  if (state.busy) await forceUnlock("page-scope-changed");

  const epoch = ++state.scopeEpoch;
  state.scopeKey = nextScope;
  state.sessionId = "";
  state.scopeRecord = emptyScopeRecord();
  state.sessions = [];
  state.requestId = "";
  $("session-id").textContent = "新";
  $("thread-title").textContent = DEFAULT_THREAD_TITLE;
  closeDrawer();
  clearFeed();

  const draft = state.drafts.get(nextScope);
  if (input) input.value = draft?.text || "";
  state.chips = Array.isArray(draft?.chips) ? draft.chips.slice() : [];
  renderChips();
  autosizeInput();

  await scopePersistQueue.catch(() => {});
  const data = await chrome.storage.session.get(PAGE_SCOPE_STORE_KEY);
  if (state.scopeKey !== nextScope || state.scopeEpoch !== epoch) return true;
  state.scopeRecord = readScopeRecord(data[PAGE_SCOPE_STORE_KEY], nextScope);
  state.sessionId = state.scopeRecord.activeSessionId;
  $("session-id").textContent = state.sessionId || "新";
  $("thread-title").textContent = state.scopeRecord.title || DEFAULT_THREAD_TITLE;
  if (state.sessionId) {
    await selectSession(state.sessionId, {
      expectedScope: nextScope,
      closeAfter: false,
    });
  }
  return true;
}

function formatSessionTime(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso).slice(0, 10);
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) {
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function shortCwd(cwd) {
  if (!cwd) return "";
  const home = ""; // browser side can't expand ~
  const s = String(cwd);
  if (s.length <= 36) return s;
  return "…" + s.slice(-34);
}

async function loadSessions(query) {
  state.sessionLoading = true;
  const status = $("session-list-status");
  if (status) status.textContent = "加载会话…";
  try {
    const res = await chrome.runtime.sendMessage({
      type: "list-sessions",
      query: query || "",
      limit: 200,
    });
    if (!res?.ok) {
      state.sessions = [];
      if (status) status.textContent = res?.error || "无法加载会话（检查 native host）";
      renderSessionList();
      return;
    }
    state.sessions = filterSessionsForScope(res.sessions, state.scopeRecord);
    if (status) {
      status.textContent = state.sessions.length
        ? `${state.sessions.length} 个本页会话`
        : "当前标签页暂无会话";
    }
    renderSessionList();
  } catch (e) {
    state.sessions = [];
    if (status) status.textContent = String(e?.message || e);
    renderSessionList();
  } finally {
    state.sessionLoading = false;
  }
}

function renderSessionList() {
  const ul = $("thread-list");
  if (!ul) return;
  ul.innerHTML = "";
  for (const s of state.sessions) {
    const li = document.createElement("li");
    const btn = document.createElement("button");
    btn.type = "button";
    const active = s.id && s.id === state.sessionId;
    btn.className = `thread-item${active ? " active" : ""}`;
    const title = s.title || s.summary || "（无标题）";
    const time = formatSessionTime(s.updatedAt);
    const cwd = shortCwd(s.cwd);
    btn.innerHTML =
      `<span class="title">${escapeText(title)}</span>` +
      (cwd ? `<span class="preview">${escapeText(cwd)}</span>` : "") +
      `<span class="meta">${escapeText(time)}${s.numMessages ? " · " + s.numMessages + " 条" : ""}</span>`;
    btn.addEventListener("click", () => selectSession(s.id));
    li.appendChild(btn);
    ul.appendChild(li);
  }
}

/**
 * 选中历史会话：加载 transcript 并续聊。
 */
async function selectSession(sessionId, options = {}) {
  if (!sessionId) return;
  const expectedScope = options.expectedScope || state.scopeKey;
  if (!expectedScope || state.scopeKey !== expectedScope) return;
  if (!state.scopeRecord.sessionIds.includes(sessionId)) {
    appendError("已阻止打开其他网页绑定的会话。");
    return;
  }
  // 切换会话时必须打断当前 turn，否则 busy 卡住导致「发送了但输入框不空」
  if (state.busy) await forceUnlock("switch-session");
  const status = $("session-list-status");
  if (status) status.textContent = "打开会话…";
  try {
    const res = await chrome.runtime.sendMessage({
      type: "get-session",
      sessionId,
      limit: 60,
    });
    if (!res?.ok) {
      appendError(res?.error || "无法打开会话");
      return;
    }
    if (state.scopeKey !== expectedScope) return;
    state.sessionId = res.sessionId || sessionId;
    const title = res.session?.title || res.session?.summary || "会话";
    $("session-id").textContent = state.sessionId;
    $("thread-title").textContent = title;
    await persistScopePatch(
      { activeSessionId: state.sessionId, title },
      expectedScope
    ).catch(() => {});
    if (state.scopeKey !== expectedScope) return;

    clearFeed();
    hideEmpty();
    const msgs = Array.isArray(res.messages) ? res.messages : [];
    if (!msgs.length) {
      const log = $("log");
      const tip = document.createElement("div");
      tip.className = "tool-row";
      tip.innerHTML = `<strong>已选中历史会话</strong><div class="tool-preview">暂无本地 transcript 预览，发送消息将 --resume 续聊</div>`;
      log.appendChild(tip);
    } else {
      for (const m of msgs) {
        if (m.role === "user") appendUserBubble(m.text || "");
        else if (m.role === "thinking") appendThinking(m.text || "");
        else if (m.role === "assistant") {
          state.assistantEl = null;
          state.assistantRaw = "";
          updateAssistantMarkdown(m.text || "", false);
          state.assistantEl = null;
          state.assistantRaw = "";
        }
      }
    }
    renderSessionList();
    if (options.closeAfter !== false) closeDrawer();
  } catch (e) {
    if (state.scopeKey === expectedScope) appendError(String(e));
  }
}

function persistActiveThread(patch) {
  // 续聊时更新标题展示；历史列表仍以本机会话库为准
  if (patch?.title) $("thread-title").textContent = patch.title;
  if (patch?.sessionId) {
    state.sessionId = patch.sessionId;
    $("session-id").textContent = state.sessionId;
    persistScopePatch({ activeSessionId: state.sessionId }).catch(() => {});
  }
}

function newThread() {
  if (state.busy) forceUnlock("new-thread");
  state.sessionId = "";
  $("session-id").textContent = "新";
  $("thread-title").textContent = DEFAULT_THREAD_TITLE;
  persistScopePatch({ activeSessionId: "", title: DEFAULT_THREAD_TITLE }).catch(() => {});
  clearFeed();
  renderSessionList();
  closeDrawer();
}

function openDrawer() {
  $("thread-drawer").hidden = false;
  $("drawer-scrim").hidden = false;
  loadSessions(state.sessionQuery || $("session-search")?.value || "");
}
function closeDrawer() {
  $("thread-drawer").hidden = true;
  $("drawer-scrim").hidden = true;
}

async function initializePageScope() {
  try {
    const currentWindow = await chrome.windows.getCurrent();
    state.windowId = currentWindow?.id ?? null;
  } catch {
    state.windowId = null;
  }
  const page = await collectPage();
  if (page?.windowId != null) state.windowId = page.windowId;
  if (page) await queuePageActivation(page);
  // 旧版全局键会造成跨网页续聊；升级后不再读取，并主动清除。
  chrome.storage.local.remove(["lastSessionId", "lastSessionTitle"]).catch(() => {});
}

$("btn-new").addEventListener("click", newThread);
$("btn-new-thread-drawer").addEventListener("click", newThread);
$("btn-threads").addEventListener("click", () => {
  if ($("thread-drawer").hidden) openDrawer();
  else closeDrawer();
});
$("btn-close-threads").addEventListener("click", closeDrawer);
$("drawer-scrim").addEventListener("click", closeDrawer);
// 历史搜索（防抖）
let searchTimer = 0;
$("session-search")?.addEventListener("input", () => {
  const q = $("session-search").value.trim();
  state.sessionQuery = q;
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => loadSessions(q), 220);
});

$("btn-cancel").addEventListener("click", async () => {
  try {
    await chrome.runtime.sendMessage({ type: "host-cancel", requestId: state.requestId });
  } catch (e) {
    appendError(String(e));
  }
});

$("btn-send").addEventListener("click", async () => {
  const inputEl = $("input");
  const raw = (inputEl?.value || "").trim();
  if (!raw) return;

  // 若上一 turn 卡 busy，先取消再发，避免「输入框文字发不出去」
  if (state.busy) {
    await forceUnlock("re-send");
  }

  let page;
  try {
    page = await collectPage();
    if (!page) throw new Error("无法读取当前标签页");
    await queuePageActivation(page);
    await scopePersistQueue.catch(() => {});
  } catch (e) {
    appendError(String(e));
    return;
  }

  const turnScope = state.scopeKey;
  if (!turnScope) {
    appendError("当前页面没有可用的隔离作用域。");
    return;
  }

  // 页面作用域确认后再快照 session/chip，禁止沿用上一网页的状态。
  const displayText = stripMentionTokens(raw) || raw;
  const chipsSnapshot = state.chips.slice();
  const cwd = $("cwd").value.trim();
  const dryRun = !!$("dry-run").checked;
  const attachBody = $("attach-body")?.checked !== false;
  const sessionIdSnapshot =
    dryRun || isDrySessionId(state.sessionId) ? "" : state.sessionId || "";

  closePopovers();
  // 双保险清空
  if (inputEl) {
    inputEl.value = "";
    inputEl.textContent = "";
    inputEl.dispatchEvent(new Event("input", { bubbles: true }));
  }
  autosizeInput();
  state.chips = [];
  renderChips();
  state.drafts.set(turnScope, { text: "", chips: [] });

  setBusy(true);
  state.assistantEl = null;
  state.thinkingEl = null;
  state.thinkingDetails = null;
  state.assistantRaw = "";
  state.liveThinkingChars = 0;
  state.liveAssistantChars = 0;

  const requestId = `ui-${Date.now()}`;
  state.requestId = requestId;

  // 安全阀：防止 host 无响应导致永远卡在「停止」
  const busyWatchdog = setTimeout(() => {
    if (state.busy && state.requestId === requestId) {
      setBusy(false);
      state.requestId = "";
      appendError("响应超时，已解除锁定。可点停止或重试。");
    }
  }, 180000);

  appendUserBubble(displayText);

  // 标题：用用户首句，不依赖已删除的 state.threads
  const titleEl = $("thread-title");
  if (
    titleEl &&
    (!titleEl.textContent ||
      titleEl.textContent === DEFAULT_THREAD_TITLE ||
      titleEl.textContent === "新聊天")
  ) {
    const title = titleFromUserText(displayText);
    titleEl.textContent = title;
    persistScopePatch({ title }, turnScope).catch(() => {});
  }

  // 让浏览器先画出「输入已清空 + 用户气泡」
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  inputEl?.focus();

  // 确保 Port 连着
  if (!state.port) connectSidepanelPort();

  try {
    const browser = packBrowserContext({
      page,
      inputText: raw,
      chips: chipsSnapshot,
      defaultPageText: attachBody,
      includePageText:
        chipsSnapshot.some((c) => c.kind === "pageText") || /@body|@pageText/i.test(raw)
          ? true
          : undefined,
    });

    const payload = {
      requestId,
      pageScope: turnScope,
      sessionId: sessionIdSnapshot,
      cwd,
      apiBase: $("api-base")?.value.trim() || "",
      apiKey: $("api-key")?.value.trim() || "",
      model: $("api-model")?.value.trim() || "",
      text: displayText,
      browser,
      reasoningEffort: "high",
      dryRun,
    };

    const result = await chrome.runtime.sendMessage({ type: "host-send", payload });
    if (state.scopeKey !== turnScope || state.requestId !== requestId) return;
    if (result?.sessionId) applySessionId(result.sessionId, turnScope);
    if (result?.error) appendError(result.error);
    else if (result?.ok === false && !result?.error) appendError("请求失败");

    // live 丢事件时，用 batch events 补 thinking/text
    replayEventsIfNeeded(result?.events || []);
  } catch (e) {
    if (state.scopeKey === turnScope && state.requestId === requestId) {
      appendError(String(e));
      setHostStatus(false, String(e));
    }
  } finally {
    clearTimeout(busyWatchdog);
    if (state.requestId === requestId) {
      state.requestId = "";
      setBusy(false);
      // 再清一次输入，防止异常路径回写
      if (inputEl && inputEl.value === raw) inputEl.value = "";
      autosizeInput();
      state.assistantEl = null;
      // 保留 thinkingEl 以便用户仍能看到本 turn 思考块；下次 send 会重置
    }
  }
});

// Prefs — 附带正文默认开；dry-run 默认关。迁移时关闭旧版浏览器工具开关。
chrome.storage.local.get(["cwd", "attachBody", "dryRun", "apiBase", "apiKey", "apiModel"]).then((data) => {
  if (data.cwd) $("cwd").value = data.cwd;
  if (data.apiBase) $("api-base").value = data.apiBase;
  if (data.apiKey) $("api-key").value = data.apiKey;
  if (data.apiModel) $("api-model").value = data.apiModel;
  $("dry-run").checked = data.dryRun === true;
  if (typeof data.attachBody === "boolean") {
    $("attach-body").checked = data.attachBody;
  } else {
    $("attach-body").checked = true;
  }
  chrome.storage.local.set({ browserControl: false }).catch(() => {});
});
function persistPrefs() {
  chrome.storage.local.set({
    cwd: $("cwd").value,
    apiBase: $("api-base")?.value || "",
    apiKey: $("api-key")?.value || "",
    apiModel: $("api-model")?.value || "",
    attachBody: $("attach-body").checked,
    dryRun: $("dry-run").checked,
  });
}
["cwd", "dry-run", "attach-body", "api-base", "api-key", "api-model"].forEach((id) => {
  const el = $(id);
  if (!el) return;
  el.addEventListener("change", persistPrefs);
});

// Init
renderChips();
initializePageScope().catch((error) => appendError(String(error)));
autosizeInput();
chrome.runtime
  .sendMessage({ type: "host-ping" })
  .then((r) => {
    if (r?.ok) setHostStatus(true, `host ${r.version || "ok"}`);
    else setHostStatus(false, r?.error || "host unavailable");
  })
  .catch((e) => setHostStatus(false, String(e?.message || e)));

chrome.runtime
  .sendMessage({ type: "consume-pending-ask" })
  .then((r) => {
    if (r?.ask) return applyPendingAsk(r.ask);
  })
  .catch(() => {});
