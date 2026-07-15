use std::collections::BTreeMap;
use std::fs::File;
use std::io::{Read, Write};
use std::path::Path;

use crate::error::{EngineError, Result};
use crate::record::ValueLocation;

const CHECKPOINT_MAGIC: [u8; 8] = *b"RDG2CKPT";
const CHECKPOINT_VERSION: u32 = 1;

#[derive(Debug, Clone)]
pub struct CheckpointState {
    pub active_segment_id: u64,
    pub active_offset: u64,
    pub index: BTreeMap<Vec<u8>, ValueLocation>,
}

pub fn load(path: &Path) -> Result<Option<CheckpointState>> {
    if !path.exists() {
        return Ok(None);
    }

    let mut file = File::open(path)?;
    let mut magic = [0_u8; 8];
    file.read_exact(&mut magic)?;
    if magic != CHECKPOINT_MAGIC {
        return Err(EngineError::Corruption(
            "invalid checkpoint magic".to_string(),
        ));
    }

    let version = read_u32(&mut file)?;
    if version != CHECKPOINT_VERSION {
        return Err(EngineError::Corruption(format!(
            "unsupported checkpoint version {version}"
        )));
    }

    let active_segment_id = read_u64(&mut file)?;
    let active_offset = read_u64(&mut file)?;
    let entry_count = read_u64(&mut file)?;
    let mut index = BTreeMap::new();

    for _ in 0..entry_count {
        let key_len = read_u32(&mut file)? as usize;
        let mut key = vec![0_u8; key_len];
        file.read_exact(&mut key)?;

        let segment_id = read_u64(&mut file)?;
        let record_offset = read_u64(&mut file)?;
        let value_offset = read_u64(&mut file)?;
        let value_len = read_u32(&mut file)?;

        index.insert(
            key,
            ValueLocation {
                segment_id,
                record_offset,
                value_offset,
                value_len,
            },
        );
    }

    Ok(Some(CheckpointState {
        active_segment_id,
        active_offset,
        index,
    }))
}

pub fn write(path: &Path, state: &CheckpointState) -> Result<()> {
    let tmp_path = path.with_extension("tmp");
    let mut file = File::create(&tmp_path)?;

    file.write_all(&CHECKPOINT_MAGIC)?;
    file.write_all(&CHECKPOINT_VERSION.to_le_bytes())?;
    file.write_all(&state.active_segment_id.to_le_bytes())?;
    file.write_all(&state.active_offset.to_le_bytes())?;
    file.write_all(&(state.index.len() as u64).to_le_bytes())?;

    for (key, location) in &state.index {
        file.write_all(&(key.len() as u32).to_le_bytes())?;
        file.write_all(key)?;
        file.write_all(&location.segment_id.to_le_bytes())?;
        file.write_all(&location.record_offset.to_le_bytes())?;
        file.write_all(&location.value_offset.to_le_bytes())?;
        file.write_all(&location.value_len.to_le_bytes())?;
    }

    file.flush()?;
    file.sync_all()?;
    drop(file);

    std::fs::rename(tmp_path, path)?;
    Ok(())
}

fn read_u32(file: &mut File) -> Result<u32> {
    let mut buf = [0_u8; 4];
    file.read_exact(&mut buf)?;
    Ok(u32::from_le_bytes(buf))
}

fn read_u64(file: &mut File) -> Result<u64> {
    let mut buf = [0_u8; 8];
    file.read_exact(&mut buf)?;
    Ok(u64::from_le_bytes(buf))
}
