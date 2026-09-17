/**
 * 页面脚本：抽取上下文 + 划词浮层与蓝白右键菜单。
 * 不直接访问公司 API。
 */

function getSelectionText() {
  try {
    return String(window.getSelection?.()?.toString?.() || "").trim();
  } catch {
    return "";
  }
}

function getPageText(limit = 20000) {
  try {
    const root =
      document.querySelector("article") ||
      document.querySelector("main") ||
      document.body;
    if (!root) return "";
    return (root.innerText || root.textContent || "")
      .replace(/\s+\n/g, "\n")
      .replace(/\n{3,}/g, "\n\n")
      .trim()
      .slice(0, limit);
  } catch {
    return "";
  }
}

function clickSelector(selector) {
  const el = document.querySelector(selector);
  if (!el) return { ok: false, error: `not found: ${selector}` };
  el.scrollIntoView({ block: "center", inline: "center" });
  el.click();
  return { ok: true, selector, tag: el.tagName };
}

function fillSelector(selector, value) {
  const el = document.querySelector(selector);
  if (!el) return { ok: false, error: `not found: ${selector}` };
  el.focus();
  if ("value" in el) {
    el.value = value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return { ok: true, selector, valueLength: String(value).length };
  }
  if (el.isContentEditable) {
    el.textContent = value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    return { ok: true, selector, valueLength: String(value).length };
  }
  return { ok: false, error: "element not fillable" };
}

function pressKey(selector, key) {
  const el = selector ? document.querySelector(selector) : document.activeElement || document.body;
  if (!el) return { ok: false, error: "no target" };
  el.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
  el.dispatchEvent(new KeyboardEvent("keyup", { key, bubbles: true, cancelable: true }));
  return { ok: true, key, selector: selector || null };
}

function isEditable(el) {
  if (!el || !(el instanceof Element)) return false;
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  return el.isContentEditable;
}

function selectionRect() {
  const sel = window.getSelection?.();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const range = sel.getRangeAt(0);
  const rect = range.getBoundingClientRect();
  if (!rect || (rect.width === 0 && rect.height === 0)) return null;
  return rect;
}

const BRAND_BLUE = "#1E50E6";
const BRAND_CYAN = "#50C8FF";

let askRoot = null;
let askBtn = null;
let menuEl = null;
let menuCtx = null;

function sendAsk(payload) {
  chrome.runtime.sendMessage({ type: "ask-from-page", pageUrl: location.href, ...payload }).catch(() => {});
}

function closestFromEvent(e, selector) {
  const path = typeof e.composedPath === "function" ? e.composedPath() : [];
  for (const node of path) {
    if (node instanceof Element && node.matches(selector)) return node;
  }
  return e.target instanceof Element ? e.target.closest(selector) : null;
}

