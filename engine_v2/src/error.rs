use std::fmt::{Display, Formatter};
use std::io;

#[derive(Debug)]
pub enum EngineError {
    Io(io::Error),
    Corruption(String),
    InvalidOptions(String),
    SegmentNotFound(u64),
}

impl Display for EngineError {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Io(err) => write!(f, "io error: {err}"),
            Self::Corruption(msg) => write!(f, "corruption: {msg}"),
            Self::InvalidOptions(msg) => write!(f, "invalid options: {msg}"),
            Self::SegmentNotFound(id) => write!(f, "segment not found: {id}"),
        }
    }
}

impl std::error::Error for EngineError {}

impl From<io::Error> for EngineError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

pub type Result<T> = std::result::Result<T, EngineError>;
