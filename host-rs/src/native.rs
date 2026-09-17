use crate::protocol::{ClientMessage, HostMessage};
use serde::de::DeserializeOwned;
use std::io::{Read, Write};

/// 读取 Chrome Native Messaging 一帧：小端 u32 长度 + JSON。
pub fn read_native_message<R: Read>(mut r: R) -> Result<ClientMessage, NativeError> {
    let mut len_buf = [0u8; 4];
    r.read_exact(&mut len_buf).map_err(NativeError::from_read)?;
    let n = u32::from_le_bytes(len_buf) as usize;
    if n == 0 || n > 64 * 1024 * 1024 {
        return Err(NativeError::BadLength(n));
    }
    let mut buf = vec![0u8; n];
    r.read_exact(&mut buf).map_err(NativeError::from_read)?;
    serde_json::from_slice(&buf).map_err(|e| NativeError::Json(e.to_string()))
}

pub fn write_native_message<W: Write>(mut w: W, msg: &HostMessage) -> Result<(), NativeError> {
    let data = serde_json::to_vec(msg).map_err(|e| NativeError::Json(e.to_string()))?;
    let n = data.len() as u32;
    w.write_all(&n.to_le_bytes())?;
    w.write_all(&data)?;
    w.flush()?;
    Ok(())
}

pub fn read_json_line<R: Read, T: DeserializeOwned>(r: &mut R) -> Result<T, NativeError> {
    let mut dec = serde_json::Deserializer::from_reader(r);
    T::deserialize(&mut dec).map_err(|e| NativeError::Json(e.to_string()))
}

pub fn write_json_line<W: Write>(mut w: W, msg: &HostMessage) -> Result<(), NativeError> {
    let mut data = serde_json::to_vec(msg).map_err(|e| NativeError::Json(e.to_string()))?;
    data.push(b'\n');
    w.write_all(&data)?;
    w.flush()?;
    Ok(())
}

#[derive(Debug)]
pub enum NativeError {
    Eof,
    Io(std::io::Error),
    BadLength(usize),
    Json(String),
}

impl NativeError {
    fn from_read(e: std::io::Error) -> Self {
        if e.kind() == std::io::ErrorKind::UnexpectedEof {
            NativeError::Eof
        } else {
            NativeError::Io(e)
        }
    }
}

impl From<std::io::Error> for NativeError {
    fn from(e: std::io::Error) -> Self {
        NativeError::Io(e)
    }
}

impl std::fmt::Display for NativeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            NativeError::Eof => write!(f, "eof"),
            NativeError::Io(e) => write!(f, "{e}"),
            NativeError::BadLength(n) => write!(f, "invalid native message length {n}"),
            NativeError::Json(e) => write!(f, "json: {e}"),
        }
    }
}
