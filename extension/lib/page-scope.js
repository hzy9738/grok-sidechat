export const PAGE_SCOPE_STORE_KEY = "pageScopeSessionsV1";
/** 新会话 / 空作用域的默认标题，不放品牌四字。 */
export const DEFAULT_THREAD_TITLE = "新聊天";

const MAX_SESSIONS_PER_SCOPE = 50;

function normalizedBoundary(url) {
  try {
    const parsed = new URL(String(url || ""));
    if (parsed.origin && parsed.origin !== "null") {
      return `origin:${parsed.origin.toLowerCase()}`;
    }
    parsed.hash = "";
    return `page:${parsed.href}`;
  } catch {
    return `page:${String(url || "unknown")}`;
  }
}

/**
 * A conversation belongs to one Chrome tab and one web origin.
 * Opaque/restricted URLs use the full page URL because they have no origin.
 */
export function derivePageScope(tabId, url) {
  const id = Number(tabId);
  if (!Number.isInteger(id) || id < 0) return "";
  return `v1|tab:${id}|${normalizedBoundary(url)}`;
}

export function emptyScopeRecord() {
  return { activeSessionId: "", title: DEFAULT_THREAD_TITLE, sessionIds: [], updatedAt: 0 };
}

export function readScopeRecord(store, scopeKey) {
  const raw = store && typeof store === "object" ? store[scopeKey] : null;
  if (!raw || typeof raw !== "object") return emptyScopeRecord();
  const sessionIds = Array.isArray(raw.sessionIds)
    ? [...new Set(raw.sessionIds.filter((id) => typeof id === "string" && id))].slice(
        -MAX_SESSIONS_PER_SCOPE
      )
    : [];
  const activeSessionId =
    typeof raw.activeSessionId === "string" ? raw.activeSessionId : "";
  if (activeSessionId && !sessionIds.includes(activeSessionId)) sessionIds.push(activeSessionId);
  return {
    activeSessionId,
    title: typeof raw.title === "string" && raw.title ? raw.title : DEFAULT_THREAD_TITLE,
    sessionIds: sessionIds.slice(-MAX_SESSIONS_PER_SCOPE),
    updatedAt: Number(raw.updatedAt) || 0,
  };
}

export function updateScopeRecord(store, scopeKey, patch = {}) {
  if (!scopeKey) return store && typeof store === "object" ? { ...store } : {};
  const nextStore = store && typeof store === "object" ? { ...store } : {};
  const current = readScopeRecord(nextStore, scopeKey);
  const next = {
    ...current,
    ...patch,
    sessionIds: current.sessionIds.slice(),
    updatedAt: Date.now(),
  };
  if (Array.isArray(patch.sessionIds)) {
    next.sessionIds = [...new Set(patch.sessionIds.filter((id) => typeof id === "string" && id))];
  }
  if (typeof next.activeSessionId !== "string") next.activeSessionId = "";
  if (next.activeSessionId && !next.sessionIds.includes(next.activeSessionId)) {
    next.sessionIds.push(next.activeSessionId);
  }
  next.sessionIds = next.sessionIds.slice(-MAX_SESSIONS_PER_SCOPE);
  if (typeof next.title !== "string" || !next.title) next.title = DEFAULT_THREAD_TITLE;
  nextStore[scopeKey] = next;
  return nextStore;
}

export function removeScopesForTab(store, tabId) {
  const next = store && typeof store === "object" ? { ...store } : {};
  const prefix = `v1|tab:${Number(tabId)}|`;
  for (const key of Object.keys(next)) {
    if (key.startsWith(prefix)) delete next[key];
  }
  return next;
}

export function filterSessionsForScope(sessions, record) {
  const allowed = new Set(readScopeRecord({ current: record }, "current").sessionIds);
  return (Array.isArray(sessions) ? sessions : []).filter((session) => allowed.has(session?.id));
}