function ensureAskUi() {
  if (askRoot) return;
  askRoot = document.createElement("div");
  askRoot.id = "chint-sidechat-root";
  const shadow = askRoot.attachShadow({ mode: "open" });
  const style = document.createElement("style");
  style.textContent = `
    :host { all: initial; }
    .chip, .menu {
      position: fixed;
      z-index: 2147483646;
      font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Segoe UI", sans-serif;
    }
    .chip {
      display: none;
      align-items: center;
      gap: 6px;
      border: 1px solid ${BRAND_BLUE};
      border-radius: 999px;
      padding: 6px 12px 6px 8px;
      color: ${BRAND_BLUE};
      background: #fff;
      box-shadow: 0 6px 20px rgba(30, 80, 230, 0.18);
      cursor: pointer;
      font-size: 12px;
      font-weight: 620;
      line-height: 1.2;
    }
    .chip:hover { background: #e8eefc; }
    .chip-mark {
      width: 18px;
      height: 18px;
      flex: 0 0 auto;
    }
    .menu {
      display: none;
      width: 196px;
      padding: 6px;
      border: 1px solid rgba(30, 80, 230, 0.16);
      border-radius: 14px;
      background: #fff;
      box-shadow: 0 12px 32px rgba(30, 80, 230, 0.16);
      color: #1a1a1a;
    }
    .menu button {
      display: block;
      width: 100%;
      border: 0;
      border-radius: 9px;
      padding: 9px 10px;
      background: transparent;
      color: #1a1a1a;
      text-align: left;
      font: 13px/1.3 inherit;
      cursor: pointer;
    }
    .menu button:hover { background: #e8eefc; color: ${BRAND_BLUE}; }
    .menu .sep {
      height: 1px;
      margin: 4px 6px;
      background: rgba(30, 80, 230, 0.1);
    }
  `;
  askBtn = document.createElement("button");
  askBtn.type = "button";
  askBtn.className = "chip";
  askBtn.innerHTML = `${markSvg("chip-mark")}<span>询问</span>`;
  askBtn.addEventListener("mousedown", (e) => e.preventDefault());
  askBtn.addEventListener("click", () => {
    const text = getSelectionText();
    hideAsk();
    hideMenu();
    if (!text) return;
    sendAsk({ kind: "selection", text });
  });

  menuEl = document.createElement("div");
  menuEl.className = "menu";
  menuEl.addEventListener("mousedown", (e) => e.preventDefault());
  menuEl.addEventListener("click", (e) => {
    const btn = e.target instanceof Element ? e.target.closest("button[data-act]") : null;
    if (!btn) return;
    runMenuAction(btn.getAttribute("data-act"));
  });

  shadow.append(style, askBtn, menuEl);
  document.documentElement.appendChild(askRoot);
}

function markSvg(className) {
  return `<svg class="${className}" viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><rect width="32" height="32" rx="8" fill="${BRAND_BLUE}"/><path d="M8 12.5A2.5 2.5 0 0 1 10.5 10h11A2.5 2.5 0 0 1 24 12.5v7a2.5 2.5 0 0 1-2.5 2.5H16l-3.2 2.4a.8.8 0 0 1-1.3-.6V22H10.5A2.5 2.5 0 0 1 8 19.5v-7Z" fill="#fff"/><circle cx="24.5" cy="8.5" r="3.2" fill="${BRAND_CYAN}"/></svg>`;
}

function showAsk(rect) {
  ensureAskUi();
  hideMenu();
  askBtn.style.top = `${Math.max(8, rect.top - 40)}px`;
  askBtn.style.left = `${Math.max(8, Math.min(window.innerWidth - 96, rect.left + rect.width / 2 - 36))}px`;
  askBtn.style.display = "inline-flex";
}

function hideAsk() {
  if (askBtn) askBtn.style.display = "none";
}

/** 页面右键改用蓝白菜单；输入框仍走浏览器原生菜单，避免挡复制粘贴。 */
function collectMenuContext(e) {
  const img = closestFromEvent(e, "img");
  const link = closestFromEvent(e, "a[href]");
  return {
    text: getSelectionText(),
    imageSrc: img instanceof HTMLImageElement ? img.currentSrc || img.src || "" : "",
    imageAlt: img instanceof HTMLImageElement ? img.alt || "" : "",
    linkUrl: link instanceof HTMLAnchorElement ? link.href || "" : "",
  };
}

function renderMenu(ctx) {
  const rows = [];
  if (ctx.text) rows.push(["selection", "询问选中文字"]);
  if (ctx.imageSrc) rows.push(["image", "询问这张图片"]);
  if (ctx.linkUrl) rows.push(["link", "询问此链接"]);
  rows.push(["page", "询问此页"]);
  rows.push(["open", "打开侧栏"]);
  const extras = [];
  if (ctx.text) extras.push(["copy", "复制选中文字"]);
  if (ctx.linkUrl) extras.push(["open-link", "打开链接"]);
  menuEl.innerHTML = `${rows
    .map(([act, label]) => `<button type="button" data-act="${act}">${label}</button>`)
    .join("")}${extras.length ? `<div class="sep"></div>${extras
    .map(([act, label]) => `<button type="button" data-act="${act}">${label}</button>`)
    .join("")}` : ""}`;
}

