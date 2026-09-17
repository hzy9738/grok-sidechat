/**
 * Pure-helper tests for extension lib (real shipped modules).
 * Run: node extension/test/run-tests.mjs
 */
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const lib = path.join(__dirname, "..", "lib");

const context = await import(pathToFileURL(path.join(lib, "context.js")).href);
const events = await import(pathToFileURL(path.join(lib, "events.js")).href);
const threads = await import(pathToFileURL(path.join(lib, "threads.js")).href);
const markdown = await import(pathToFileURL(path.join(lib, "markdown.js")).href);
const hostBridge = await import(pathToFileURL(path.join(lib, "host-bridge.js")).href);
const pageScope = await import(pathToFileURL(path.join(lib, "page-scope.js")).href);

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log(`ok  - ${name}`);
  } catch (e) {
    failed++;
    console.error(`fail - ${name}`);
    console.error(e);
  }
}

test("parseMentionTokens finds page/selection/tabs", () => {
  const { kinds } = context.parseMentionTokens("look at @page and @selection please @tabs");
  assert.ok(kinds.has("page"));
  assert.ok(kinds.has("selection"));
  assert.ok(kinds.has("tabs"));
});

test("packBrowserContext keeps selected images for vision turns", () => {
  const packed = context.packBrowserContext({
    page: {
      title: "Example",
      url: "https://example.com/docs",
    },
    inputText: "这张图是什么",
    chips: [
      {
        kind: "image",
        src: "https://example.com/a.png",
        dataUrl: "data:image/png;base64,xx",
        alt: "示意图",
      },
    ],
  });
  assert.equal(packed.images.length, 1);
  assert.equal(packed.images[0].src, "https://example.com/a.png");
  assert.ok(packed.mentions.some((m) => m.kind === "image"));
});

test("packBrowserContext keeps extracted PDF text as a document mention", () => {
  const packed = context.packBrowserContext({
    page: { title: "Example", url: "https://example.com" },
    inputText: "总结文档",
    chips: [{ kind: "document", title: "方案.pdf", text: "第一章 项目背景" }],
  });
  const document = packed.mentions.find((item) => item.kind === "document");
  assert.equal(document.title, "方案.pdf");
  assert.match(document.text, /项目背景/);
});

test("packBrowserContext contains page context but no tool toggle or cwd", () => {
  const packed = context.packBrowserContext({
    page: {
      title: "Example",
      url: "https://example.com/docs",
      selection: "hi",
      pageText: "body",
      tabs: [{ title: "Example", url: "https://example.com/docs", id: 1 }],
    },
    inputText: "summarize @page @selection",
    chips: [],
    enableBrowserControl: true,
  });
  assert.equal(packed.url, "https://example.com/docs");
  assert.ok(packed.mentions.some((m) => m.kind === "page"));
  assert.ok(packed.mentions.some((m) => m.kind === "selection"));
  assert.equal(Object.prototype.hasOwnProperty.call(packed, "enableBrowserControl"), false);
  // no cwd field on browser payload
  assert.equal(Object.prototype.hasOwnProperty.call(packed, "cwd"), false);
});

test("localPathHintFromURL only localhost", () => {
  assert.equal(context.localPathHintFromURL("https://example.com/x"), "");
  assert.equal(context.localPathHintFromURL("http://localhost:5173/src/App.tsx"), "/src/App.tsx");
});

test("page scope isolates tabs and cross-origin navigation", () => {
  const docsA = pageScope.derivePageScope(7, "https://example.com/docs/a");
  const docsB = pageScope.derivePageScope(7, "https://example.com/docs/b?x=1");
  assert.equal(docsA, docsB, "same tab and origin should keep the conversation");
  assert.notEqual(docsA, pageScope.derivePageScope(8, "https://example.com/docs/a"));
  assert.notEqual(docsA, pageScope.derivePageScope(7, "https://other.example/docs/a"));
  assert.notEqual(
    pageScope.derivePageScope(7, "file:///tmp/a.html"),
    pageScope.derivePageScope(7, "file:///tmp/b.html")
  );
});

test("scope records retain only sessions bound to that page scope", () => {
  const scopeA = pageScope.derivePageScope(1, "https://a.example/one");
  const scopeB = pageScope.derivePageScope(2, "https://a.example/one");
  let store = pageScope.updateScopeRecord({}, scopeA, {
    activeSessionId: "session-a",
    title: "A",
  });
  store = pageScope.updateScopeRecord(store, scopeB, { activeSessionId: "session-b" });
  assert.deepEqual(pageScope.readScopeRecord(store, scopeA).sessionIds, ["session-a"]);
  assert.deepEqual(
    pageScope.filterSessionsForScope(
      [{ id: "session-a" }, { id: "session-b" }],
      pageScope.readScopeRecord(store, scopeA)
    ).map((session) => session.id),
    ["session-a"]
  );
  store = pageScope.removeScopesForTab(store, 1);
  assert.equal(pageScope.readScopeRecord(store, scopeA).activeSessionId, "");
  assert.equal(pageScope.readScopeRecord(store, scopeB).activeSessionId, "session-b");
});

