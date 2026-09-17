use serde::{Deserialize, Serialize};
use serde_json::Value;

pub const HOST_VERSION: &str = "0.6.2";

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
#[allow(dead_code)]
pub struct ClientMessage {
    pub op: String,
    pub request_id: Option<String>,
    pub session_id: Option<String>,
    pub cwd: Option<String>,
    pub text: Option<String>,
    pub browser: Option<BrowserContext>,
    pub model: Option<String>,
    pub mode: Option<String>,
    pub reasoning_effort: Option<String>,
    pub max_turns: Option<i32>,
    pub always_approve: Option<bool>,
    pub dry_run: Option<bool>,
    pub api_base: Option<String>,
    pub api_key: Option<String>,
    pub query: Option<String>,
    pub limit: Option<i32>,
    pub call_id: Option<String>,
    pub result: Option<Value>,
    pub ok: Option<bool>,
    pub error: Option<String>,
    pub media_data: Option<String>,
    pub mime_type: Option<String>,
    pub file_name: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TabInfo {
    pub title: Option<String>,
    pub url: Option<String>,
    pub id: Option<i64>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ContextMention {
    pub kind: Option<String>,
    pub title: Option<String>,
    pub url: Option<String>,
    pub text: Option<String>,
    pub tab_id: Option<i64>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ImageRef {
    pub src: Option<String>,
    pub data_url: Option<String>,
    pub alt: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct BrowserContext {
    pub title: Option<String>,
    pub url: Option<String>,
    pub selection: Option<String>,
    pub page_text: Option<String>,
    pub tabs: Option<Vec<TabInfo>>,
    pub include_tabs: Option<bool>,
    pub mentions: Option<Vec<ContextMention>>,
    pub local_path_hint: Option<String>,
    pub enable_browser_control: Option<bool>,
    pub images: Option<Vec<ImageRef>>,
    pub tab_id: Option<i64>,
    pub window_id: Option<i64>,
}

#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HostMessage {
    pub op: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub event: Option<StreamEvent>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub argv: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prompt: Option<String>,
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub info: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sessions: Option<Vec<SessionInfo>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub messages: Option<Vec<SessionMessage>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub session: Option<SessionInfo>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub call_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub action: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StreamEvent {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub input: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub raw_type: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionInfo {
    pub id: String,
    pub title: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub summary: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cwd: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub updated_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub created_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub num_messages: Option<i32>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SessionMessage {
    pub role: String,
    pub text: String,
}

impl HostMessage {
    pub fn pong(request_id: Option<String>) -> Self {
        Self {
            op: "pong".into(),
            request_id,
            ok: true,
            version: Some(HOST_VERSION.into()),
            ..Default::default()
        }
    }

    pub fn hello() -> Self {
        Self {
            op: "hello".into(),
            ok: true,
            version: Some(HOST_VERSION.into()),
            ..Default::default()
        }
    }

    pub fn error(request_id: Option<String>, err: impl Into<String>) -> Self {
        Self {
            op: "error".into(),
            request_id,
            ok: false,
            error: Some(err.into()),
            ..Default::default()
        }
    }
}
