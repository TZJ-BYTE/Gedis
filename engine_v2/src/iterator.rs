use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use crate::error::Result;
use crate::options::Options;
use crate::reader_cache::ReaderCache;
use crate::record::ValueLocation;
use crate::segment::read_value;

#[derive(Debug, Clone)]
pub struct EngineIterator {
    data_dir: PathBuf,
    reader_cache: Arc<Mutex<ReaderCache>>,
    entries: Vec<(Vec<u8>, ValueLocation)>,
    position: usize,
}

impl EngineIterator {
    pub(crate) fn new(
        options: &Options,
        reader_cache: Arc<Mutex<ReaderCache>>,
        entries: Vec<(Vec<u8>, ValueLocation)>,
    ) -> Self {
        Self {
            data_dir: options.data_dir.clone(),
            reader_cache,
            entries,
            position: 0,
        }
    }

    pub fn next_entry(&mut self) -> Result<Option<(Vec<u8>, Vec<u8>)>> {
        if self.position >= self.entries.len() {
            return Ok(None);
        }

        let (key, location) = &self.entries[self.position];
        self.position += 1;

        let path = self
            .data_dir
            .join("segments")
            .join(format!("{:010}.seg", location.segment_id));
        let mut file = self
            .reader_cache
            .lock()
            .expect("reader cache lock poisoned")
            .open_clone(location.segment_id, &path)?;
        let value = read_value(&mut file, location)?;

        Ok(Some((key.clone(), value)))
    }
}
