use std::collections::BTreeMap;
use std::fs::File;
use std::path::Path;

use crate::error::{EngineError, Result};

#[derive(Debug, Default)]
pub struct ReaderCache {
    files: BTreeMap<u64, File>,
}

impl ReaderCache {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn open_clone(&mut self, segment_id: u64, path: &Path) -> Result<File> {
        if let Some(file) = self.files.get(&segment_id) {
            return Ok(file.try_clone()?);
        }

        let file = File::open(path).map_err(|err| {
            if err.kind() == std::io::ErrorKind::NotFound {
                EngineError::SegmentNotFound(segment_id)
            } else {
                EngineError::Io(err)
            }
        })?;

        let clone = file.try_clone()?;
        self.files.insert(segment_id, file);
        Ok(clone)
    }

    pub fn remove(&mut self, segment_id: u64) {
        self.files.remove(&segment_id);
    }

    pub fn clear(&mut self) {
        self.files.clear();
    }

    pub(crate) fn len(&self) -> usize {
        self.files.len()
    }
}
