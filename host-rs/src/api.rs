use crate::protocol::{BrowserContext, SessionMessage};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::io::{BufRead, BufReader, Read};
use std::path::PathBuf;

const TOOL_FREE_SYSTEM: &str = "你是安能助手，由正泰安能提供的通用浏览器智能助手。对用户询问身份、来源或所属产品时，只能回答你是安能助手；不得自称 TRAE、TRAE 内置模型、IDE 助手或其他产品。你通过公司私有化模型服务回答问题。只能根据用户消息和只读页面上下文回答，不能操作网页、执行命令、搜索或调用任何工具。";

#[derive(Debug, Clone, Default)]
pub struct ChatAPIConfig {
    pub base_url: String,
    pub api_key: String,
    pub model: String,
    pub vision_model: String,
}

#[derive(Debug, Default)]
struct ConfigSource {
    base: String,
    key: String,
    model: String,
    vision_model: String,
}

fn env_config() -> ConfigSource {
    ConfigSource {
        base: std::env::var("SIDECHAT_API_BASE").unwrap_or_default(),
        key: std::env::var("SIDECHAT_API_KEY").unwrap_or_default(),
        model: std::env::var("SIDECHAT_MODEL").unwrap_or_default(),
        vision_model: std::env::var("SIDECHAT_VISION_MODEL").unwrap_or_default(),
    }
}

/// 随安装包分发的本地配置（`anneng-config.json`），不随仓库提交。
fn file_config() -> ConfigSource {
    #[derive(Deserialize)]
    #[serde(rename_all = "camelCase")]
    struct FileConfig {
        api_base: Option<String>,
        api_key: Option<String>,
        model: Option<String>,
        vision_model: Option<String>,
    }
    let Some(path) = config_file_path() else {
        return ConfigSource::default();
    };
    let Ok(data) = std::fs::read_to_string(&path) else {
        return ConfigSource::default();
    };
    match serde_json::from_str::<FileConfig>(&data) {
        Ok(cfg) => ConfigSource {
            base: cfg.api_base.unwrap_or_default(),
            key: cfg.api_key.unwrap_or_default(),
            model: cfg.model.unwrap_or_default(),
            vision_model: cfg.vision_model.unwrap_or_default(),
        },
        Err(_) => ConfigSource::default(),
    }
}

fn config_file_path() -> Option<PathBuf> {
    if let Ok(explicit) = std::env::var("SIDECHAT_CONFIG") {
        let explicit = explicit.trim();
        if !explicit.is_empty() {
            return Some(PathBuf::from(explicit));
        }
    }
    let exe = std::env::current_exe().ok()?;
    Some(exe.parent()?.join("anneng-config.json"))
}

/// 解析优先级：请求覆盖 > 环境变量 > 随包本地配置。地址与凭据同源，不交叉回退。
pub fn resolve_chat_api(
    req_base: &str,
    req_key: &str,
    req_model: &str,
    has_images: bool,
) -> ChatAPIConfig {
    resolve_from(
        req_base,
        req_key,
        req_model,
        &env_config(),
        &file_config(),
        has_images,
    )
}

fn resolve_from(
    req_base: &str,
    req_key: &str,
    req_model: &str,
    env: &ConfigSource,
    file: &ConfigSource,
    has_images: bool,
) -> ChatAPIConfig {
    let (base_url, api_key) = if !req_base.trim().is_empty() {
        (req_base.trim().to_string(), req_key.trim().to_string())
    } else if !env.base.trim().is_empty() {
        (env.base.trim().to_string(), env.key.trim().to_string())
    } else if !file.base.trim().is_empty() {
        (file.base.trim().to_string(), file.key.trim().to_string())
    } else {
        (String::new(), String::new())
    };
    let mut cfg = ChatAPIConfig {
        base_url,
        api_key,
        model: first_nonempty(&[req_model, &env.model, &file.model, "deepseek-v4"]),
        vision_model: first_nonempty(&[&env.vision_model, &file.vision_model, "qwen-vl"]),
    };
    if has_images && req_model.trim().is_empty() {
        cfg.model = cfg.vision_model.clone();
    }
    cfg
}

