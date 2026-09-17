use crate::api::{build_chat_messages, diagnostic_argv, resolve_chat_api, send_stream};
use crate::prompt::{assemble_prompt, local_path_hint_from_url, resolve_turn_cwd};
use crate::protocol::{BrowserContext, StreamEvent};
use crate::store::{
    append_turn, default_session_root, list_stored_sessions, load_stored_messages,
    load_stored_session, new_session_id, save_stored_session, StoredSession,
};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

pub struct Runner {
    pub dry_run: bool,
    pub store_dir: Option<PathBuf>,
    cancels: Mutex<HashMap<String, Arc<AtomicBool>>>,
}

impl Default for Runner {
    fn default() -> Self {
        Self {
            dry_run: false,
            store_dir: None,
            cancels: Mutex::new(HashMap::new()),
        }
    }
}

impl Runner {
    pub fn new(dry_run: bool) -> Self {
        Self {
            dry_run,
            ..Default::default()
        }
    }
}

pub struct TurnRequest {
    pub request_id: String,
    pub session_id: String,
    pub cwd: String,
    pub user_text: String,
    pub browser: BrowserContext,
    pub model: String,
    pub api_base: String,
    pub api_key: String,
    pub dry_run: bool,
    pub max_page_chars: usize,
}

pub struct TurnResult {
    pub session_id: String,
    pub argv: Vec<String>,
    pub prompt: String,
    pub dry_run: bool,
    pub err: Option<String>,
}

impl Runner {
    pub fn session_root(&self) -> PathBuf {
        self.store_dir.clone().unwrap_or_else(default_session_root)
    }

    pub fn cancel(&self, request_id: &str) {
        if let Ok(g) = self.cancels.lock() {
            if request_id.is_empty() {
                for flag in g.values() {
                    flag.store(true, Ordering::Relaxed);
                }
            } else if let Some(flag) = g.get(request_id) {
                flag.store(true, Ordering::Relaxed);
            }
        }
    }

    pub fn list_sessions(
        &self,
        query: &str,
        limit: i32,
    ) -> Result<Vec<crate::protocol::SessionInfo>, String> {
        list_stored_sessions(&self.session_root(), query, limit)
    }

    pub fn get_session(
        &self,
        id: &str,
        limit: i32,
    ) -> Result<
        (
            Vec<crate::protocol::SessionMessage>,
            crate::protocol::SessionInfo,
        ),
        String,
    > {
        load_stored_messages(&self.session_root(), id, limit)
    }

