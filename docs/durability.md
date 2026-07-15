# Durability & Recovery Semantics

## Write Success Semantics

This project implements Redis-compatible commands on top of an internal WAL + LSM-tree (and optional value log) persistence stack.

When persistence is enabled:

- A successful write command means the mutation is appended to WAL and flushed from the process buffer to the OS file buffer.
- If WAL sync is enabled, a successful write also means the WAL has been fsync-ed to stable storage.
- MemTable updates happen after the WAL append succeeds.

## Crash Recovery Order

On startup, the engine recovers in this order:

1. Open version metadata (CURRENT/MANIFEST) to locate existing SSTables.
2. Replay WAL to rebuild the in-memory mutable MemTable.
3. If the recovered MemTable is large, flush it into a new SSTable and update the version metadata.

## Flush Semantics

`FLUSHDB` is a hard reset when persistence is enabled:

- Clears in-memory state.
- Resets WAL, SSTable, version metadata and value log directories under the DB path.
- After `FLUSHDB`, deleted data will not reappear after restart.

