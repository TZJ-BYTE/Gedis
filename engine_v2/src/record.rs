use std::fs::File;
use std::io::{Read, Seek, SeekFrom};

use crate::error::{EngineError, Result};

const RECORD_MAGIC: [u8; 4] = *b"RDG2";
const RECORD_VERSION: u8 = 1;
pub const RECORD_HEADER_LEN: usize = 20;

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub enum RecordKind {
    Put = 1,
    Delete = 2,
}

impl RecordKind {
    fn from_byte(value: u8) -> Result<Self> {
        match value {
            1 => Ok(Self::Put),
            2 => Ok(Self::Delete),
            _ => Err(EngineError::Corruption(format!(
                "unknown record kind: {value}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct ValueLocation {
    pub segment_id: u64,
    pub record_offset: u64,
    pub value_offset: u64,
    pub value_len: u32,
}

#[derive(Debug, Clone)]
pub struct Record {
    pub kind: RecordKind,
    pub key: Vec<u8>,
    pub value: Vec<u8>,
}

#[derive(Debug, Clone)]
pub struct ParsedRecord {
    pub kind: RecordKind,
    pub key: Vec<u8>,
    pub value: Vec<u8>,
    pub next_offset: u64,
}

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub enum ReadStatus {
    Complete,
    EndOfFile,
    TruncatedTail,
}

impl Record {
    pub fn put(key: &[u8], value: &[u8]) -> Self {
        Self {
            kind: RecordKind::Put,
            key: key.to_vec(),
            value: value.to_vec(),
        }
    }

    pub fn delete(key: &[u8]) -> Self {
        Self {
            kind: RecordKind::Delete,
            key: key.to_vec(),
            value: Vec::new(),
        }
    }

    pub fn encoded_len(&self) -> usize {
        RECORD_HEADER_LEN + self.key.len() + self.value.len()
    }

    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(self.encoded_len());
        buf.extend_from_slice(&RECORD_MAGIC);
        buf.push(RECORD_VERSION);
        buf.push(self.kind as u8);
        buf.extend_from_slice(&[0_u8; 2]);
        buf.extend_from_slice(&(self.key.len() as u32).to_le_bytes());
        buf.extend_from_slice(&(self.value.len() as u32).to_le_bytes());
        buf.extend_from_slice(&checksum(self.kind, &self.key, &self.value).to_le_bytes());
        buf.extend_from_slice(&self.key);
        buf.extend_from_slice(&self.value);
        buf
    }
}

pub fn read_record(file: &mut File, offset: u64) -> Result<(ReadStatus, Option<ParsedRecord>)> {
    file.seek(SeekFrom::Start(offset))?;

    let mut header = [0_u8; RECORD_HEADER_LEN];
    let read_header = file.read(&mut header)?;
    if read_header == 0 {
        return Ok((ReadStatus::EndOfFile, None));
    }
    if read_header < RECORD_HEADER_LEN {
        return Ok((ReadStatus::TruncatedTail, None));
    }

    if header[..4] != RECORD_MAGIC {
        return Err(EngineError::Corruption(format!(
            "invalid record magic at offset {offset}"
        )));
    }
    if header[4] != RECORD_VERSION {
        return Err(EngineError::Corruption(format!(
            "unsupported record version {} at offset {offset}",
            header[4]
        )));
    }

    let kind = RecordKind::from_byte(header[5])?;
    let key_len = u32::from_le_bytes(header[8..12].try_into().unwrap()) as usize;
    let value_len = u32::from_le_bytes(header[12..16].try_into().unwrap()) as usize;
    let expected_checksum = u32::from_le_bytes(header[16..20].try_into().unwrap());

    let mut key = vec![0_u8; key_len];
    if file.read_exact(&mut key).is_err() {
        return Ok((ReadStatus::TruncatedTail, None));
    }

    let mut value = vec![0_u8; value_len];
    if file.read_exact(&mut value).is_err() {
        return Ok((ReadStatus::TruncatedTail, None));
    }

    let actual_checksum = checksum(kind, &key, &value);
    if actual_checksum != expected_checksum {
        return Ok((ReadStatus::TruncatedTail, None));
    }

    Ok((
        ReadStatus::Complete,
        Some(ParsedRecord {
            kind,
            key,
            value,
            next_offset: offset + RECORD_HEADER_LEN as u64 + key_len as u64 + value_len as u64,
        }),
    ))
}

fn checksum(kind: RecordKind, key: &[u8], value: &[u8]) -> u32 {
    let mut hash = 2_166_136_261_u32;
    hash = hash.wrapping_mul(16_777_619) ^ (kind as u8 as u32);
    for &byte in key {
        hash = hash.wrapping_mul(16_777_619) ^ u32::from(byte);
    }
    for &byte in value {
        hash = hash.wrapping_mul(16_777_619) ^ u32::from(byte);
    }
    hash
}
