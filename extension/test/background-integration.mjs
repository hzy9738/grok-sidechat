import assert from "node:assert/strict";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { fileURLToPath } from "node:url";

const directory = path.dirname(fileURLToPath(import.meta.url));
const runtimeMessageListeners = [];
const runtimeConnectListeners = [];
const nativeMessageListeners = [];
const nativeOutbound = [];
const sidepanelOutbound = [];
const storage = { browserControl: true };
const sessionStorage = {};
const tabActivatedListeners = [];
const tabUpdatedListeners = [];
const tabRemovedListeners = [];
const activeTab = {
  id: 11,
  windowId: 3,
  active: true,
  title: "Mock page",
  url: "https://example.test/page",
};
let scriptExecutions = 0;

const eventTarget = () => ({
  addListener(listener) { this.listener = listener; },
  removeListener() {},
});

const nativePort = {
  onMessage: eventTarget(),
  onDisconnect: eventTarget(),
  postMessage(message) { nativeOutbound.push(message); },
};
nativePort.onMessage.addListener = (listener) => nativeMessageListeners.push(listener);

globalThis.chrome = {
  runtime: {
    lastError: undefined,
    onInstalled: { addListener() {} },
    onStartup: { addListener() {} },
    onMessage: { addListener(listener) { runtimeMessageListeners.push(listener); } },
    onConnect: { addListener(listener) { runtimeConnectListeners.push(listener); } },
    connectNative() { return nativePort; },
    sendMessage: async () => undefined,
  },
  sidePanel: {
    setPanelBehavior: async () => undefined,
    open: async () => undefined,
  },
  contextMenus: {
    removeAll(callback) { callback?.(); },
    create() {},
    onClicked: { addListener() {} },
  },
  commands: { onCommand: { addListener() {} } },
  tabs: {
    async query(query) { return query?.active ? [activeTab] : [activeTab]; },
    async get(tabId) { return tabId === activeTab.id ? activeTab : null; },
    onActivated: { addListener(listener) { tabActivatedListeners.push(listener); } },
    onUpdated: { addListener(listener) { tabUpdatedListeners.push(listener); }, removeListener() {} },
    onRemoved: { addListener(listener) { tabRemovedListeners.push(listener); } },
  },
  scripting: {
    async executeScript() {
      scriptExecutions++;
      return [];
    },
  },
  storage: {
    local: {
      async get(keys) {
        const names = Array.isArray(keys) ? keys : Object.keys(keys || {});
        return Object.fromEntries(names.map((name) => [name, storage[name]]));
      },
      async set(values) { Object.assign(storage, values); },
    },
    session: {
      async get(keys) {
        const names = Array.isArray(keys) ? keys : [keys];
        return Object.fromEntries(names.map((name) => [name, sessionStorage[name]]));
      },
      async set(values) { Object.assign(sessionStorage, values); },
      async remove(key) { delete sessionStorage[key]; },
    },
  },
};

await import(pathToFileURL(path.join(directory, "..", "background.js")).href);
assert.equal(runtimeMessageListeners.length, 1, "background should register one runtime message handler");
assert.equal(runtimeConnectListeners.length, 1, "background should register one runtime connect handler");

runtimeConnectListeners[0]({
  name: "sidepanel",
  postMessage(message) { sidepanelOutbound.push(message); },
  onDisconnect: { addListener() {} },
});

const runtimeHandler = runtimeMessageListeners[0];
let resolveHostSend;
const hostSendResponse = new Promise((resolve) => { resolveHostSend = resolve; });
assert.equal(runtimeHandler({
  type: "host-send",
  payload: {
    requestId: "tool-free-turn",
    pageScope: "v1|tab:11|origin:https://example.test",
    text: "think about this page",
    browserControl: true,
    enableBrowserControl: true,
    browser: {
      tabId: 11,
      windowId: 3,
      title: "Mock page",
      url: "https://example.test/page",
      pageText: "Mock page body",
      enableBrowserControl: true,
    },
    mode: "yolo",
    maxTurns: 99,
    alwaysApprove: true,
  },
}, {}, resolveHostSend), true);

