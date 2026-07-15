use std::collections::BTreeMap;
use std::fs;
use std::fs::File;
use std::io::Write;
use std::sync::{Arc, Mutex, RwLock};

use crate::batch::WriteOp;
use crate::checkpoint::{self, CheckpointState};
use crate::error::Result;
use crate::iterator::EngineIterator;
use crate::options::{Options, SyncPolicy};
use crate::reader_cache::ReaderCache;
use crate::record::{
    RECORD_HEADER_LEN, ReadStatus, Record, RecordKind, ValueLocation, read_record,
};
use crate::segment::{
    list_segment_ids, open_active_segment, read_value, scan_segment, truncate_segment,
};
use crate::snapshot::{EngineSnapshot, SnapshotRegistry};

#[derive(Debug)]
struct EngineState {
    index: BTreeMap<Vec<u8>, ValueLocation>,
    active_segment_id: u64,
    active_len: u64,
    active_file: File,
    ops_since_checkpoint: u64,
}

#[derive(Debug)]
pub struct Engine {
    options: Options,
    inner: RwLock<EngineState>,
    reader_cache: Arc<Mutex<ReaderCache>>,
    snapshot_registry: Arc<SnapshotRegistry>,
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct EngineStats {
    pub keys: usize,
    pub active_segment_id: u64,
    pub segment_count: usize,
    pub cached_readers: usize,
    pub pinned_segments: usize,
    pub pending_reclaims: usize,
}

impl Engine {
    pub fn open(options: Options) -> Result<Self> {
        options.ensure_dirs()?;

        let checkpoint = checkpoint::load(&options.checkpoint_path()).ok().flatten();

        let mut index = checkpoint
            .as_ref()
            .map(|state| state.index.clone())
            .unwrap_or_default();
        let checkpoint_segment_id = checkpoint
            .as_ref()
            .map(|state| state.active_segment_id)
            .unwrap_or(0);
        let checkpoint_offset = checkpoint
            .as_ref()
            .map(|state| state.active_offset)
            .unwrap_or(0);

        let segment_ids = list_segment_ids(&options.segments_dir())?;

        let (active_segment_id, active_len) = if segment_ids.is_empty() {
            let path = options.segment_path(1);
            let file = open_active_segment(&path)?;
            drop(file);
            (1, 0)
        } else {
            let active_segment_id = *segment_ids.last().expect("segment list is not empty");
            let mut active_len = 0_u64;

            for segment_id in segment_ids {
                if checkpoint_segment_id > 0 && segment_id < checkpoint_segment_id {
                    continue;
                }

                let start_offset = if segment_id == checkpoint_segment_id {
                    checkpoint_offset
                } else {
                    0
                };

                let path = options.segment_path(segment_id);
                let summary = scan_segment(&path, segment_id, start_offset, &mut index)?;
                if segment_id == active_segment_id {
                    truncate_segment(&path, summary.valid_end)?;
                    active_len = summary.valid_end;
                }
            }

            (active_segment_id, active_len)
        };

        let active_file = open_active_segment(&options.segment_path(active_segment_id))?;

        Ok(Self {
            options,
            inner: RwLock::new(EngineState {
                index,
                active_segment_id,
                active_len,
                active_file,
                ops_since_checkpoint: 0,
            }),
            reader_cache: Arc::new(Mutex::new(ReaderCache::new())),
            snapshot_registry: Arc::new(SnapshotRegistry::new()),
        })
    }

    pub fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        let mut state = self.inner.write().expect("engine lock poisoned");
        let record = Record::put(key, value);
        let location = self.append_record_locked(&mut state, &record)?;
        state.index.insert(key.to_vec(), location);
        state.ops_since_checkpoint += 1;
        self.maybe_checkpoint_locked(&mut state)?;
        Ok(())
    }

