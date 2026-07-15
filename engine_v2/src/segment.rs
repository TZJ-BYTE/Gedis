use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, SeekFrom};
use std::path::Path;

use crate::error::Result;
use crate::record::{RECORD_HEADER_LEN, ReadStatus, RecordKind, ValueLocation, read_record};

#[derive(Debug, Clone, Copy)]
pub struct ScanSummary {
    pub valid_end: u64,
}

pub fn list_segment_ids(segments_dir: &Path) -> Result<Vec<u64>> {
    let mut ids = Vec::new();
    for entry in std::fs::read_dir(segments_dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.extension().and_then(|ext| ext.to_str()) != Some("seg") {
            continue;
        }
        let Some(stem) = path.file_stem().and_then(|name| name.to_str()) else {
            continue;
        };
        let Ok(id) = stem.parse::<u64>() else {
            continue;
        };
        ids.push(id);
    }
    ids.sort_unstable();
    Ok(ids)
}

pub fn open_active_segment(path: &Path) -> Result<File> {
    Ok(OpenOptions::new()
        .create(true)
        .read(true)
        .append(true)
        .open(path)?)
}

pub fn scan_segment(
    path: &Path,
    segment_id: u64,
    start_offset: u64,
    index: &mut BTreeMap<Vec<u8>, ValueLocation>,
) -> Result<ScanSummary> {
    let mut file = File::open(path)?;
    file.seek(SeekFrom::Start(start_offset))?;

    let mut offset = start_offset;
    loop {
        let (status, parsed) = read_record(&mut file, offset)?;
        match status {
            ReadStatus::EndOfFile => {
                break;
            }
            ReadStatus::TruncatedTail => {
                break;
            }
            ReadStatus::Complete => {
                let record = parsed.expect("complete status must carry a record");
                match record.kind {
                    RecordKind::Put => {
                        let key_len = record.key.len() as u64;
                        let value_offset = offset + RECORD_HEADER_LEN as u64 + key_len;
                        index.insert(
                            record.key,
                            ValueLocation {
                                segment_id,
                                record_offset: offset,
                                value_offset,
                                value_len: record.value.len() as u32,
                            },
                        );
                    }
                    RecordKind::Delete => {
                        index.remove(&record.key);
                    }
                }

                offset = record.next_offset;
            }
        }
    }

    Ok(ScanSummary { valid_end: offset })
}

pub fn truncate_segment(path: &Path, valid_end: u64) -> Result<()> {
    let file = OpenOptions::new().write(true).open(path)?;
    file.set_len(valid_end)?;
    Ok(())
}

pub fn read_value(file: &mut File, location: &ValueLocation) -> Result<Vec<u8>> {
    file.seek(SeekFrom::Start(location.value_offset))?;
    let mut value = vec![0_u8; location.value_len as usize];
    file.read_exact(&mut value)?;
    Ok(value)
}
