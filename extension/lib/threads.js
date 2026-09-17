/**
 * 会话线程模型（纯逻辑）：列表/新建/标题推断。
 * 持久化由调用方通过 chrome.storage 完成。
 */

/**
 * @typedef {{ id: string, title: string, sessionId: string, createdAt: number, updatedAt: number, preview?: string }} Thread
 */

/**
 * @returns {Thread}
 */
export function createThread(partial = {}) {
  const now = Date.now();
  return {
    id: partial.id || `th-${now}-${Math.random().toString(36).slice(2, 8)}`,
    title: partial.title || "新对话",
    sessionId: partial.sessionId || "",
    createdAt: partial.createdAt || now,
    updatedAt: partial.updatedAt || now,
    preview: partial.preview || "",
  };
}

/**
 * @param {Thread[]} threads
 * @param {string} id
 * @returns {Thread[]}
 */
export function removeThread(threads, id) {
  return (threads || []).filter((t) => t.id !== id);
}

/**
 * 按更新时间倒序。
 * @param {Thread[]} threads
 */
export function sortThreads(threads) {
  return [...(threads || [])].sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0));
}

/**
 * 用首条用户消息生成短标题。
 * @param {string} text
 */
export function titleFromUserText(text) {
  const t = String(text || "").replace(/\s+/g, " ").trim();
  if (!t) return "新对话";
  return t.length > 40 ? `${t.slice(0, 40)}…` : t;
}

/**
 * 合并更新线程。
 * @param {Thread[]} threads
 * @param {string} id
 * @param {Partial<Thread>} patch
 */
export function updateThread(threads, id, patch) {
  return (threads || []).map((t) => {
    if (t.id !== id) return t;
    return { ...t, ...patch, updatedAt: Date.now() };
  });
}
