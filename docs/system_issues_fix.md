# 系统级问题排查与修复建议（一次性清点）

更新时间：2026-03-18

本文聚焦“系统级问题”：可能导致数据丢失/不一致、崩溃恢复异常、资源耗尽、协议层 DoS、长稳运行不可运维的问题。对于仅影响局部命令语义的小差异，不在本文主清单内。

## 已修复（本轮已落地）

- **FLUSHDB/FLUSHALL 在开启 LSM 时不会回潮**：清库触发 LSM Reset，重置 WAL/SSTable/version/vLog，并补齐竞态防护。
  - 参考：[database.go](file:///d:/project/go/RediGo/internal/database/database.go)、[lsm\_engine.go](file:///d:/project/go/RediGo/internal/persistence/lsm_engine.go)、[flushall.go](file:///d:/project/go/RediGo/internal/command/flushall.go)
- **RESP2 空值/数组 nil 语义**：`$-1` 与数组中包含 nil 元素的编码已修复。
  - 参考：[parser.go](file:///d:/project/go/RediGo/internal/protocol/parser.go)
- **RESP 协议最大长度限制（防 DoS）**：新增配置 `resp_max_bulk_len` 与 `resp_max_array_len`，默认开启；超限直接返回协议错误并拒绝继续解析。
  - 参考：[config.yaml](file:///d:/project/go/RediGo/config.yaml)、[parser.go](file:///d:/project/go/RediGo/internal/protocol/parser.go)、[factory.go](file:///d:/project/go/RediGo/internal/server/factory.go)
- **LSM MemTableSize 配置一致化**：MemTable 创建与刷写阈值统一使用 `options.MemTableSize`，移除硬编码 4MB 与错误的 `NewMemTable(3)`。
  - 参考：[options.go](file:///d:/project/go/RediGo/internal/persistence/options.go)、[lsm_engine.go](file:///d:/project/go/RediGo/internal/persistence/lsm_engine.go)
- **SSTable KeyRange 元数据补齐**：Flush/Compaction 生成的 `FileMetadata` 会写入 `SmallestKey/LargestKey`，并在 MANIFEST 恢复时对缺失 keyrange 做回填。
  - 参考：[sstable_builder.go](file:///d:/project/go/RediGo/internal/persistence/sstable_builder.go)、[lsm_engine.go](file:///d:/project/go/RediGo/internal/persistence/lsm_engine.go)、[compaction.go](file:///d:/project/go/RediGo/internal/persistence/compaction.go)、[version_set.go](file:///d:/project/go/RediGo/internal/persistence/version_set.go)
- **LSM 过期键删除改为有界队列**：懒加载读到过期键后不再无界起 goroutine，而是入队给单 worker 处理，避免 goroutine/IO 风暴；队列长度与丢弃计数可观测。
  - 参考：[database.go](file:///d:/project/go/RediGo/internal/database/database.go)
- **冷启动策略配置生效**：冷启动策略从启动时加载的配置注入到 Database，不再在库内读取 `config.DefaultConfig()`；`load_all/lazy_load/no_load` 的启动行为一致。
  - 参考：[db_manager.go](file:///d:/project/go/RediGo/internal/database/db_manager.go)、[database.go](file:///d:/project/go/RediGo/internal/database/database.go)
- **过期键懒删除与 usedMemory 修正**：读路径命中过期键会立刻从内存删除并扣减 `usedMemory`；若启用 LSM，会同步入队删除以避免复活与内存漂移。
  - 参考：[database.go](file:///d:/project/go/RediGo/internal/database/database.go)
- **Stdout 污染治理**：移除 LSM/Database 内部 `fmt.Printf/Println` 输出，统一走 logger（可控等级/可关闭），避免污染 stdout。
  - 参考：[lsm_engine.go](file:///d:/project/go/RediGo/internal/persistence/lsm_engine.go)、[database.go](file:///d:/project/go/RediGo/internal/database/database.go)
- **TableCache 并发安全关闭**：SSTableReader 增加引用计数，淘汰/关闭缓存时不会影响正在进行的读取与迭代，避免并发 Close 导致读失败或崩溃。
  - 参考：[table_cache.go](file:///d:/project/go/RediGo/internal/persistence/table_cache.go)、[sstable_reader.go](file:///d:/project/go/RediGo/internal/persistence/sstable_reader.go)
- **WAL 背压与可运维性**：WAL 入队支持超时（队列满返回错误而非无限阻塞），并暴露队列长度/入队耗时/超时计数等统计用于运维观测。
  - 参考：[options.go](file:///d:/project/go/RediGo/internal/persistence/options.go)、[wal.go](file:///d:/project/go/RediGo/internal/persistence/wal.go)、[lsm_engine.go](file:///d:/project/go/RediGo/internal/persistence/lsm_engine.go)
- **vLog GC 的 CAS 语义**：rewrite 回调携带旧 VP，写入前锁内比对，避免并发覆盖/复活。
  - 参考：[gc.go](file:///d:/project/go/RediGo/internal/persistence/vlog/gc.go)、[lsm\_engine.go](file:///d:/project/go/RediGo/internal/persistence/lsm_engine.go)
- **offload 远端对象回收**：ObjectStore 支持 Delete，Reset 时按 VersionSet 清理远端 SSTable 与校验文件（offload 默认关闭，作为可选能力）。
  - 参考：[object\_store.go](file:///d:/project/go/RediGo/internal/persistence/object_store.go)、[sstable\_offloader.go](file:///d:/project/go/RediGo/internal/persistence/sstable_offloader.go)
- **写入成功语义（强/弱一致可选）**：新增配置 `persistence_write_mode`（strong/weak）与 `persistence_durability`（wal/wal_fsync/lsm），默认 strong；strong 模式下持久化写失败会返回 `-ERR`，weak 模式下吞错但通过 INFO/统计可观测。
  - 参考：[config.yaml](file:///d:/project/go/RediGo/config.yaml)、[database.go](file:///d:/project/go/RediGo/internal/database/database.go)、[string_bytes.go](file:///d:/project/go/RediGo/internal/database/string_bytes.go)

## 待修复系统级问题（按优先级）

### P0（高危：可能导致数据丢失/不一致/DoS）

### P1（中高危：长稳运行会退化/不可运维/高概率踩坑）

### P2（低到中危：可观测/体验/维护成本问题）

## 建议的修复顺序（一次“抓大头”）
暂无
