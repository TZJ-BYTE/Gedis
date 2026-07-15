use std::path::{Path, PathBuf};

use crate::error::{EngineError, Result};

#[derive(Debug, Clone, Copy, Eq, PartialEq, Default)]
pub enum SyncPolicy {
    #[default]
    None,
    Flush,
    SyncData,
}

#[derive(Debug, Clone)]
pub struct Options {
    pub data_dir: PathBuf,
    pub segment_size_bytes: u64,
    pub checkpoint_after_ops: u64,
    pub sync_policy: SyncPolicy,
}

impl Options {
    pub fn new<P: Into<PathBuf>>(data_dir: P) -> Self {
        Self {
            data_dir: data_dir.into(),
            ..Self::default()
        }
    }

    pub fn validate(&self) -> Result<()> {
        if self.data_dir.as_os_str().is_empty() {
            return Err(EngineError::InvalidOptions(
                "data_dir must not be empty".to_string(),
            ));
        }
        if self.segment_size_bytes < 4 * 1024 {
            return Err(EngineError::InvalidOptions(
                "segment_size_bytes must be at least 4KB".to_string(),
            ));
        }
        if self.checkpoint_after_ops == 0 {
            return Err(EngineError::InvalidOptions(
                "checkpoint_after_ops must be greater than 0".to_string(),
            ));
        }
        Ok(())
    }

    pub(crate) fn segments_dir(&self) -> PathBuf {
        self.data_dir.join("segments")
    }

    pub(crate) fn meta_dir(&self) -> PathBuf {
        self.data_dir.join("meta")
    }

    pub(crate) fn checkpoint_path(&self) -> PathBuf {
        self.meta_dir().join("checkpoint.bin")
    }

    pub(crate) fn segment_path(&self, segment_id: u64) -> PathBuf {
        self.segments_dir().join(format!("{segment_id:010}.seg"))
    }

    pub(crate) fn ensure_dirs(&self) -> Result<()> {
        self.validate()?;
        std::fs::create_dir_all(&self.data_dir)?;
        std::fs::create_dir_all(self.segments_dir())?;
        std::fs::create_dir_all(self.meta_dir())?;
        Ok(())
    }

    pub fn data_dir(&self) -> &Path {
        &self.data_dir
    }
}

impl Default for Options {
    fn default() -> Self {
        Self {
            data_dir: PathBuf::from("./engine_v2_data"),
            segment_size_bytes: 64 * 1024 * 1024,
            checkpoint_after_ops: 1024,
            sync_policy: SyncPolicy::None,
        }
    }
}
