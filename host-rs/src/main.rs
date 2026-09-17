mod api;
mod media;
mod native;
mod prompt;
mod protocol;
mod runner;
mod store;

use native::{
    read_json_line, read_native_message, write_json_line, write_native_message, NativeError,
};
use protocol::{ClientMessage, HostMessage, HOST_VERSION};
use runner::{Runner, TurnRequest};
use std::io::{self, Write};
use std::sync::{Arc, Mutex};
use std::thread;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut mode = "native".to_string();
    let mut dry_run = false;
    let mut once_json = String::new();
    let mut cwd = String::new();
    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "-mode" | "--mode" => {
                i += 1;
                if i < args.len() {
                    mode = args[i].clone();
                }
            }
            "-dry-run" | "--dry-run" => dry_run = true,
            "-once" | "--once" => {
                i += 1;
                if i < args.len() {
                    once_json = args[i].clone();
                }
            }
            "-cwd" | "--cwd" => {
                i += 1;
                if i < args.len() {
                    cwd = args[i].clone();
                }
            }
            "-cmd" | "--cmd" | "-fixture" | "--fixture" => {
                i += 1; // 兼容旧参数，忽略
            }
            _ => {}
        }
        i += 1;
    }

    let runner = Arc::new(Runner::new(dry_run));

    match mode.as_str() {
        "once" => {
            if let Err(e) = run_once(&runner, &cwd, &once_json) {
                eprintln!("once: {e}");
                std::process::exit(1);
            }
        }
        "stdio" => {
            if let Err(e) = serve_loop(&runner, &cwd, true) {
                if !matches!(e, LoopError::Eof) {
                    eprintln!("stdio: {e}");
                    std::process::exit(1);
                }
            }
        }
        _ => {
            if let Err(e) = serve_loop(&runner, &cwd, false) {
                if !matches!(e, LoopError::Eof) {
                    eprintln!("native: {e}");
                    std::process::exit(1);
                }
            }
        }
    }
}

fn run_once(runner: &Runner, default_cwd: &str, once_path: &str) -> Result<(), String> {
    let msg = if once_path.is_empty() {
        ClientMessage {
            op: "send".into(),
            request_id: Some("once-1".into()),
            text: Some("hello from once mode".into()),
            browser: Some(protocol::BrowserContext {
                title: Some("Example Page".into()),
                url: Some("https://example.com/".into()),
                selection: Some("selected snippet".into()),
                include_tabs: Some(true),
                tabs: Some(vec![
                    protocol::TabInfo {
                        title: Some("Example Page".into()),
                        url: Some("https://example.com/".into()),
                        id: None,
                    },
                    protocol::TabInfo {
                        title: Some("Docs".into()),
                        url: Some("https://docs.example.com/".into()),
                        id: None,
                    },
                ]),
                ..Default::default()
            }),
            cwd: Some(default_cwd.to_string()),
            dry_run: Some(runner.dry_run),
            mode: Some("default".into()),
            ..Default::default()
        }
    } else {
        let data = std::fs::read(once_path).map_err(|e| e.to_string())?;
        serde_json::from_slice(&data).map_err(|e| e.to_string())?
    };
    let stdout = io::stdout();
    handle_message(runner, default_cwd, msg, |out| {
        let mut w = stdout.lock();
        serde_json::to_writer_pretty(&mut w, &out).map_err(|e| e.to_string())?;
        w.write_all(b"\n").map_err(|e| e.to_string())?;
        Ok(())
    })
}

enum LoopError {
    Eof,
    Other(String),
}

impl std::fmt::Display for LoopError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            LoopError::Eof => write!(f, "eof"),
            LoopError::Other(s) => write!(f, "{s}"),
        }
    }
}

#[derive(Clone)]
struct OutWriter {
    stdout: Arc<Mutex<io::Stdout>>,
    line_json: bool,
}

impl OutWriter {
    fn send(&self, msg: HostMessage) -> Result<(), String> {
        let mut w = self.stdout.lock().map_err(|e| e.to_string())?;
        if self.line_json {
            write_json_line(&mut *w, &msg).map_err(|e| e.to_string())
        } else {
            write_native_message(&mut *w, &msg).map_err(|e| e.to_string())
        }
    }
}

fn serve_loop(runner: &Arc<Runner>, default_cwd: &str, line_json: bool) -> Result<(), LoopError> {
    let writer = OutWriter {
        stdout: Arc::new(Mutex::new(io::stdout())),
        line_json,
    };
    let _ = writer.send(HostMessage::hello());

    let stdin = io::stdin();
    let mut input = stdin.lock();
    loop {
        let msg = if line_json {
            match read_json_line(&mut input) {
                Ok(m) => m,
                Err(NativeError::Eof) => return Err(LoopError::Eof),
                Err(e) => return Err(LoopError::Other(e.to_string())),
            }
        } else {
            match read_native_message(&mut input) {
                Ok(m) => m,
                Err(NativeError::Eof) => return Err(LoopError::Eof),
                Err(e) => return Err(LoopError::Other(e.to_string())),
            }
        };
        let op = msg.op.to_ascii_lowercase();
        if op == "send" {
            let runner = runner.clone();
            let cwd = default_cwd.to_string();
            let writer = writer.clone();
            let req_id = msg.request_id.clone();
            thread::spawn(move || {
                if let Err(e) = handle_send(&runner, &cwd, msg, &|m| writer.send(m)) {
                    let _ = writer.send(HostMessage::error(req_id, e));
                }
            });
            continue;
        }
        if let Err(e) = handle_message(runner, default_cwd, msg, |m| writer.send(m)) {
            let _ = writer.send(HostMessage::error(None, e));
        }
    }
}