function showMenu(x, y, ctx) {
  ensureAskUi();
  hideAsk();
  menuCtx = ctx;
  renderMenu(ctx);
  menuEl.style.display = "block";
  const w = menuEl.offsetWidth || 196;
  const h = menuEl.offsetHeight || 220;
  menuEl.style.left = `${Math.max(8, Math.min(x, window.innerWidth - w - 8))}px`;
  menuEl.style.top = `${Math.max(8, Math.min(y, window.innerHeight - h - 8))}px`;
}

function hideMenu() {
  if (menuEl) menuEl.style.display = "none";
  menuCtx = null;
}

function runMenuAction(act) {
  const ctx = menuCtx || { text: getSelectionText(), imageSrc: "", imageAlt: "", linkUrl: "" };
  hideMenu();
  if (act === "selection") {
    if (ctx.text) sendAsk({ kind: "selection", text: ctx.text });
    return;
  }
  if (act === "image") {
    sendAsk({
      kind: "image",
      text: "这张图是什么？请结合当前页说明。",
      image: { src: ctx.imageSrc, alt: ctx.imageAlt },
    });
    return;
  }
  if (act === "link") {
    sendAsk({ kind: "link", text: ctx.linkUrl });
    return;
  }
  if (act === "page") {
    sendAsk({ kind: "page", text: `请总结并回答关于当前页的问题：${location.href}` });
    return;
  }
  if (act === "open") {
    chrome.runtime.sendMessage({ type: "open-panel" }).catch(() => {});
    return;
  }
  if (act === "copy" && ctx.text) {
    navigator.clipboard?.writeText(ctx.text).catch(() => {});
    return;
  }
  if (act === "open-link" && ctx.linkUrl) {
    window.open(ctx.linkUrl, "_blank", "noopener,noreferrer");
  }
}

document.addEventListener("mouseup", (e) => {
  if (askRoot?.contains(e.target) || askBtn?.contains?.(e.target)) return;
  if (e.button !== 0) return;
  if (isEditable(document.activeElement) || isEditable(e.target)) {
    hideAsk();
    return;
  }
  const text = getSelectionText();
  const rect = selectionRect();
  if (!text || text.length < 2 || !rect) {
    hideAsk();
    return;
  }
  showAsk(rect);
});

document.addEventListener(
  "contextmenu",
  (e) => {
    if (askRoot?.contains(e.target)) return;
    if (isEditable(document.activeElement) || isEditable(e.target)) return;
    if (e.shiftKey) return;
    e.preventDefault();
    showMenu(e.clientX, e.clientY, collectMenuContext(e));
  },
  true
);

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    hideAsk();
    hideMenu();
  }
});
document.addEventListener("scroll", () => {
  hideAsk();
  hideMenu();
}, true);
document.addEventListener("mousedown", (e) => {
  if (askRoot?.contains(e.target)) return;
  if (e.button === 2) return;
  hideMenu();
});

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg?.type === "extract-context") {
    sendResponse({
      title: document.title || "",
      url: location.href,
      selection: getSelectionText(),
      pageText: getPageText(msg.limit || 20000),
    });
    return true;
  }
  if (msg?.type === "browser-page-action") {
    const action = msg.action;
    try {
      if (action === "click") sendResponse(clickSelector(msg.selector));
      else if (action === "fill") sendResponse(fillSelector(msg.selector, msg.value ?? ""));
      else if (action === "press") sendResponse(pressKey(msg.selector || null, msg.key || "Enter"));
      else if (action === "read") {
        sendResponse({
          ok: true,
          title: document.title || "",
          url: location.href,
          selection: getSelectionText(),
          pageText: getPageText(msg.limit || 20000),
        });
      } else sendResponse({ ok: false, error: `unknown page action: ${action}` });
    } catch (e) {
      sendResponse({ ok: false, error: String(e) });
    }
    return true;
  }
  return false;
});
