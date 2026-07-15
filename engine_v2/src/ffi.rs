use std::ffi::{CStr, CString, c_char};
use std::ptr;

use crate::engine::{Engine, EngineStats};
use crate::iterator::EngineIterator;
use crate::options::{Options, SyncPolicy};
use crate::snapshot::EngineSnapshot;

#[repr(C)]
pub struct RedigoBuf {
    pub ptr: *mut u8,
    pub len: usize,
}

#[repr(C)]
pub struct RedigoStats {
    pub keys: u64,
    pub active_segment_id: u64,
    pub segment_count: u64,
    pub cached_readers: u64,
    pub pinned_segments: u64,
    pub pending_reclaims: u64,
}

pub struct SnapshotIterHandle {
    _snapshot: EngineSnapshot,
    iter: EngineIterator,
}

fn set_error(err_out: *mut RedigoBuf, message: String) {
    if err_out.is_null() {
        return;
    }
    unsafe {
        *err_out = bytes_to_buf(message.into_bytes());
    }
}

fn bytes_to_buf(bytes: Vec<u8>) -> RedigoBuf {
    let boxed = bytes.into_boxed_slice();
    let len = boxed.len();
    let ptr = Box::into_raw(boxed) as *mut u8;
    RedigoBuf { ptr, len }
}

fn stats_to_ffi(stats: EngineStats) -> RedigoStats {
    RedigoStats {
        keys: stats.keys as u64,
        active_segment_id: stats.active_segment_id,
        segment_count: stats.segment_count as u64,
        cached_readers: stats.cached_readers as u64,
        pinned_segments: stats.pinned_segments as u64,
        pending_reclaims: stats.pending_reclaims as u64,
    }
}

fn parse_sync_policy(raw: u32) -> SyncPolicy {
    match raw {
        1 => SyncPolicy::Flush,
        2 => SyncPolicy::SyncData,
        _ => SyncPolicy::None,
    }
}

