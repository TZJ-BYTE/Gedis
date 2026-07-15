#[derive(Debug, Clone, Eq, PartialEq)]
pub enum WriteOp {
    Put(Vec<u8>, Vec<u8>),
    Delete(Vec<u8>),
}

impl WriteOp {
    pub fn put(key: impl Into<Vec<u8>>, value: impl Into<Vec<u8>>) -> Self {
        Self::Put(key.into(), value.into())
    }

    pub fn delete(key: impl Into<Vec<u8>>) -> Self {
        Self::Delete(key.into())
    }
}