fn handle_message(
    runner: &Runner,
    default_cwd: &str,
    msg: ClientMessage,
    write: impl Fn(HostMessage) -> Result<(), String>,
) -> Result<(), String> {
    match msg.op.to_ascii_lowercase().as_str() {
        "ping" | "hello" => write(HostMessage::pong(msg.request_id)),
        "cancel" => {
            runner.cancel(msg.request_id.as_deref().unwrap_or(""));
            write(HostMessage {
                op: "cancelled".into(),
                request_id: msg.request_id,
                ok: true,
                version: Some(HOST_VERSION.into()),
                ..Default::default()
            })
        }
        "list_sessions" => match runner
            .list_sessions(msg.query.as_deref().unwrap_or(""), msg.limit.unwrap_or(40))
        {
            Ok(list) => write(HostMessage {
                op: "list_sessions".into(),
                request_id: msg.request_id,
                ok: true,
                sessions: Some(list),
                ..Default::default()
            }),
            Err(e) => write(HostMessage {
                op: "list_sessions".into(),
                request_id: msg.request_id,
                ok: false,
                error: Some(e),
                ..Default::default()
            }),
        },
        "get_session" => {
            match runner.get_session(
                msg.session_id.as_deref().unwrap_or(""),
                msg.limit.unwrap_or(80),
            ) {
                Ok((messages, meta)) => write(HostMessage {
                    op: "get_session".into(),
                    request_id: msg.request_id,
                    ok: true,
                    session_id: Some(meta.id.clone()),
                    session: Some(meta),
                    messages: Some(messages),
                    ..Default::default()
                }),
                Err(e) => write(HostMessage {
                    op: "get_session".into(),
                    request_id: msg.request_id,
                    ok: false,
                    error: Some(e),
                    ..Default::default()
                }),
            }
        }
        "transcribe_audio" => {
            let cfg = media::resolve_media_api(
                msg.api_base.as_deref().unwrap_or(""),
                msg.api_key.as_deref().unwrap_or(""),
            );
            match media::transcribe_audio(
                &cfg,
                msg.media_data.as_deref().unwrap_or(""),
                msg.mime_type.as_deref().unwrap_or("audio/webm"),
                msg.file_name.as_deref().unwrap_or("recording.webm"),
            ) {
                Ok(text) => write(HostMessage {
                    op: "transcribe_audio".into(),
                    request_id: msg.request_id,
                    ok: true,
                    text: Some(text),
                    ..Default::default()
                }),
                Err(e) => write(HostMessage::error(msg.request_id, e)),
            }
        }
        "extract_pdf" => match media::extract_pdf_text(msg.media_data.as_deref().unwrap_or("")) {
            Ok(text) => write(HostMessage {
                op: "extract_pdf".into(),
                request_id: msg.request_id,
                ok: true,
                text: Some(text),
                ..Default::default()
            }),
            Err(e) => write(HostMessage::error(msg.request_id, e)),
        },
        "send" => handle_send(runner, default_cwd, msg, &write),
        other => write(HostMessage::error(
            msg.request_id,
            format!("unknown op {other:?}"),
        )),
    }
}

fn handle_send(
    runner: &Runner,
    default_cwd: &str,
    msg: ClientMessage,
    write: &impl Fn(HostMessage) -> Result<(), String>,
) -> Result<(), String> {
    let text = msg.text.as_deref().unwrap_or("").trim().to_string();
    if text.is_empty() {
        return write(HostMessage::error(
            msg.request_id.clone(),
            "text is required",
        ));
    }
    let explicit = first_nonempty(&[msg.cwd.as_deref().unwrap_or(""), default_cwd]);
    let mut browser = msg.browser.unwrap_or_default();
    browser.enable_browser_control = Some(false);
    let req = TurnRequest {
        request_id: msg.request_id.clone().unwrap_or_default(),
        session_id: msg.session_id.unwrap_or_default(),
        cwd: explicit,
        user_text: text,
        browser,
        model: msg.model.unwrap_or_default(),
        api_base: msg.api_base.unwrap_or_default(),
        api_key: msg.api_key.unwrap_or_default(),
        dry_run: msg.dry_run.unwrap_or(false),
        max_page_chars: 16000,
    };
    let request_id = msg.request_id.clone();
    let mut session_id = String::new();
    let result = runner.run_turn(req, |ev| {
        if ev.kind == "session" {
            if let Some(sid) = &ev.session_id {
                session_id = sid.clone();
            }
        }
        let _ = write(HostMessage {
            op: "event".into(),
            request_id: request_id.clone(),
            event: Some(ev),
            session_id: if session_id.is_empty() {
                None
            } else {
                Some(session_id.clone())
            },
            ok: true,
            ..Default::default()
        });
    });
    session_id = if result.session_id.is_empty() {
        session_id
    } else {
        result.session_id.clone()
    };
    write(HostMessage {
        op: "send_done".into(),
        request_id,
        session_id: Some(session_id),
        argv: Some(result.argv),
        prompt: Some(result.prompt),
        ok: result.err.is_none(),
        error: result.err,
        info: if result.dry_run {
            Some(serde_json::json!({"dryRun": true}))
        } else {
            None
        },
        ..Default::default()
    })
}

fn first_nonempty(vals: &[&str]) -> String {
    vals.iter()
        .map(|s| s.trim())
        .find(|s| !s.is_empty())
        .unwrap_or("")
        .to_string()
}