unsafe fn bytes_from_raw<'a>(ptr: *const u8, len: usize) -> &'a [u8] {
    if ptr.is_null() || len == 0 {
        &[]
    } else {
        unsafe { std::slice::from_raw_parts(ptr, len) }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_buf_free(ptr: *mut u8, len: usize) {
    if ptr.is_null() || len == 0 {
        return;
    }
    unsafe {
        let slice_ptr = ptr::slice_from_raw_parts_mut(ptr, len);
        drop(Box::from_raw(slice_ptr));
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_open(
    data_dir: *const c_char,
    segment_size_bytes: u64,
    checkpoint_after_ops: u64,
    sync_policy: u32,
    err_out: *mut RedigoBuf,
) -> *mut Engine {
    if data_dir.is_null() {
        set_error(err_out, "data_dir must not be null".to_string());
        return ptr::null_mut();
    }

    let data_dir = unsafe { CStr::from_ptr(data_dir) };
    let Ok(data_dir) = data_dir.to_str() else {
        set_error(err_out, "data_dir must be valid UTF-8".to_string());
        return ptr::null_mut();
    };

    let mut options = Options::new(data_dir);
    options.segment_size_bytes = segment_size_bytes;
    options.checkpoint_after_ops = checkpoint_after_ops;
    options.sync_policy = parse_sync_policy(sync_policy);

    match Engine::open(options) {
        Ok(engine) => Box::into_raw(Box::new(engine)),
        Err(err) => {
            set_error(err_out, err.to_string());
            ptr::null_mut()
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_close(engine: *mut Engine, err_out: *mut RedigoBuf) -> i32 {
    if engine.is_null() {
        return 0;
    }

    let engine = unsafe { Box::from_raw(engine) };
    match engine.close() {
        Ok(()) => 0,
        Err(err) => {
            set_error(err_out, err.to_string());
            -1
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_put(
    engine: *mut Engine,
    key_ptr: *const u8,
    key_len: usize,
    value_ptr: *const u8,
    value_len: usize,
    err_out: *mut RedigoBuf,
) -> i32 {
    let Some(engine) = (unsafe { engine.as_ref() }) else {
        set_error(err_out, "engine handle must not be null".to_string());
        return -1;
    };

    let key = unsafe { bytes_from_raw(key_ptr, key_len) };
    let value = unsafe { bytes_from_raw(value_ptr, value_len) };
    match engine.put(key, value) {
        Ok(()) => 0,
        Err(err) => {
            set_error(err_out, err.to_string());
            -1
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_get(
    engine: *mut Engine,
    key_ptr: *const u8,
    key_len: usize,
    value_out: *mut RedigoBuf,
    err_out: *mut RedigoBuf,
) -> i32 {
    let Some(engine) = (unsafe { engine.as_ref() }) else {
        set_error(err_out, "engine handle must not be null".to_string());
        return -1;
    };

    let key = unsafe { bytes_from_raw(key_ptr, key_len) };
    match engine.get(key) {
        Ok(Some(value)) => {
            if !value_out.is_null() {
                unsafe {
                    *value_out = bytes_to_buf(value);
                }
            }
            1
        }
        Ok(None) => 0,
        Err(err) => {
            set_error(err_out, err.to_string());
            -1
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_delete(
    engine: *mut Engine,
    key_ptr: *const u8,
    key_len: usize,
    err_out: *mut RedigoBuf,
) -> i32 {
    let Some(engine) = (unsafe { engine.as_ref() }) else {
        set_error(err_out, "engine handle must not be null".to_string());
        return -1;
    };

    let key = unsafe { bytes_from_raw(key_ptr, key_len) };
    match engine.delete(key) {
        Ok(()) => 0,
        Err(err) => {
            set_error(err_out, err.to_string());
            -1
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_compact_until_stable(
    engine: *mut Engine,
    max_rounds: usize,
    rewritten_out: *mut u64,
    err_out: *mut RedigoBuf,
) -> i32 {
    let Some(engine) = (unsafe { engine.as_ref() }) else {
        set_error(err_out, "engine handle must not be null".to_string());
        return -1;
    };

    match engine.compact_until_stable(max_rounds) {
        Ok(rewritten) => {
            if !rewritten_out.is_null() {
                unsafe {
                    *rewritten_out = rewritten as u64;
                }
            }
            0
        }
        Err(err) => {
            set_error(err_out, err.to_string());
            -1
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_stats(
    engine: *mut Engine,
    stats_out: *mut RedigoStats,
    err_out: *mut RedigoBuf,
) -> i32 {
    let Some(engine) = (unsafe { engine.as_ref() }) else {
        set_error(err_out, "engine handle must not be null".to_string());
        return -1;
    };

    if stats_out.is_null() {
        set_error(err_out, "stats_out must not be null".to_string());
        return -1;
    }

    unsafe {
        *stats_out = stats_to_ffi(engine.stats());
    }
    0
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_iter_open(
    engine: *mut Engine,
    err_out: *mut RedigoBuf,
) -> *mut SnapshotIterHandle {
    let Some(engine) = (unsafe { engine.as_ref() }) else {
        set_error(err_out, "engine handle must not be null".to_string());
        return ptr::null_mut();
    };

    let snapshot = engine.snapshot();
    let iter = snapshot.iter();
    Box::into_raw(Box::new(SnapshotIterHandle {
        _snapshot: snapshot,
        iter,
    }))
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_iter_next(
    iter_handle: *mut SnapshotIterHandle,
    key_out: *mut RedigoBuf,
    value_out: *mut RedigoBuf,
    err_out: *mut RedigoBuf,
) -> i32 {
    let Some(iter_handle) = (unsafe { iter_handle.as_mut() }) else {
        set_error(err_out, "iterator handle must not be null".to_string());
        return -1;
    };

    match iter_handle.iter.next_entry() {
        Ok(Some((key, value))) => {
            unsafe {
                if !key_out.is_null() {
                    *key_out = bytes_to_buf(key);
                }
                if !value_out.is_null() {
                    *value_out = bytes_to_buf(value);
                }
            }
            1
        }
        Ok(None) => 0,
        Err(err) => {
            set_error(err_out, err.to_string());
            -1
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_iter_close(iter_handle: *mut SnapshotIterHandle) {
    if iter_handle.is_null() {
        return;
    }
    unsafe {
        drop(Box::from_raw(iter_handle));
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_engine_version() -> *mut c_char {
    CString::new("engine_v2").unwrap().into_raw()
}

#[unsafe(no_mangle)]
pub extern "C" fn redigo_cstring_free(ptr: *mut c_char) {
    if ptr.is_null() {
        return;
    }
    unsafe {
        drop(CString::from_raw(ptr));
    }
}
