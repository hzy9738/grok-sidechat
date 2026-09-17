package main

import (
	"fmt"
	"strings"
)

// TabInfo describes an open browser tab identity.
type TabInfo struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	ID    int    `json:"id,omitempty"`
}

// ContextMention is an @-style chip attached by the side panel (tab/page/selection/…).
type ContextMention struct {
	Kind  string `json:"kind"` // tab | page | selection | pageText | localPath
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
	Text  string `json:"text,omitempty"`
	TabID int    `json:"tabId,omitempty"`
}

// BrowserContext is page/tab/selection context collected by the extension.
// It is never used as an automatic project --cwd bind.
type BrowserContext struct {
	Title         string           `json:"title"`
	URL           string           `json:"url"`
	Selection     string           `json:"selection"`
	PageText      string           `json:"pageText"`
	Tabs          []TabInfo        `json:"tabs"`
	IncludeTabs   bool             `json:"includeTabs"`
	Mentions      []ContextMention `json:"mentions,omitempty"`
	LocalPathHint string           `json:"localPathHint,omitempty"` // localhost debug path only
	// Kept for wire compatibility with older extension builds. The host ignores it.
	EnableBrowserControl bool `json:"enableBrowserControl,omitempty"`
}

// AssembleBrowserContext 只组装只读页面快照，供通用 Chat API 的 system 消息使用。
func AssembleBrowserContext(ctx BrowserContext, maxPageChars int) string {
	if maxPageChars <= 0 {
		maxPageChars = 16000
	}

	var b strings.Builder
	hasMentions := len(ctx.Mentions) > 0
	hasBrowser := strings.TrimSpace(ctx.Title) != "" ||
		strings.TrimSpace(ctx.URL) != "" ||
		strings.TrimSpace(ctx.Selection) != "" ||
		strings.TrimSpace(ctx.PageText) != "" ||
		(ctx.IncludeTabs && len(ctx.Tabs) > 0) ||
		hasMentions ||
		strings.TrimSpace(ctx.LocalPathHint) != ""

	if !hasBrowser {
		return ""
	}

	b.WriteString("[Browser context]\n")
		if t := strings.TrimSpace(ctx.Title); t != "" {
			fmt.Fprintf(&b, "Title: %s\n", t)
		}
		if u := strings.TrimSpace(ctx.URL); u != "" {
			fmt.Fprintf(&b, "URL: %s\n", u)
		}
		if s := strings.TrimSpace(ctx.Selection); s != "" {
			b.WriteString("Selection:\n")
			b.WriteString(s)
			b.WriteString("\n")
		}
		if p := strings.TrimSpace(ctx.PageText); p != "" {
			if len(p) > maxPageChars {
				p = p[:maxPageChars] + "\n…[truncated]"
			}
			b.WriteString("Page extract:\n")
			b.WriteString(p)
			b.WriteString("\n")
		}
		if hint := strings.TrimSpace(ctx.LocalPathHint); hint != "" {
			fmt.Fprintf(&b, "Local path hint (localhost debug only; not a bound workspace): %s\n", hint)
		}
		if hasMentions {
			b.WriteString("Attached mentions:\n")
			for _, m := range ctx.Mentions {
				kind := strings.TrimSpace(m.Kind)
				if kind == "" {
					kind = "item"
				}
				title := strings.TrimSpace(m.Title)
				url := strings.TrimSpace(m.URL)
				text := strings.TrimSpace(m.Text)
				switch {
				case title != "" && url != "":
					fmt.Fprintf(&b, "- @%s %s | %s\n", kind, title, url)
				case title != "":
					fmt.Fprintf(&b, "- @%s %s\n", kind, title)
				case url != "":
					fmt.Fprintf(&b, "- @%s %s\n", kind, url)
				default:
					fmt.Fprintf(&b, "- @%s\n", kind)
				}
				if text != "" {
					// Keep mention payloads bounded.
					if len(text) > maxPageChars {
						text = text[:maxPageChars] + "\n…[truncated]"
					}
					b.WriteString(text)
					b.WriteString("\n")
				}
			}
		}
		if ctx.IncludeTabs && len(ctx.Tabs) > 0 {
			b.WriteString("Open tabs:\n")
			for _, tab := range ctx.Tabs {
				title := strings.TrimSpace(tab.Title)
				url := strings.TrimSpace(tab.URL)
				if title == "" && url == "" {
					continue
				}
				if title == "" {
					title = "(untitled)"
				}
				fmt.Fprintf(&b, "- %s | %s\n", title, url)
			}
		}
	b.WriteString("\n")
	return b.String()
}

// AssemblePrompt 保留完整可观测文本：页面快照 + 用户原文。
func AssemblePrompt(ctx BrowserContext, userText string, maxPageChars int) string {
	var b strings.Builder
	if page := AssembleBrowserContext(ctx, maxPageChars); page != "" {
		b.WriteString(page)
	}
	b.WriteString("[User]\n")
	b.WriteString(strings.TrimSpace(userText))
	if !strings.HasSuffix(userText, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}
