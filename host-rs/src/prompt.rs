use crate::protocol::BrowserContext;

/// 把只读页面快照编成 system 附加文本。图片本身走多模态 content，不在这里展开 data URL。
pub fn assemble_browser_context(ctx: &BrowserContext, max_page_chars: usize) -> String {
    let max_page = if max_page_chars == 0 {
        16000
    } else {
        max_page_chars
    };
    let mentions = ctx.mentions.clone().unwrap_or_default();
    let tabs = ctx.tabs.clone().unwrap_or_default();
    let images = ctx.images.clone().unwrap_or_default();
    let has_mentions = !mentions.is_empty();
    let include_tabs = ctx.include_tabs.unwrap_or(false) && !tabs.is_empty();
    let has_browser = nonempty(&ctx.title)
        || nonempty(&ctx.url)
        || nonempty(&ctx.selection)
        || nonempty(&ctx.page_text)
        || include_tabs
        || has_mentions
        || nonempty(&ctx.local_path_hint)
        || !images.is_empty();
    if !has_browser {
        return String::new();
    }

    let mut b = String::from("[Browser context]\n");
    if let Some(t) = trim_opt(&ctx.title) {
        b.push_str(&format!("Title: {t}\n"));
    }
    if let Some(u) = trim_opt(&ctx.url) {
        b.push_str(&format!("URL: {u}\n"));
    }
    if let Some(s) = trim_opt(&ctx.selection) {
        b.push_str("Selection:\n");
        b.push_str(s);
        b.push('\n');
    }
    if let Some(mut p) = trim_opt(&ctx.page_text).map(|s| s.to_string()) {
        if p.chars().count() > max_page {
            p = p.chars().take(max_page).collect::<String>() + "\n…[truncated]";
        }
        b.push_str("Page extract:\n");
        b.push_str(&p);
        b.push('\n');
    }
    if let Some(hint) = trim_opt(&ctx.local_path_hint) {
        b.push_str(&format!(
            "Local path hint (localhost debug only; not a bound workspace): {hint}\n"
        ));
    }
    if !images.is_empty() {
        b.push_str("Attached images:\n");
        for img in &images {
            let src = trim_opt(&img.src).unwrap_or("(inline)");
            let alt = trim_opt(&img.alt).unwrap_or("");
            if alt.is_empty() {
                b.push_str(&format!("- {src}\n"));
            } else {
                b.push_str(&format!("- {alt} | {src}\n"));
            }
        }
    }
    if has_mentions {
        b.push_str("Attached mentions:\n");
        for m in &mentions {
            let kind = trim_opt(&m.kind).unwrap_or("item");
            let title = trim_opt(&m.title).unwrap_or("");
            let url = trim_opt(&m.url).unwrap_or("");
            match (title.is_empty(), url.is_empty()) {
                (false, false) => b.push_str(&format!("- @{kind} {title} | {url}\n")),
                (false, true) => b.push_str(&format!("- @{kind} {title}\n")),
                (true, false) => b.push_str(&format!("- @{kind} {url}\n")),
                (true, true) => b.push_str(&format!("- @{kind}\n")),
            }
            if let Some(mut text) = trim_opt(&m.text).map(|s| s.to_string()) {
                if text.chars().count() > max_page {
                    text = text.chars().take(max_page).collect::<String>() + "\n…[truncated]";
                }
                b.push_str(&text);
                b.push('\n');
            }
        }
    }
    if include_tabs {
        b.push_str("Open tabs:\n");
        for tab in &tabs {
            let title = trim_opt(&tab.title).unwrap_or("(untitled)");
            let url = trim_opt(&tab.url).unwrap_or("");
            if title == "(untitled)" && url.is_empty() {
                continue;
            }
            b.push_str(&format!("- {title} | {url}\n"));
        }
    }
    b.push('\n');
    b
}

pub fn assemble_prompt(ctx: &BrowserContext, user_text: &str, max_page_chars: usize) -> String {
    let mut b = assemble_browser_context(ctx, max_page_chars);
    b.push_str("[User]\n");
    b.push_str(user_text.trim());
    if !user_text.ends_with('\n') {
        b.push('\n');
    }
    b
}

pub fn resolve_turn_cwd(explicit: &str, base: &str) -> String {
    let cwd = explicit.trim();
    if cwd.is_empty() || looks_like_url(cwd) {
        return String::new();
    }
    let path = std::path::Path::new(cwd);
    if path.is_absolute() {
        return path.clean_like();
    }
    if base.trim().is_empty() {
        return path.clean_like();
    }
    std::path::Path::new(base).join(path).clean_like()
}

pub fn local_path_hint_from_url(page_url: &str) -> String {
    let Ok(u) = url::Url::parse(page_url.trim()) else {
        return String::new();
    };
    let host = u.host_str().unwrap_or("").to_ascii_lowercase();
    if host != "localhost" && host != "127.0.0.1" && host != "::1" {
        return String::new();
    }
    let p = u.path();
    if p.is_empty() || p == "/" {
        String::new()
    } else {
        p.to_string()
    }
}

fn looks_like_url(s: &str) -> bool {
    let low = s.trim().to_ascii_lowercase();
    [
        "http://",
        "https://",
        "file://",
        "chrome://",
        "chrome-extension://",
    ]
    .iter()
    .any(|p| low.starts_with(p))
}

fn nonempty(v: &Option<String>) -> bool {
    v.as_deref().map(|s| !s.trim().is_empty()).unwrap_or(false)
}

fn trim_opt(v: &Option<String>) -> Option<&str> {
    v.as_deref().map(str::trim).filter(|s| !s.is_empty())
}

trait CleanLike {
    fn clean_like(&self) -> String;
}

impl CleanLike for std::path::Path {
    fn clean_like(&self) -> String {
        self.components()
            .collect::<std::path::PathBuf>()
            .to_string_lossy()
            .into_owned()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_context() {
        assert!(assemble_browser_context(&BrowserContext::default(), 100).is_empty());
    }

    #[test]
    fn cwd_rejects_url() {
        assert_eq!(resolve_turn_cwd("https://example.com/x", ""), "");
        assert_eq!(resolve_turn_cwd("", "/tmp"), "");
    }

    #[test]
    fn localhost_hint() {
        assert_eq!(
            local_path_hint_from_url("http://localhost:5173/src/App.tsx"),
            "/src/App.tsx"
        );
        assert_eq!(local_path_hint_from_url("https://example.com/x"), "");
    }
}
