/**
 * 轻量 Markdown 渲染（安全优先）：支持标题、列表、代码块、行内代码、粗体、链接。
 * 用于侧栏助手消息；不做完整 CommonMark。
 */

/**
 * HTML 转义。
 * @param {string} s
 */
export function escapeHtml(s) {
  return String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/**
 * 将 Markdown 转为可插入 DOM 的 HTML 字符串。
 * @param {string} md
 * @returns {string}
 */
export function renderMarkdown(md) {
  const src = String(md ?? "");
  const codeBlocks = [];
  let text = src.replace(/```([\w-]*)\n?([\s\S]*?)```/g, (_, lang, code) => {
    const i = codeBlocks.length;
    codeBlocks.push({ lang: lang || "", code: code.replace(/\n$/, "") });
    return `\u0000CODE${i}\u0000`;
  });

  text = escapeHtml(text);

  // Headings
  text = text.replace(/^######\s+(.+)$/gm, "<h6>$1</h6>");
  text = text.replace(/^#####\s+(.+)$/gm, "<h5>$1</h5>");
  text = text.replace(/^####\s+(.+)$/gm, "<h4>$1</h4>");
  text = text.replace(/^###\s+(.+)$/gm, "<h3>$1</h3>");
  text = text.replace(/^##\s+(.+)$/gm, "<h2>$1</h2>");
  text = text.replace(/^#\s+(.+)$/gm, "<h1>$1</h1>");

  // Lists
  text = text.replace(/^(?:[-*])\s+(.+)$/gm, "<li>$1</li>");
  text = text.replace(/(?:<li>.*<\/li>\n?)+/g, (block) => `<ul>${block}</ul>`);
  text = text.replace(/^\d+\.\s+(.+)$/gm, "<li>$1</li>");

  // Inline
  text = text.replace(/`([^`]+)`/g, "<code>$1</code>");
  text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  text = text.replace(/\[([^\]]+)\]\((https?:[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>');

  // Paragraphs: double newlines
  text = text
    .split(/\n{2,}/)
    .map((para) => {
      if (/^<(h[1-6]|ul|ol|pre|blockquote)/.test(para.trim())) return para;
      if (para.includes("\u0000CODE")) return para;
      return `<p>${para.replace(/\n/g, "<br>")}</p>`;
    })
    .join("");

  // Restore code blocks with copy affordance
  text = text.replace(/\u0000CODE(\d+)\u0000/g, (_, idx) => {
    const block = codeBlocks[Number(idx)] || { lang: "", code: "" };
    const lang = escapeHtml(block.lang);
    const code = escapeHtml(block.code);
    return `<div class="code-block" data-copy-source="1"><div class="code-toolbar"><span class="code-lang">${lang}</span><button type="button" class="btn-copy-code" data-copy="1">复制</button></div><pre><code>${code}</code></pre></div>`;
  });

  return text;
}

/**
 * 将 HTML 容器内的复制按钮绑定到剪贴板。
 * @param {ParentNode} root
 */
export function bindCopyButtons(root) {
  if (!root) return;
  root.querySelectorAll("[data-copy]").forEach((btn) => {
    if (btn.dataset.bound === "1") return;
    btn.dataset.bound = "1";
    btn.addEventListener("click", async () => {
      const block = btn.closest("[data-copy-source]");
      const code = block?.querySelector("code")?.textContent || block?.textContent || "";
      try {
        await navigator.clipboard.writeText(code);
        const prev = btn.textContent;
        btn.textContent = "已复制";
        setTimeout(() => {
          btn.textContent = prev || "复制";
        }, 1200);
      } catch {
        btn.textContent = "失败";
      }
    });
  });
}