pub fn chat_completions_url(base: &str) -> Result<String, String> {
    let base = base.trim().trim_end_matches('/');
    if base.is_empty() {
        return Err("需要 API 地址（扩展设置、SIDECHAT_API_BASE 或随包 anneng-config.json）".into());
    }
    if base.ends_with("/chat/completions") {
        Ok(base.to_string())
    } else {
        Ok(format!("{base}/chat/completions"))
    }
}

#[derive(Debug, Serialize)]
struct ChatCompletionRequest {
    model: String,
    messages: Vec<Value>,
    stream: bool,
    thinking: Value,
}

/// 组装 Chat Completions messages；有图时 user content 用多模态数组。
pub fn build_chat_messages(
    browser: &BrowserContext,
    user_text: &str,
    history: &[SessionMessage],
    max_page_chars: usize,
) -> Vec<Value> {
    let mut sys = TOOL_FREE_SYSTEM.to_string();
    let page = crate::prompt::assemble_browser_context(browser, max_page_chars);
    if !page.is_empty() {
        sys.push_str("\n\n");
        sys.push_str(&page);
    }
    let mut out = vec![json!({"role":"system","content":sys})];
    for m in history {
        let role = m.role.trim();
        if role != "user" && role != "assistant" {
            continue;
        }
        if m.text.trim().is_empty() {
            continue;
        }
        out.push(json!({"role":role,"content":m.text}));
    }
    out.push(json!({
        "role":"user",
        "content": user_content(user_text, browser),
    }));
    out
}

fn user_content(user_text: &str, browser: &BrowserContext) -> Value {
    let images = browser.images.clone().unwrap_or_default();
    let urls: Vec<String> = images
        .iter()
        .filter_map(|img| {
            first_nonempty(&[
                img.data_url.as_deref().unwrap_or(""),
                img.src.as_deref().unwrap_or(""),
            ])
            .into()
        })
        .filter(|s| !s.is_empty())
        .collect();
    if urls.is_empty() {
        return Value::String(user_text.trim().to_string());
    }
    let mut parts = vec![json!({"type":"text","text":user_text.trim()})];
    for url in urls {
        if url.len() > 8 * 1024 * 1024 {
            continue;
        }
        parts.push(json!({"type":"image_url","image_url":{"url":url}}));
    }
    Value::Array(parts)
}

pub fn send_stream(
    cfg: &ChatAPIConfig,
    messages: Vec<Value>,
    cancel: &std::sync::atomic::AtomicBool,
    mut on_delta: impl FnMut(&str, &str),
) -> Result<(String, String), String> {
    let endpoint = chat_completions_url(&cfg.base_url)?;
    if cfg.model.trim().is_empty() {
        return Err("需要模型名".into());
    }
    let body = ChatCompletionRequest {
        model: cfg.model.clone(),
        messages,
        stream: true,
        thinking: json!({"type": "enabled"}),
    };
    // 读超时让阻塞的流式读定期返回，消费循环才能及时响应取消；空闲超时不视为错误。
    let agent = ureq::AgentBuilder::new()
        .timeout_connect(std::time::Duration::from_secs(15))
        .timeout_read(std::time::Duration::from_millis(500))
        .build();
    let mut req = agent
        .post(&endpoint)
        .set("Content-Type", "application/json")
        .set("Accept", "text/event-stream");
    if !cfg.api_key.is_empty() {
        req = req.set("Authorization", &format!("Bearer {}", cfg.api_key));
    }
    let resp = match req.send_json(serde_json::to_value(&body).map_err(|e| e.to_string())?) {
        Ok(resp) => resp,
        Err(ureq::Error::Status(status, resp)) => {
            let mut buf = String::new();
            let _ = resp.into_reader().take(4096).read_to_string(&mut buf);
            return Err(format_http_error(status, &buf));
        }
        Err(e) => return Err(format!("chat api: {e}")),
    };
    consume_openai_stream(resp.into_reader(), cancel, &mut on_delta)
}

fn format_http_error(status: u16, body: &str) -> String {
    let body = body.trim();
    if body.is_empty() {
        format!("模型服务返回 HTTP {status}")
    } else {
        format!("模型服务返回 HTTP {status}: {body}")
    }
}

