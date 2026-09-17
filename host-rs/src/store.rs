use crate::protocol::{SessionInfo, SessionMessage};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct StoredSession {
    pub info: SessionInfo,
    pub messages: Vec<SessionMessage>,
}

pub fn sidechat_home() -> PathBuf {
    if let Ok(h) = std::env::var("SIDECHAT_HOME") {
        if !h.trim().is_empty() {
            return PathBuf::from(h);
        }
    }
    dirs_next_home().join(".sidechat")
}

fn dirs_next_home() -> PathBuf {
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
}

pub fn default_session_root() -> PathBuf {
    sidechat_home().join("sessions")
}

pub fn new_session_id() -> String {
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis())
        .unwrap_or(0);
    let rand = {
        let mut b = [0u8; 4];
        rand::rngs::OsRng.fill_bytes(&mut b);
        hex4(&b)
    };
    format!("sc-{millis}-{rand}")
}

fn hex4(b: &[u8; 4]) -> String {
    format!("{:02x}{:02x}{:02x}{:02x}", b[0], b[1], b[2], b[3])
}

fn session_file(root: &Path, id: &str) -> PathBuf {
    root.join(id).join("session.json")
}

pub fn load_stored_session(root: &Path, id: &str) -> Result<StoredSession, String> {
    let id = id.trim();
    if id.is_empty() {
        return Err("session not found".into());
    }
    let data = fs::read(session_file(root, id)).map_err(|_| "session not found".to_string())?;
    let mut sess: StoredSession = serde_json::from_slice(&data).map_err(|e| e.to_string())?;
    if sess.info.id.is_empty() {
        sess.info.id = id.to_string();
    }
    Ok(sess)
}

pub fn save_stored_session(root: &Path, sess: &StoredSession) -> Result<(), String> {
    if sess.info.id.trim().is_empty() {
        return Err("invalid session".into());
    }
    let dir = root.join(&sess.info.id);
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(&dir, fs::Permissions::from_mode(0o700));
    }
    let data = serde_json::to_vec_pretty(sess).map_err(|e| e.to_string())?;
    let path = session_file(root, &sess.info.id);
    fs::write(&path, data).map_err(|e| e.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(&path, fs::Permissions::from_mode(0o600));
    }
    Ok(())
}

fn title_from_user(text: &str) -> String {
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if text.is_empty() {
        return "新对话".into();
    }
    let chars: Vec<char> = text.chars().collect();
    if chars.len() <= 40 {
        text
    } else {
        format!("{}…", chars.into_iter().take(40).collect::<String>())
    }
}

pub fn append_turn(
    sess: &mut StoredSession,
    cwd: &str,
    model: &str,
    user: &str,
    assistant: &str,
    thinking: &str,
) {
    let now = now_rfc3339();
    if sess.info.created_at.as_deref().unwrap_or("").is_empty() {
        sess.info.created_at = Some(now.clone());
    }
    sess.info.updated_at = Some(now);
    if !cwd.is_empty() {
        sess.info.cwd = Some(cwd.to_string());
    }
    if !model.is_empty() {
        sess.info.model = Some(model.to_string());
    }
    if sess.info.title.is_empty() || sess.info.title == "新对话" {
        sess.info.title = title_from_user(user);
    }
    if !user.trim().is_empty() {
        sess.messages.push(SessionMessage {
            role: "user".into(),
            text: user.to_string(),
        });
    }
    if !thinking.trim().is_empty() {
        sess.messages.push(SessionMessage {
            role: "thinking".into(),
            text: thinking.to_string(),
        });
    }
    if !assistant.trim().is_empty() {
        sess.messages.push(SessionMessage {
            role: "assistant".into(),
            text: assistant.to_string(),
        });
    }
    sess.info.num_messages = Some(sess.messages.len() as i32);
    let summary = if assistant.trim().is_empty() {
        user.trim().to_string()
    } else {
        assistant.trim().to_string()
    };
    sess.info.summary = Some(summary);
}

pub fn list_stored_sessions(
    root: &Path,
    query: &str,
    limit: i32,
) -> Result<Vec<SessionInfo>, String> {
    if !root.exists() {
        return Ok(vec![]);
    }
    let query = query.trim().to_lowercase();
    let mut out = vec![];
    let entries = fs::read_dir(root).map_err(|e| e.to_string())?;
    for ent in entries.flatten() {
        if !ent.path().is_dir() {
            continue;
        }
        let name = ent.file_name().to_string_lossy().into_owned();
        let Ok(sess) = load_stored_session(root, &name) else {
            continue;
        };
        let info = sess.info;
        if !query.is_empty() {
            let blob = format!(
                "{} {} {}",
                info.title,
                info.summary.clone().unwrap_or_default(),
                info.id
            )
            .to_lowercase();
            if !blob.contains(&query) {
                continue;
            }
        }
        out.push(info);
    }
    out.sort_by(|a, b| b.updated_at.cmp(&a.updated_at).then(b.id.cmp(&a.id)));
    if limit > 0 && out.len() > limit as usize {
        out.truncate(limit as usize);
    }
    Ok(out)
}

pub fn load_stored_messages(
    root: &Path,
    session_id: &str,
    max_msgs: i32,
) -> Result<(Vec<SessionMessage>, SessionInfo), String> {
    let sess = load_stored_session(root, session_id)?;
    let mut msgs = sess.messages;
    if max_msgs > 0 && msgs.len() > max_msgs as usize {
        msgs = msgs.split_off(msgs.len() - max_msgs as usize);
    }
    Ok((msgs, sess.info))
}

fn now_rfc3339() -> String {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    format_rfc3339(secs)
}

fn format_rfc3339(secs: u64) -> String {
    // 足够排序与展示；避免再拉 chrono 依赖。
    let days = secs / 86400;
    let tod = secs % 86400;
    let (y, m, d) = civil_from_days(days as i64);
    let hh = tod / 3600;
    let mm = (tod % 3600) / 60;
    let ss = tod % 60;
    format!("{y:04}-{m:02}-{d:02}T{hh:02}:{mm:02}:{ss:02}Z")
}

fn civil_from_days(z: i64) -> (i32, u32, u32) {
    let z = z + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    (y as i32, m as u32, d as u32)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn persist_session() {
        let root = std::env::temp_dir().join(format!("sidechat-test-{}", new_session_id()));
        let mut sess = StoredSession {
            info: SessionInfo {
                id: "sc-demo".into(),
                title: "新对话".into(),
                ..Default::default()
            },
            messages: vec![],
        };
        append_turn(&mut sess, "", "deepseek-v4", "你好世界", "收到", "想了想");
        save_stored_session(&root, &sess).unwrap();
        let loaded = load_stored_session(&root, "sc-demo").unwrap();
        assert_eq!(loaded.info.title, "你好世界");
        assert_eq!(loaded.messages.len(), 3);
        let _ = fs::remove_dir_all(root);
    }
}