await new Promise((resolve) => setTimeout(resolve, 0));

const nativeSend = nativeOutbound.find(
  (message) => message.op === "send" && message.requestId === "tool-free-turn"
);
assert.ok(nativeSend);
assert.equal(nativeSend.model, "");
assert.equal(nativeSend.apiBase, "");
assert.equal(nativeSend.apiKey, "");
assert.equal(nativeSend.reasoningEffort, "high");
assert.equal(nativeSend.mode, "default");
assert.equal(nativeSend.maxTurns, 1);
assert.equal(nativeSend.alwaysApprove, false);
assert.equal(nativeSend.browser.enableBrowserControl, undefined);
assert.equal(storage.browserControl, false);
assert.equal(nativeSend.browser.tabId, 11);

tabActivatedListeners[0]({ tabId: activeTab.id, windowId: activeTab.windowId });
await new Promise((resolve) => setTimeout(resolve, 0));
assert.ok(
  sidepanelOutbound.some(
    (message) => message.type === "active-page-changed" && message.page?.tabId === 11
  ),
  "active tab changes should notify the side panel"
);

const outboundBeforeBlockedSend = nativeOutbound.length;
const blockedCrossPageSend = await new Promise((resolve) => {
  runtimeHandler(
    {
      type: "host-send",
      payload: {
        requestId: "wrong-page",
        pageScope: "v1|tab:99|origin:https://other.test",
        text: "do not send",
        browser: {
          tabId: 11,
          windowId: 3,
          url: "https://example.test/page",
        },
      },
    },
    {},
    resolve
  );
});
assert.equal(blockedCrossPageSend.ok, false);
assert.match(blockedCrossPageSend.error, /跨网页|标签页已切换/);
assert.equal(nativeOutbound.length, outboundBeforeBlockedSend);

sessionStorage.pageScopeSessionsV1 = {
  "v1|tab:11|origin:https://example.test": {
    activeSessionId: "bound-session",
    sessionIds: ["bound-session"],
    title: "Bound",
  },
};
const blockedForeignSession = await new Promise((resolve) => {
  runtimeHandler(
    {
      type: "host-send",
      payload: {
        requestId: "foreign-session",
        pageScope: "v1|tab:11|origin:https://example.test",
        sessionId: "session-from-another-page",
        text: "do not resume",
        browser: {
          tabId: 11,
          windowId: 3,
          url: "https://example.test/page",
        },
      },
    },
    {},
    resolve
  );
});
assert.equal(blockedForeignSession.ok, false);
assert.match(blockedForeignSession.error, /不属于当前网页/);
assert.equal(nativeOutbound.length, outboundBeforeBlockedSend);

function emitNative(message) {
  for (const listener of nativeMessageListeners) listener(message);
}

emitNative({
  op: "event",
  requestId: "tool-free-turn",
  event: { type: "thinking", text: "reasoning..." },
});
assert.ok(
  sidepanelOutbound.some(
    (message) => message.type === "host-event" && message.event?.type === "thinking"
  ),
  "thinking event should stream to the side panel"
);

emitNative({
  op: "browser_action",
  requestId: "tool-free-turn",
  callId: "unexpected-tool-call",
  action: "click",
  arguments: { selector: "button" },
});
await new Promise((resolve) => setTimeout(resolve, 0));
const blockedTool = nativeOutbound.find((message) => message.callId === "unexpected-tool-call");
assert.equal(blockedTool.ok, false);
assert.match(blockedTool.error, /tools are disabled/i);
assert.equal(scriptExecutions, 0, "blocked model tool must not touch the page");

emitNative({ op: "send_done", requestId: "tool-free-turn", ok: true, sessionId: "mock-session" });
const completed = await hostSendResponse;
assert.equal(completed.ok, true);
assert.equal(completed.sessionId, "mock-session");

console.log("ok  - background tool-free reasoning integration");