#[derive(Debug, Deserialize)]
struct StreamChunk {
    error: Option<StreamError>,
    choices: Option<Vec<StreamChoice>>,
}

#[derive(Debug, Deserialize)]
struct StreamError {
    message: Option<String>,
}

#[derive(Debug, Deserialize)]
struct StreamChoice {
    delta: Option<StreamDelta>,
    message: Option<StreamDelta>,
}

#[derive(Debug, Deserialize)]
struct StreamDelta {
    content: Option<String>,
    reasoning_content: Option<String>,
    reasoning: Option<String>,
}

/// 解析一行 SSE，抽出 thinking / text。
pub fn parse_openai_stream_line(line: &str) -> Result<(String, String, bool), String> {
    let mut line = line.trim();
    if line.is_empty() || line.starts_with(':') {
        return Ok((String::new(), String::new(), false));
    }
    if let Some(rest) = line.strip_prefix("data:") {
        line = rest.trim();
    }
    if line.is_empty() {
        return Ok((String::new(), String::new(), false));
    }
    if line == "[DONE]" {
        return Ok((String::new(), String::new(), true));
    }
    let chunk: StreamChunk = match serde_json::from_str(line) {
        Ok(v) => v,
        Err(_) => return Ok((String::new(), String::new(), false)),
    };
    if let Some(err) = chunk
        .error
        .and_then(|e| e.message)
        .filter(|s| !s.trim().is_empty())
    {
        return Err(err);
    }
    let Some(choice) = chunk.choices.and_then(|c| c.into_iter().next()) else {
        return Ok((String::new(), String::new(), false));
    };
    let delta = choice.delta.or(choice.message);
    let Some(delta) = delta else {
        return Ok((String::new(), String::new(), false));
    };
    let thinking = first_nonempty(&[
        delta.reasoning_content.as_deref().unwrap_or(""),
        delta.reasoning.as_deref().unwrap_or(""),
    ]);
    let text = delta.content.unwrap_or_default();
    Ok((thinking, text, false))
}

fn consume_openai_stream(
    reader: impl Read,
    cancel: &std::sync::atomic::AtomicBool,
    on_delta: &mut impl FnMut(&str, &str),
) -> Result<(String, String), String> {
    use std::io::ErrorKind;
    let mut assistant = String::new();
    let mut thinking = String::new();
    let mut buf = BufReader::new(reader);
    let mut line = String::new();
    loop {
        if cancel.load(std::sync::atomic::Ordering::Relaxed) {
            break;
        }
        match buf.read_line(&mut line) {
            Ok(0) => {
                let tail = std::mem::take(&mut line);
                if !tail.trim().is_empty() {
                    let (th, tx, _) = parse_openai_stream_line(&tail)?;
                    thinking.push_str(&th);
                    assistant.push_str(&tx);
                    if !th.is_empty() {
                        on_delta("thinking", &th);
                    }
                    if !tx.is_empty() {
                        on_delta("partial", &tx);
                    }
                }
                break;
            }
            Ok(_) => {}
            Err(e) => match e.kind() {
                ErrorKind::WouldBlock | ErrorKind::TimedOut | ErrorKind::Interrupted => continue,
                _ => return Err(e.to_string()),
            },
        }
        let full = std::mem::take(&mut line);
        let (th, tx, done) = parse_openai_stream_line(&full)?;
        if !th.is_empty() {
            thinking.push_str(&th);
            on_delta("thinking", &th);
        }
        if !tx.is_empty() {
            assistant.push_str(&tx);
            on_delta("partial", &tx);
        }
        if done {
            break;
        }
    }
    Ok((assistant, thinking))
}

pub fn diagnostic_argv(cfg: &ChatAPIConfig, session_id: &str, cwd: &str) -> Vec<String> {
    let endpoint = chat_completions_url(&cfg.base_url).unwrap_or_else(|_| cfg.base_url.clone());
    let mut argv = vec![
        "openai-compat".into(),
        endpoint,
        "--model".into(),
        cfg.model.clone(),
    ];
    if !session_id.is_empty() {
        argv.push("--session".into());
        argv.push(session_id.into());
    }
    if !cwd.is_empty() {
        argv.push("--cwd".into());
        argv.push(cwd.into());
    }
    argv
}