    pub fn delete(&self, key: &[u8]) -> Result<()> {
        let mut state = self.inner.write().expect("engine lock poisoned");
        let record = Record::delete(key);
        self.append_record_locked(&mut state, &record)?;
        state.index.remove(key);
        state.ops_since_checkpoint += 1;
        self.maybe_checkpoint_locked(&mut state)?;
        Ok(())
    }

    pub fn write_batch(&self, operations: &[WriteOp]) -> Result<()> {
        if operations.is_empty() {
            return Ok(());
        }

        let mut state = self.inner.write().expect("engine lock poisoned");
        for operation in operations {
            match operation {
                WriteOp::Put(key, value) => {
                    let record = Record::put(key, value);
                    let location = self.append_record_locked(&mut state, &record)?;
                    state.index.insert(key.clone(), location);
                }
                WriteOp::Delete(key) => {
                    let record = Record::delete(key);
                    self.append_record_locked(&mut state, &record)?;
                    state.index.remove(key.as_slice());
                }
            }
        }

        state.ops_since_checkpoint += operations.len() as u64;
        self.maybe_checkpoint_locked(&mut state)?;
        Ok(())
    }

    pub fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>> {
        let location = {
            let state = self.inner.read().expect("engine lock poisoned");
            state.index.get(key).cloned()
        };

        let Some(location) = location else {
            return Ok(None);
        };

        let path = self.options.segment_path(location.segment_id);
        let mut file = self
            .reader_cache
            .lock()
            .expect("reader cache lock poisoned")
            .open_clone(location.segment_id, &path)?;
        Ok(Some(read_value(&mut file, &location)?))
    }

    pub fn contains_key(&self, key: &[u8]) -> bool {
        let state = self.inner.read().expect("engine lock poisoned");
        state.index.contains_key(key)
    }

