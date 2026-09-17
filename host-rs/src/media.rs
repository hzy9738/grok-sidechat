use crate::api::{resolve_chat_api, ChatAPIConfig};
use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use serde::Deserialize;

const MAX_AUDIO_BYTES: usize = 24 * 1024 * 1024;
const MAX_PDF_BYTES: usize = 20 * 1024 * 1024;
const MAX_PDF_CHARS: usize = 120_000;

pub fn resolve_media_api(req_base: &str, req_key: &str) -> ChatAPIConfig {
    resolve_chat_api(req_base, req_key, "", false)
}

pub fn transcribe_audio(
    cfg: &ChatAPIConfig,
    data_url: &str,
    mime_type: &str,
    file_name: &str,
) -> Result<String, String> {
    let bytes = decode_data_url(data_url, MAX_AUDIO_BYTES)?;
    let endpoint = api_url(&cfg.base_url, "audio/transcriptions")?;
    let boundary = format!("anneng-{}", rand::random::<u64>());
    let safe_name = safe_file_name(file_name, extension_for_mime(mime_type));
    let mut body = Vec::with_capacity(bytes.len() + 512);
    append_field(&mut body, &boundary, "model", "whisper-1");
    body.extend_from_slice(format!("--{boundary}\r\n").as_bytes());
    body.extend_from_slice(
        format!(
            "Content-Disposition: form-data; name=\"file\"; filename=\"{safe_name}\"\r\nContent-Type: {}\r\n\r\n",
            clean_mime(mime_type)
        )
        .as_bytes(),
    );
    body.extend_from_slice(&bytes);
    body.extend_from_slice(format!("\r\n--{boundary}--\r\n").as_bytes());

    let mut req = ureq::post(&endpoint)
        .set(
            "Content-Type",
            &format!("multipart/form-data; boundary={boundary}"),
        )
        .set("Accept", "application/json");
    if !cfg.api_key.is_empty() {
        req = req.set("Authorization", &format!("Bearer {}", cfg.api_key));
    }
    let response = req
        .send_bytes(&body)
        .map_err(|e| format!("语音转写接口: {e}"))?;
    let parsed: TranscriptionResponse = response
        .into_json()
        .map_err(|e| format!("语音转写响应无效: {e}"))?;
    let text = parsed.text.trim().to_string();
    if text.is_empty() {
        Err("语音转写结果为空".into())
    } else {
        Ok(text)
    }
}

pub fn extract_pdf_text(data_url: &str) -> Result<String, String> {
    let bytes = decode_data_url(data_url, MAX_PDF_BYTES)?;
    let text =
        pdf_extract::extract_text_from_mem(&bytes).map_err(|e| format!("PDF 文本提取失败: {e}"))?;
    let clean = text.trim();
    if clean.is_empty() {
        return Err("PDF 没有可提取文字；扫描版请先用图片 OCR。".into());
    }
    Ok(clean.chars().take(MAX_PDF_CHARS).collect())
}

fn api_url(base: &str, path: &str) -> Result<String, String> {
    let mut root = base.trim().trim_end_matches('/').to_string();
    if root.is_empty() {
        return Err("需要 API 地址（扩展设置、SIDECHAT_API_BASE 或随包 anneng-config.json）".into());
    }
    if let Some(prefix) = root.strip_suffix("/chat/completions") {
        root = prefix.to_string();
    }
    Ok(format!("{root}/{path}"))
}

fn decode_data_url(input: &str, max_bytes: usize) -> Result<Vec<u8>, String> {
    let encoded = input
        .split_once(',')
        .map(|(_, data)| data)
        .unwrap_or(input)
        .trim();
    if encoded.len() > max_bytes.saturating_mul(4) / 3 + 16 {
        return Err("文件过大".into());
    }
    let bytes = STANDARD
        .decode(encoded)
        .map_err(|_| "文件编码无效".to_string())?;
    if bytes.is_empty() || bytes.len() > max_bytes {
        return Err("文件为空或超过大小限制".into());
    }
    Ok(bytes)
}

fn append_field(body: &mut Vec<u8>, boundary: &str, name: &str, value: &str) {
    body.extend_from_slice(format!("--{boundary}\r\n").as_bytes());
    body.extend_from_slice(
        format!("Content-Disposition: form-data; name=\"{name}\"\r\n\r\n{value}\r\n").as_bytes(),
    );
}

fn extension_for_mime(mime: &str) -> &'static str {
    match mime.split(';').next().unwrap_or("").trim() {
        "audio/mp4" | "audio/x-m4a" => "m4a",
        "audio/ogg" => "ogg",
        "audio/wav" | "audio/wave" => "wav",
        _ => "webm",
    }
}

fn clean_mime(mime: &str) -> &str {
    let value = mime.trim();
    if value.starts_with("audio/") {
        value
    } else {
        "audio/webm"
    }
}

fn safe_file_name(name: &str, fallback_ext: &str) -> String {
    let clean: String = name
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || matches!(c, '.' | '-' | '_'))
        .take(80)
        .collect();
    if clean.is_empty() {
        format!("recording.{fallback_ext}")
    } else {
        clean
    }
}

#[derive(Deserialize)]
struct TranscriptionResponse {
    text: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn endpoint_uses_openai_compatible_audio_path() {
        assert_eq!(
            api_url(
                "https://example.com/api/chat/completions",
                "audio/transcriptions"
            )
            .unwrap(),
            "https://example.com/api/audio/transcriptions"
        );
    }

    #[test]
    fn rejects_oversized_data_before_decoding() {
        let huge = "a".repeat(200);
        assert!(decode_data_url(&huge, 8).is_err());
    }
}