    /// 跑一轮对话：dry-run 不碰外网；正式请求走加密内置配置或覆盖项。
    pub fn run_turn(
        &self,
        mut req: TurnRequest,
        mut on_event: impl FnMut(StreamEvent),
    ) -> TurnResult {
        if req
            .browser
            .local_path_hint
            .as_deref()
            .unwrap_or("")
            .trim()
            .is_empty()
        {
            if let Some(url) = req.browser.url.clone() {
                let hint = local_path_hint_from_url(&url);
                if !hint.is_empty() {
                    req.browser.local_path_hint = Some(hint);
                }
            }
        }
        let cwd = resolve_turn_cwd(&req.cwd, "");
        let prompt = assemble_prompt(&req.browser, &req.user_text, req.max_page_chars);
        let has_images = req
            .browser
            .images
            .as_ref()
            .map(|v| !v.is_empty())
            .unwrap_or(false);
        let cfg = resolve_chat_api(&req.api_base, &req.api_key, &req.model, has_images);
        let dry = self.dry_run || req.dry_run;
        let mut result = TurnResult {
            session_id: req.session_id.clone(),
            argv: diagnostic_argv(&cfg, &req.session_id, &cwd),
            prompt,
            dry_run: dry,
            err: None,
        };
        on_event(StreamEvent {
            kind: "partial".into(),
            text: Some(format!("argv: {:?}", result.argv)),
            raw_type: Some("host.argv".into()),
            ..Default::default()
        });

        if dry {
            return self.run_fixture(&req, result, &mut on_event);
        }

        if let Err(e) = crate::api::chat_completions_url(&cfg.base_url) {
            result.err = Some(e.clone());
            on_event(StreamEvent {
                kind: "error".into(),
                text: Some(e),
                ..Default::default()
            });
            return result;
        }

        let root = self.session_root();
        let mut history = vec![];
        if !req.session_id.trim().is_empty() {
            if let Ok((msgs, _)) = load_stored_messages(&root, &req.session_id, 0) {
                history = msgs;
            }
        }
        let messages =
            build_chat_messages(&req.browser, &req.user_text, &history, req.max_page_chars);
        let flag = Arc::new(AtomicBool::new(false));
        let cancel_key = req.request_id.trim().to_string();
        if !cancel_key.is_empty() {
            if let Ok(mut g) = self.cancels.lock() {
                g.insert(cancel_key.clone(), flag.clone());
            }
        }

        let send = send_stream(&cfg, messages, &flag, |kind, text| {
            on_event(StreamEvent {
                kind: kind.into(),
                text: Some(text.to_string()),
                raw_type: Some(if kind == "thinking" {
                    "openai.reasoning".into()
                } else {
                    "openai.delta".into()
                }),
                ..Default::default()
            });
        });

        if !cancel_key.is_empty() {
            if let Ok(mut g) = self.cancels.lock() {
                g.remove(&cancel_key);
            }
        }

        match send {
            Ok((assistant, thinking)) => {
                let mut sid = req.session_id.clone();
                if sid.trim().is_empty() {
                    sid = new_session_id();
                }
                result.session_id = sid.clone();
                on_event(StreamEvent {
                    kind: "session".into(),
                    session_id: Some(sid.clone()),
                    raw_type: Some("host.session".into()),
                    ..Default::default()
                });
                let mut sess = load_stored_session(&root, &sid).unwrap_or(StoredSession {
                    info: crate::protocol::SessionInfo {
                        id: sid.clone(),
                        title: "新对话".into(),
                        ..Default::default()
                    },
                    messages: vec![],
                });
                append_turn(
                    &mut sess,
                    &cwd,
                    &cfg.model,
                    req.user_text.trim(),
                    &assistant,
                    &thinking,
                );
                if let Err(e) = save_stored_session(&root, &sess) {
                    on_event(StreamEvent {
                        kind: "error".into(),
                        text: Some(format!("保存会话失败: {e}")),
                        ..Default::default()
                    });
                }
                if flag.load(Ordering::Relaxed) {
                    on_event(StreamEvent {
                        kind: "done".into(),
                        raw_type: Some("host.cancelled".into()),
                        ..Default::default()
                    });
                } else {
                    on_event(StreamEvent {
                        kind: "done".into(),
                        raw_type: Some("host.api_done".into()),
                        ..Default::default()
                    });
                }
            }
            Err(e) => {
                if flag.load(Ordering::Relaxed) {
                    on_event(StreamEvent {
                        kind: "done".into(),
                        raw_type: Some("host.cancelled".into()),
                        ..Default::default()
                    });
                } else {
                    result.err = Some(e.clone());
                    on_event(StreamEvent {
                        kind: "error".into(),
                        text: Some(e),
                        ..Default::default()
                    });
                }
            }
        }
        result
    }

    fn run_fixture(
        &self,
        req: &TurnRequest,
        mut result: TurnResult,
        on_event: &mut impl FnMut(StreamEvent),
    ) -> TurnResult {
        let sid = if req.session_id.trim().is_empty() {
            "dry-session-1".into()
        } else {
            req.session_id.clone()
        };
        result.session_id = sid.clone();
        on_event(StreamEvent {
            kind: "session".into(),
            session_id: Some(sid),
            raw_type: Some("host.session".into()),
            ..Default::default()
        });
        on_event(StreamEvent {
            kind: "thinking".into(),
            text: Some("dry-run thinking… ".into()),
            raw_type: Some("thinking_delta".into()),
            ..Default::default()
        });
        on_event(StreamEvent {
            kind: "partial".into(),
            text: Some("dry-run ok".into()),
            raw_type: Some("text_delta".into()),
            ..Default::default()
        });
        on_event(StreamEvent {
            kind: "done".into(),
            raw_type: Some("host.dry_run".into()),
            ..Default::default()
        });
        result
    }
}