    pub fn len(&self) -> usize {
        let state = self.inner.read().expect("engine lock poisoned");
        state.index.len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    pub fn sync(&self) -> Result<()> {
        let mut state = self.inner.write().expect("engine lock poisoned");
        Self::apply_sync_policy(&mut state.active_file, SyncPolicy::SyncData)
    }

    pub fn checkpoint(&self) -> Result<()> {
        let mut state = self.inner.write().expect("engine lock poisoned");
        self.write_checkpoint_locked(&mut state)
    }

    pub fn snapshot(&self) -> EngineSnapshot {
        let state = self.inner.read().expect("engine lock poisoned");
        let entries = state
            .index
            .iter()
            .map(|(key, location)| (key.clone(), location.clone()))
            .collect();

        EngineSnapshot::new(
            &self.options,
            Arc::clone(&self.reader_cache),
            Arc::clone(&self.snapshot_registry),
            entries,
        )
    }

    pub fn stats(&self) -> EngineStats {
        let state = self.inner.read().expect("engine lock poisoned");
        let segment_count = list_segment_ids(&self.options.segments_dir())
            .map(|segments| segments.len())
            .unwrap_or(0);
        let cached_readers = self
            .reader_cache
            .lock()
            .expect("reader cache lock poisoned")
            .len();
        let (pinned_segments, pending_reclaims) = self.snapshot_registry.stats();

        EngineStats {
            keys: state.index.len(),
            active_segment_id: state.active_segment_id,
            segment_count,
            cached_readers,
            pinned_segments,
            pending_reclaims,
        }
    }

    pub fn compact_once(&self) -> Result<usize> {
        let mut state = self.inner.write().expect("engine lock poisoned");
        let active_segment_id = state.active_segment_id;
        let old_segments: Vec<u64> = list_segment_ids(&self.options.segments_dir())?
            .into_iter()
            .filter(|segment_id| *segment_id < active_segment_id)
            .collect();

        if old_segments.is_empty() {
            return Ok(0);
        }

        let mut rewritten = 0_usize;
        let mut reclaimable_segments = Vec::with_capacity(old_segments.len());

        for segment_id in old_segments {
            let mut file = File::open(self.options.segment_path(segment_id))?;
            let mut offset = 0_u64;

            loop {
                let (status, parsed) = read_record(&mut file, offset)?;
                match status {
                    ReadStatus::EndOfFile | ReadStatus::TruncatedTail => {
                        break;
                    }
                    ReadStatus::Complete => {
                        let record = parsed.expect("complete status must carry a record");
                        let current_location = state.index.get(record.key.as_slice());
                        let is_live_entry = matches!(
                            current_location,
                            Some(location)
                                if location.segment_id == segment_id && location.record_offset == offset
                        );

                        if is_live_entry && record.kind == RecordKind::Put {
                            let rewritten_record = Record::put(&record.key, &record.value);
                            let new_location =
                                self.append_record_locked(&mut state, &rewritten_record)?;
                            state.index.insert(record.key.clone(), new_location);
                            rewritten += 1;
                        }

                        offset = record.next_offset;
                    }
                }
            }

            reclaimable_segments.push(segment_id);
        }

        if rewritten > 0 {
            state.ops_since_checkpoint += rewritten as u64;
            self.write_checkpoint_locked(&mut state)?;
        }

        for segment_id in reclaimable_segments {
            if self.snapshot_registry.schedule_delete(segment_id) {
                self.delete_segment_file(segment_id)?;
            }
        }

        Ok(rewritten)
    }

    pub fn compact_until_stable(&self, max_rounds: usize) -> Result<usize> {
        let mut total_rewritten = 0_usize;

        for _ in 0..max_rounds {
            let segment_count = list_segment_ids(&self.options.segments_dir())?.len();
            if segment_count <= 1 {
                break;
            }

            let rewritten = self.compact_once()?;
            total_rewritten += rewritten;

            if rewritten == 0 {
                break;
            }
        }

        Ok(total_rewritten)
    }

    pub fn close(&self) -> Result<()> {
        self.write_checkpoint_locked(&mut self.inner.write().expect("engine lock poisoned"))?;
        self.reader_cache
            .lock()
            .expect("reader cache lock poisoned")
            .clear();
        Ok(())
    }

    pub fn iter(&self) -> EngineIterator {
        let state = self.inner.read().expect("engine lock poisoned");
        let entries = state
            .index
            .iter()
            .map(|(key, location)| (key.clone(), location.clone()))
            .collect();
        EngineIterator::new(&self.options, Arc::clone(&self.reader_cache), entries)
    }

    fn append_record_locked(
        &self,
        state: &mut EngineState,
        record: &Record,
    ) -> Result<ValueLocation> {
        let record_len = record.encoded_len() as u64;
        if state.active_len > 0 && state.active_len + record_len > self.options.segment_size_bytes {
            self.rotate_segment_locked(state)?;
        }

        let record_offset = state.active_len;
        let encoded = record.encode();
        state.active_file.write_all(&encoded)?;
        Self::apply_sync_policy(&mut state.active_file, self.options.sync_policy)?;
        state.active_len += encoded.len() as u64;

        Ok(ValueLocation {
            segment_id: state.active_segment_id,
            record_offset,
            value_offset: record_offset + RECORD_HEADER_LEN as u64 + record.key.len() as u64,
            value_len: record.value.len() as u32,
        })
    }

    fn rotate_segment_locked(&self, state: &mut EngineState) -> Result<()> {
        state.active_file.flush()?;
        if matches!(self.options.sync_policy, SyncPolicy::SyncData) {
            state.active_file.sync_data()?;
        }

        state.active_segment_id += 1;
        state.active_len = 0;
        state.active_file =
            open_active_segment(&self.options.segment_path(state.active_segment_id))?;
        Ok(())
    }

    fn maybe_checkpoint_locked(&self, state: &mut EngineState) -> Result<()> {
        if state.ops_since_checkpoint >= self.options.checkpoint_after_ops {
            self.write_checkpoint_locked(state)?;
        }
        Ok(())
    }

    fn write_checkpoint_locked(&self, state: &mut EngineState) -> Result<()> {
        state.active_file.flush()?;
        if matches!(self.options.sync_policy, SyncPolicy::SyncData) {
            state.active_file.sync_data()?;
        }

        let checkpoint = CheckpointState {
            active_segment_id: state.active_segment_id,
            active_offset: state.active_len,
            index: state.index.clone(),
        };
        checkpoint::write(&self.options.checkpoint_path(), &checkpoint)?;
        state.ops_since_checkpoint = 0;
        Ok(())
    }

    fn apply_sync_policy(file: &mut File, policy: SyncPolicy) -> Result<()> {
        match policy {
            SyncPolicy::None => {}
            SyncPolicy::Flush => {
                file.flush()?;
            }
            SyncPolicy::SyncData => {
                file.flush()?;
                file.sync_data()?;
            }
        }
        Ok(())
    }

    fn delete_segment_file(&self, segment_id: u64) -> Result<()> {
        let path = self.options.segment_path(segment_id);
        self.reader_cache
            .lock()
            .expect("reader cache lock poisoned")
            .remove(segment_id);
        if path.exists() {
            fs::remove_file(path)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    use super::*;

    fn temp_dir(name: &str) -> PathBuf {
        let unique = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time went backwards")
            .as_nanos();
        std::env::temp_dir().join(format!("redigo_engine_v2_{name}_{unique}"))
    }

    #[test]
    fn put_get_delete_roundtrip() {
        let dir = temp_dir("roundtrip");
        let options = Options::new(&dir);
        let engine = Engine::open(options).expect("open engine");

        engine.put(b"alpha", b"one").expect("put alpha");
        engine.put(b"beta", b"two").expect("put beta");

        assert_eq!(
            engine.get(b"alpha").expect("get alpha"),
            Some(b"one".to_vec())
        );
        assert_eq!(
            engine.get(b"beta").expect("get beta"),
            Some(b"two".to_vec())
        );

        engine.delete(b"alpha").expect("delete alpha");
        assert_eq!(engine.get(b"alpha").expect("get deleted"), None);

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn recovers_after_reopen() {
        let dir = temp_dir("reopen");
        {
            let mut options = Options::new(&dir);
            options.checkpoint_after_ops = 2;
            let engine = Engine::open(options).expect("open engine");
            engine.put(b"k1", b"v1").expect("put k1");
            engine.put(b"k2", b"v2").expect("put k2");
            engine.put(b"k3", b"v3").expect("put k3");
            engine.checkpoint().expect("checkpoint");
        }

        let engine = Engine::open(Options::new(&dir)).expect("reopen engine");
        assert_eq!(engine.get(b"k1").expect("get k1"), Some(b"v1".to_vec()));
        assert_eq!(engine.get(b"k2").expect("get k2"), Some(b"v2".to_vec()));
        assert_eq!(engine.get(b"k3").expect("get k3"), Some(b"v3".to_vec()));

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn rotates_segments_and_keeps_latest_value() {
        let dir = temp_dir("rotate");
        let mut options = Options::new(&dir);
        options.segment_size_bytes = 4 * 1024;
        options.checkpoint_after_ops = 10;
        let engine = Engine::open(options).expect("open engine");

        for idx in 0..20 {
            let key = format!("k{:02}", idx % 4);
            let value = format!("value-{idx:02}-{}", "x".repeat(512));
            engine.put(key.as_bytes(), value.as_bytes()).expect("put");
        }

        let latest = engine
            .get(b"k00")
            .expect("get latest")
            .expect("value should exist");
        assert!(latest.starts_with(b"value-16-"));

        let segment_count = list_segment_ids(&dir.join("segments"))
            .expect("list segments")
            .len();
        assert!(segment_count > 1, "expected rotated segments");

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn truncates_partial_tail_on_recovery() {
        let dir = temp_dir("truncate_tail");
        {
            let engine = Engine::open(Options::new(&dir)).expect("open engine");
            engine.put(b"good", b"value").expect("put good");
            engine.close().expect("close engine");
        }

        let active_path = dir.join("segments").join("0000000001.seg");
        {
            let mut file = std::fs::OpenOptions::new()
                .append(true)
                .open(&active_path)
                .expect("open segment");
            file.write_all(b"RDG2junk").expect("write partial garbage");
            file.flush().expect("flush partial");
        }

        let engine = Engine::open(Options::new(&dir)).expect("reopen after tail corruption");
        assert_eq!(
            engine.get(b"good").expect("read recovered value"),
            Some(b"value".to_vec())
        );

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn write_batch_updates_index_atomically_in_memory() {
        let dir = temp_dir("batch");
        let engine = Engine::open(Options::new(&dir)).expect("open engine");

        let batch = vec![
            WriteOp::put("alpha", "one"),
            WriteOp::put("beta", "two"),
            WriteOp::put("alpha", "three"),
            WriteOp::delete("beta"),
        ];
        engine.write_batch(&batch).expect("write batch");

        assert_eq!(
            engine.get(b"alpha").expect("get alpha"),
            Some(b"three".to_vec())
        );
        assert_eq!(engine.get(b"beta").expect("get beta"), None);
        assert_eq!(engine.len(), 1);

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn compact_once_rewrites_live_entries_and_reclaims_old_segments() {
        let dir = temp_dir("compact_once");
        let mut options = Options::new(&dir);
        options.segment_size_bytes = 4 * 1024;
        options.checkpoint_after_ops = 100;
        let engine = Engine::open(options).expect("open engine");

        for idx in 0..12 {
            let key = format!("key-{idx:02}");
            let value = format!("value-{idx:02}-{}", "x".repeat(512));
            engine.put(key.as_bytes(), value.as_bytes()).expect("put");
        }
        let before_segments = list_segment_ids(&dir.join("segments")).expect("list segments");
        let original_active = *before_segments
            .last()
            .expect("segments should not be empty");
        assert!(
            before_segments.len() > 1,
            "expected multiple segments before compaction"
        );

        let rewritten = engine.compact_once().expect("compact once");
        assert!(rewritten > 0, "expected cleaner to rewrite live entries");

        for idx in 0..12 {
            let key = format!("key-{idx:02}");
            let expected = format!("value-{idx:02}-{}", "x".repeat(512)).into_bytes();
            assert_eq!(
                engine.get(key.as_bytes()).expect("get after compaction"),
                Some(expected)
            );
        }

        let after_segments = list_segment_ids(&dir.join("segments")).expect("list segments after");
        assert!(
            after_segments
                .iter()
                .all(|segment_id| *segment_id >= original_active),
            "expected all pre-active segments to be reclaimed"
        );
        assert!(
            !dir.join("segments").join("0000000001.seg").exists(),
            "expected oldest segment to be reclaimed"
        );

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn reader_cache_reuses_opened_segment_and_evicts_reclaimed_files() {
        let dir = temp_dir("reader_cache");
        let mut options = Options::new(&dir);
        options.segment_size_bytes = 4 * 1024;
        options.checkpoint_after_ops = 100;
        let engine = Engine::open(options).expect("open engine");

        for idx in 0..8 {
            let key = format!("key-{idx:02}");
            let value = format!("value-{idx:02}-{}", "x".repeat(512));
            engine.put(key.as_bytes(), value.as_bytes()).expect("put");
        }

        assert_eq!(
            engine.get(b"key-00").expect("first read"),
            Some(format!("value-00-{}", "x".repeat(512)).into_bytes())
        );
        assert_eq!(
            engine
                .reader_cache
                .lock()
                .expect("reader cache lock poisoned")
                .len(),
            1
        );

        assert_eq!(
            engine.get(b"key-00").expect("second read"),
            Some(format!("value-00-{}", "x".repeat(512)).into_bytes())
        );
        assert_eq!(
            engine
                .reader_cache
                .lock()
                .expect("reader cache lock poisoned")
                .len(),
            1
        );

        engine.compact_once().expect("compact once");
        assert_eq!(
            engine
                .reader_cache
                .lock()
                .expect("reader cache lock poisoned")
                .len(),
            0
        );

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn snapshot_preserves_old_view_until_release() {
        let dir = temp_dir("snapshot");
        let mut options = Options::new(&dir);
        options.segment_size_bytes = 4 * 1024;
        options.checkpoint_after_ops = 100;
        let engine = Engine::open(options).expect("open engine");

        for idx in 0..6 {
            let key = format!("filler-{idx:02}");
            let value = format!("fill-{idx:02}-{}", "x".repeat(512));
            engine
                .put(key.as_bytes(), value.as_bytes())
                .expect("put filler");
        }
        engine.put(b"alpha", b"old-value").expect("put old value");
        for idx in 0..6 {
            let key = format!("tail-{idx:02}");
            let value = format!("tail-{idx:02}-{}", "y".repeat(512));
            engine
                .put(key.as_bytes(), value.as_bytes())
                .expect("put tail");
        }

        let snapshot = engine.snapshot();
        assert_eq!(
            snapshot.get(b"alpha").expect("snapshot get old"),
            Some(b"old-value".to_vec())
        );

        engine.put(b"alpha", b"new-value").expect("put new value");
        assert_eq!(
            engine.get(b"alpha").expect("engine get new"),
            Some(b"new-value".to_vec())
        );

        let before = engine.stats();
        assert!(before.pinned_segments > 0, "expected pinned segments");

        let rewritten = engine.compact_once().expect("compact with snapshot");
        assert!(rewritten > 0, "expected live entries rewritten");
        assert_eq!(
            snapshot.get(b"alpha").expect("snapshot get preserved"),
            Some(b"old-value".to_vec())
        );

        let during = engine.stats();
        assert!(
            during.pending_reclaims > 0,
            "expected old segments to wait for snapshot release"
        );
        assert!(
            dir.join("segments").join("0000000001.seg").exists(),
            "expected old segment to stay while snapshot is alive"
        );

        drop(snapshot);

        let after = engine.stats();
        assert_eq!(after.pinned_segments, 0);
        assert_eq!(after.pending_reclaims, 0);
        assert!(
            !dir.join("segments").join("0000000001.seg").exists(),
            "expected delayed segment deletion after snapshot release"
        );

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }

    #[test]
    fn compact_until_stable_converges_to_single_segment() {
        let dir = temp_dir("compact_stable");
        let mut options = Options::new(&dir);
        options.segment_size_bytes = 4 * 1024;
        options.checkpoint_after_ops = 100;
        let engine = Engine::open(options).expect("open engine");

        for idx in 0..24 {
            let key = format!("key-{:02}", idx % 6);
            let value = format!("value-{idx:02}-{}", "z".repeat(512));
            engine.put(key.as_bytes(), value.as_bytes()).expect("put");
        }

        let before = engine.stats();
        assert!(
            before.segment_count > 1,
            "expected multiple segments before compaction"
        );

        let rewritten = engine
            .compact_until_stable(16)
            .expect("compact until stable");
        assert!(rewritten > 0, "expected some entries to be rewritten");

        let after = engine.stats();
        assert_eq!(
            after.segment_count, 1,
            "expected single segment after stabilization"
        );

        for idx in 0..6 {
            let key = format!("key-{:02}", idx);
            let expected = format!("value-{:02}-{}", 18 + idx, "z".repeat(512)).into_bytes();
            assert_eq!(
                engine
                    .get(key.as_bytes())
                    .expect("get after stable compaction"),
                Some(expected)
            );
        }

        std::fs::remove_dir_all(dir).expect("cleanup temp dir");
    }
}