test("mapHostEventToUI maps thinking/text and hides tools", () => {
  const t = events.mapHostEventToUI({ type: "thinking", text: "..." });
  assert.equal(t[0].kind, "thinking");
  const a = events.mapHostEventToUI({ type: "partial", text: "hello" });
  assert.equal(a[0].kind, "assistant");
  assert.equal(a[0].streaming, true);
  const tool = events.mapHostEventToUI({
    type: "tool_use",
    name: "browser_click",
    input: '{"selector":"#go"}',
  });
  assert.equal(tool[0].kind, "hidden");
});

test("mapHostEventToUI hides host.argv and done noise", () => {
  const argv = events.mapHostEventToUI({
    type: "partial",
    text: "argv: [...]",
    rawType: "host.argv",
  });
  assert.equal(argv[0].kind, "hidden");
  const done = events.mapHostEventToUI({ type: "done" });
  assert.equal(done[0].kind, "hidden");
});

test("sanitizeToolText strips binary-ish noise", () => {
  const bin = "\x00\x01\x02" + "x".repeat(50);
  const s = events.sanitizeToolText(bin);
  assert.match(s, /binary|omitted/i);
});

test("exit status tool results are hidden noise", () => {
  assert.equal(events.isNoiseToolResult("exit status 1"), true);
  const mapped = events.mapHostEventToUI({ type: "tool_result", text: "exit status 1" });
  assert.equal(mapped[0].kind, "hidden");
});

test("threads create/sort/title", () => {
  const a = threads.createThread({ title: "A", updatedAt: 1 });
  const b = threads.createThread({ title: "B", updatedAt: 2 });
  const sorted = threads.sortThreads([a, b]);
  assert.equal(sorted[0].title, "B");
  assert.ok(threads.titleFromUserText("hello world").includes("hello"));
});

test("renderMarkdown code block has copy button", () => {
  const html = markdown.renderMarkdown("```js\nconst x = 1\n```");
  assert.ok(html.includes("code-block"));
  assert.ok(html.includes("data-copy"));
  assert.ok(html.includes("const x = 1"));
});

test("stripMentionTokens", () => {
  const s = context.stripMentionTokens("@page hello @selection");
  assert.ok(s.includes("hello"));
  assert.ok(!s.includes("@page"));
});

test("normalizeHostPingResult: missing host / timeout not ok", () => {
  const missing = hostBridge.normalizeHostPingResult({
    lastError: "Specified native messaging host not found.",
    disconnected: true,
  });
  assert.equal(missing.ok, false);
  assert.match(missing.error, /not found/i);

  const timed = hostBridge.normalizeHostPingResult({ timedOut: true });
  assert.equal(timed.ok, false);
  assert.match(timed.error, /timed out/i);

  const ok = hostBridge.normalizeHostPingResult({ version: "0.1.0", msg: { op: "pong" } });
  assert.equal(ok.ok, true);
  assert.equal(ok.version, "0.1.0");
});

test("isNativeHostMissingError", () => {
  assert.equal(
    hostBridge.isNativeHostMissingError("Specified native messaging host not found."),
    true
  );
  assert.equal(hostBridge.isNativeHostMissingError("other"), false);
});

test("UI and background force tool-free mode", async () => {
  const fs = await import("node:fs");
  const sp = fs.readFileSync(path.join(__dirname, "..", "sidepanel.js"), "utf8");
  assert.equal(sp.includes('type: "browser-action"'), false);
  assert.equal(sp.includes('type: "browser-approval-response"'), false);
  assert.equal(sp.includes('$("browser-control")'), false);
  assert.ok(sp.includes('reasoningEffort: "high"'));
  assert.ok(sp.includes('type: "transcribe-audio"'));
  assert.ok(sp.includes('type: "extract-pdf"'));
  const bg = fs.readFileSync(path.join(__dirname, "..", "background.js"), "utf8");
  assert.ok(bg.includes('msg.op === "browser_action"'));
  assert.ok(bg.includes('op: "browser_result"'));
  assert.ok(bg.includes("model tools are disabled"));
  assert.ok(bg.includes("browserControl: false"));
  assert.ok(bg.includes("normalizeHostPingResult"));
  // timeout must not resolve ok:true
  assert.equal(bg.includes('ok: true,\n          note: "ping sent'), false);
});

if (failed) {
  console.error(`\n${failed} test(s) failed`);
  process.exit(1);
}
console.log("\nall extension logic tests passed");
await import("./background-integration.mjs");