fn first_nonempty(vals: &[&str]) -> String {
    vals.iter()
        .map(|s| s.trim())
        .find(|s| !s.is_empty())
        .unwrap_or("")
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn completions_url() {
        assert_eq!(
            chat_completions_url("https://ai-model.chint.com/api").unwrap(),
            "https://ai-model.chint.com/api/chat/completions"
        );
        assert_eq!(
            chat_completions_url("https://ai-model.chint.com/api/chat/completions").unwrap(),
            "https://ai-model.chint.com/api/chat/completions"
        );
    }

    #[test]
    fn parse_delta() {
        let (th, tx, done) =
            parse_openai_stream_line(r#"data: {"choices":[{"delta":{"content":"你好"}}]}"#)
                .unwrap();
        assert_eq!(tx, "你好");
        assert!(th.is_empty() && !done);
        let (th, _, _) =
            parse_openai_stream_line(r#"data: {"choices":[{"delta":{"reasoning_content":"想"}}]}"#)
                .unwrap();
        assert_eq!(th, "想");
        let (_, _, done) = parse_openai_stream_line("data: [DONE]").unwrap();
        assert!(done);
    }

    #[test]
    fn resolve_binds_base_and_key_to_same_source() {
        let env = ConfigSource {
            base: "https://env.example/api".into(),
            key: "env-key".into(),
            model: "env-model".into(),
            vision_model: "env-vision".into(),
        };
        let file = ConfigSource {
            base: "https://file.example/api".into(),
            key: "file-key".into(),
            model: "file-model".into(),
            vision_model: "file-vision".into(),
        };
        // 请求优先，地址、凭据、模型全部来自请求。
        let cfg = resolve_from(
            "https://req.example/api",
            "req-key",
            "req-model",
            &env,
            &file,
            false,
        );
        assert_eq!(cfg.base_url, "https://req.example/api");
        assert_eq!(cfg.api_key, "req-key");
        assert_eq!(cfg.model, "req-model");
        // 自填地址但凭据留空：不得把环境变量或本地配置里的密钥发给该地址。
        let cfg = resolve_from("https://other.example/api", "", "", &env, &file, false);
        assert_eq!(cfg.base_url, "https://other.example/api");
        assert_eq!(cfg.api_key, "");
        assert_eq!(cfg.model, "env-model");
        // 请求未填地址：整体使用环境变量这一对。
        let cfg = resolve_from("", "", "", &env, &file, false);
        assert_eq!(cfg.base_url, "https://env.example/api");
        assert_eq!(cfg.api_key, "env-key");
        // 请求、环境变量都没有：回落到随包本地配置这一对。
        let cfg = resolve_from("", "", "", &ConfigSource::default(), &file, false);
        assert_eq!(cfg.base_url, "https://file.example/api");
        assert_eq!(cfg.api_key, "file-key");
        assert_eq!(cfg.model, "file-model");
        // 有图且未指定模型时回退视觉模型。
        let cfg = resolve_from("", "", "", &env, &file, true);
        assert_eq!(cfg.model, "env-vision");
        // 无任何来源时地址为空，由 chat_completions_url 报错。
        let cfg = resolve_from(
            "",
            "",
            "",
            &ConfigSource::default(),
            &ConfigSource::default(),
            false,
        );
        assert_eq!(cfg.base_url, "");
        assert_eq!(cfg.api_key, "");
        assert_eq!(cfg.model, "deepseek-v4");
    }

    #[test]
    fn system_prompt_fixes_product_identity() {
        let messages = build_chat_messages(&BrowserContext::default(), "你是谁", &[], 16000);
        let system = messages[0]["content"].as_str().unwrap();
        assert!(system.contains("安能助手"));
        assert!(system.contains("不得自称 TRAE"));
    }

    #[test]
    fn http_error_keeps_provider_message() {
        assert_eq!(
            format_http_error(400, r#"{"error":{"message":"invalid model"}}"#),
            r#"模型服务返回 HTTP 400: {"error":{"message":"invalid model"}}"#
        );
        assert_eq!(format_http_error(503, "  "), "模型服务返回 HTTP 503");
    }
}
