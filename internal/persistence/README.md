# LSM Tree 持久化存储引擎

> LevelDB/RocksDB 风格的高性能存储引擎，为 RediGo 提供持久化能力

***

## 📖 什么是 LSM Tree？

**LSM Tree (Log-Structured Merge Tree)** 是一种专为**高写入吞吐量**设计的存储结构。它的核心思想是将磁盘的**随机写**转换为**顺序写**。

你可以把它想象成一个繁忙的图书馆管理系统：

1. **MemTable (办公桌)**: 新来的书（数据）先随手放在办公桌上。因为就在手边，处理最快。
2. **Immutable MemTable (手推车)**: 当桌子堆满了，把这堆书打包放到手推车上。这时候这堆书只能读，不能改，准备推去仓库。
3. **SSTable (书架)**: 仓库里的书架。书被整齐地排序、归档，查找起来很方便。
4. **WAL (日记本)**: 为了防止断电，每次收书前先在日记本上记一笔。

```mermaid
graph TD
    %% Style Definitions
    classDef memory fill:#e3f2fd,stroke:#1565c0,stroke-width:2px,color:#333;
    classDef disk fill:#fff3e0,stroke:#ef6c00,stroke-width:2px,color:#333;
    classDef component fill:#ffffff,stroke:#333,stroke-width:1px,color:#333;

    subgraph Memory [In-Memory / 内存区]
        direction TB
        Mem[MemTable<br/>Active Write]:::component
        Imm[Immutable MemTable<br/>Read Only]:::component
    end

    subgraph Disk [Disk Storage / 磁盘存储区]
        direction TB
        L0[Level 0<br/>4MB<br/>Keys may overlap]:::component
        L1[Level 1<br/>10MB<br/>Sorted, No overlap]:::component
        L2[Level 2<br/>100MB]:::component
        L3[Level 3<br/>1GB]:::component
        Ln[... Level 4-5 ...]:::component
        L6[Level 6<br/>10TB]:::component
    end

    Mem -->|Full| Imm
    Imm -->|Flush| L0
    L0 -->|Compaction| L1
    L1 -->|Compaction| L2
    L2 -->|Compaction| L3
    L3 -->|Compaction| Ln
    Ln -->|Compaction| L6

    style Memory fill:#e3f2fd,stroke:#1565c0,stroke-width:2px,color:#333
    style Disk fill:#fff3e0,stroke:#ef6c00,stroke-width:2px,color:#333
```

***

## 🏗️ 核心架构

### 组件交互图

```mermaid
classDiagram
    class LSMEngine {
        +Put(key, value)
        +Get(key)
        +Delete(key)
    }
    class MemTable {
        +Insert() [插入数据]
        +Get() [查找数据]
        <<SkipList / 跳表>>
    }
    class ImmutableMemTable {
        +Get() [查找数据]
        +Flush() [刷写磁盘]
        <<ReadOnly / 只读>>
    }
    class WAL {
        +Append() [追加日志]
        +Recover() [崩溃恢复]
    }
    class SSTable {
        +Search() [二分查找]
        <<Disk File / 磁盘文件>>
    }
    class Compactor {
        +Merge() [合并文件]
        <<Background / 后台线程>>
    }

    LSMEngine --> WAL : 1. 记录日志 (防丢失)
    LSMEngine --> MemTable : 2. 写入内存 (高性能)
    LSMEngine --> ImmutableMemTable : 3. 转为只读 (准备刷盘)
    ImmutableMemTable --> SSTable : 4. 持久化文件 (落盘)
    Compactor ..> SSTable : 5. 多层合并 (空间回收)

    style LSMEngine fill:#1976d2,stroke:#333,stroke-width:2px,color:#fff
    style MemTable fill:#1976d2,stroke:#333,stroke-width:2px,color:#fff
    style ImmutableMemTable fill:#1976d2,stroke:#333,stroke-width:2px,color:#fff
    style WAL fill:#e64a19,stroke:#333,stroke-width:2px,color:#fff
    style SSTable fill:#455a64,stroke:#333,stroke-width:2px,color:#fff
    style Compactor fill:#388e3c,stroke:#333,stroke-width:2px,color:#fff
```

**组件功能详解：**

| 组件            | 中文名    | 作用                                                    | 对应代码                    |
| :------------ | :----- | :---------------------------------------------------- | :---------------------- |
| **LSMEngine** | 引擎核心   | 总指挥，协调读写请求，管理所有组件的生命周期。                               | `lsm_engine.go`         |
| **MemTable**  | 内存表    | **活跃的**内存数据结构（跳表），支持读写。所有新数据首先写入这里。                   | `memtable.go`           |
| **Immutable** | 不可变表   | **只读的**内存表。当 MemTable 满时，会转换为此状态，等待后台线程刷入磁盘。          | `immutable_memtable.go` |
| **WAL**       | 预写日志   | **Write-Ahead Log**。在写内存前先追加写磁盘日志，确保即使断电，数据也能恢复。      | `wal.go`                |
| **SSTable**   | 排序字符串表 | **Sorted String Table**。磁盘上的不可变文件，内部 Key 有序排列，支持二分查找。 | `sstable.go`            |
| **Compactor** | 压缩器    | 后台清理工。负责将多个小 SSTable 合并成大文件，清理无效数据，优化读取速度。            | `compaction.go`         |

***

## 🔄 数据流转详解

### 1. 写入流程 (Write Path)

LSM Tree 的写入设计是为了极致的性能，整个过程几乎不涉及随机磁盘 I/O，只有顺序追加写。

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant Engine as LSM引擎
    participant WAL as WAL日志
    participant MemTable as 内存表(MemTable)
    participant Disk as 磁盘存储

    Client->>Engine: 发起写请求 Put("key", "val")
    
    rect rgb(69, 90, 100)
    Note right of Engine: 第一步：持久化保障
    Engine->>WAL: Append Log (顺序追加日志)
    WAL-->>Engine: 写入成功
    end

    rect rgb(25, 118, 210)
    Note right of Engine: 第二步：内存操作
    Engine->>MemTable: Insert (插入跳表 O(log N))
    MemTable-->>Engine: 插入成功
    end

    Engine-->>Client: 返回 OK

    rect rgb(56, 142, 60)
    Note over Engine,Disk: 异步后台流程 (不阻塞客户端)
    Engine->>Engine: 检查 MemTable 大小
    opt MemTable 已满 (> 4MB)
        Engine->>Disk: 将 MemTable 冻结并刷写为 SSTable 文件 (Level 0)
    end
    end
```

**步骤解析：**

1. **记录日志 (WAL)**：
   - 为了防止服务器突然断电导致内存数据丢失，系统首先会将操作记录追加到 `WAL` 文件末尾。
   - **为什么快？** 因为这是顺序写（Sequential Write），比随机写磁盘快几个数量级。
2. **写入内存 (MemTable)**：
   - 日志记录安全后，数据被插入到内存中的 `MemTable`。
   - `MemTable` 使用 **跳表 (SkipList)** 数据结构，插入和查找的时间复杂度都是 O(log N)，非常高效。
3. **异步刷盘 (Flush)**：
   - 当 `MemTable` 达到阈值（默认 4MB），它会瞬间变成 `Immutable MemTable`（只读），并创建一个新的 `MemTable` 接收新写入。
   - 后台线程会将 `Immutable MemTable` 的数据排序后写成一个 `SSTable` 文件存入磁盘。这个过程是异步的，**不会阻塞**客户端的写入请求。

***

### 2. 读取流程 (Read Path)

读取时遵循 **"由新到旧"** 的查找顺序，因为越新的数据越可能在内存中。

```mermaid
graph TD
    Start(开始查找 Key) --> CheckMem{1. 查内存表<br/>MemTable}
    CheckMem -- 命中 --> Return[返回 Value]
    CheckMem -- 未命中 --> CheckImm{2. 查不可变表<br/>Immutable}
    
    CheckImm -- 命中 --> Return
    CheckImm -- 未命中 --> CheckL0{3. 查 Level 0 文件<br/>最新磁盘文件}
    
    CheckL0 -- 可能存在 --> ReadL0[读取 SSTable 文件]
    ReadL0 -- 命中 --> Return
    ReadL0 -- 未命中 --> CheckL1{4. 查 Level 1..6 文件<br/>老旧数据}
    CheckL0 -- 绝对不存在 --> CheckL1
    
    CheckL1 -- 命中 --> Return
    CheckL1 -- 未命中 --> NotFound[返回: 数据不存在]
    
    style Start fill:#e1f5fe,stroke:#01579b,stroke-width:2px
    style Return fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
    style NotFound fill:#ffebee,stroke:#c62828,stroke-width:2px
    style CheckMem fill:#fff9c4,stroke:#fbc02d,stroke-width:2px
    style CheckImm fill:#fff9c4,stroke:#fbc02d,stroke-width:2px
    style CheckL0 fill:#ffe0b2,stroke:#f57c00,stroke-width:2px
    style CheckL1 fill:#ffe0b2,stroke:#f57c00,stroke-width:2px
```

**步骤解析：**

1. **查热数据 (MemTable)**：
   - 首先检查内存中的 `MemTable`。如果刚写入不久，这里一定能找到，速度是纳秒级。
2. **查次热数据 (Immutable)**：
   - 如果 `MemTable` 没找到，检查正在准备刷盘的 `Immutable MemTable`。
3. **查磁盘数据 (SSTable)**：
   - 如果内存里都没有，就必须去磁盘找了。
   - **Level 0**: 最新的磁盘文件。因为是直接由内存 Dump 出来的，Level 0 的不同文件之间 Key 可能会重叠，所以可能需要查多个文件。
   - **Level 1 \~ 6**: 经过 Compaction 整理后的文件。每一层的文件之间 Key **不重叠**，可以通过二分查找快速定位。
4. **加速黑科技**：
   - **Bloom Filter (布隆过滤器)**：在读磁盘文件前，先问问 Bloom Filter。如果它说 "这个 Key 不在这个文件里"，那就绝对不在，**完全跳过**该文件的磁盘读取。这极大地减少了无效 I/O。
   - **Block Cache (块缓存)**：常用的数据块会被缓存在内存中 (LRU)，再次读取时无需访问磁盘。

***

## 🔧 关键机制

### 1. Compaction (合并压缩)

随着 Level 0 文件越来越多，读取会变慢。后台线程会定期执行 Compaction。

- **Level 0 -> Level 1**: 将 Level 0 的多个重叠文件，合并成 Level 1 的有序文件。
- **Level N -> Level N+1**: 当某一层过大时，选出一个文件，和下一层有重叠的文件合并。

```mermaid
graph TD
    %% Style Definitions
    classDef file fill:#fff3e0,stroke:#ef6c00,stroke-width:2px,color:#333;
    classDef process fill:#e1f5fe,stroke:#0277bd,stroke-width:2px,color:#333;
    classDef newfile fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px,color:#333;

    subgraph Level0 [Level 0: Key 范围可能重叠]
        F1[文件 A: 1..100]:::file
        F2[文件 B: 50..150]:::file
        F3[文件 C: 80..200]:::file
    end
    
    subgraph Level1 [Level 1: 有序，无重叠]
        L1F1[文件 X: 10..40]:::file
        L1F2[文件 Y: 200..300]:::file
    end
    
    Compactor((Compactor<br/>合并线程)):::process
    
    F1 --> Compactor
    F2 --> Compactor
    F3 --> Compactor
    L1F1 --> Compactor
    
    Compactor --> NewF1[新文件: 1..150]:::newfile
    Compactor --> NewF2[新文件: 151..300]:::newfile
    
    style Level0 fill:#fff3e0,stroke:#ef6c00,stroke-dasharray: 5 5,color:#333
    style Level1 fill:#e8f5e9,stroke:#2e7d32,stroke-dasharray: 5 5,color:#333
```

### 2. SSTable 文件格式

磁盘上的文件 (SSTable) 结构如下，包含索引和过滤器以加速查询：

```text
+-----------------+
|   Data Block 1  |  <- 存储实际 KV 数据 (按 Key 排序, Snappy 压缩)
+-----------------+
|   Data Block 2  |
+-----------------+
|       ...       |
+-----------------+
|   Filter Block  |  <- Bloom Filter 数据 (快速判断 Key 是否存在)
+-----------------+
|   Index Block   |  <- 索引 (每个 Data Block 的起始 Key 和偏移量)
+-----------------+
|      Footer     |  <- 指向 Index/Filter 的指针
+-----------------+
```

***

## 📦 文件结构

```
internal/persistence/
├── lsm_engine.go              # LSM 引擎主逻辑
├── memtable.go                # MemTable (跳表实现)
├── immutable_memtable.go      # Immutable MemTable
├── sstable.go                 # SSTable 基础定义
├── sstable_builder.go         # SSTable 构建器
├── sstable_reader.go          # SSTable 读取器
├── bloom_filter.go            # Bloom Filter
├── block_cache.go             # Block Cache (LRU)
├── table_cache.go             # Table Cache (文件描述符缓存)
├── wal.go                     # WAL 日志
├── compaction.go              # Compaction 逻辑
├── version_set.go             # Version Set 管理
├── options.go                 # 配置选项
├── iterator.go                # 迭代器接口
├── block.go                   # Block 管理
├── skiplist.go                # 跳表实现
├── utils.go                   # 工具函数
└── *_test.go                  # 单元测试文件
```

***

## 🚀 快速开始

### 1. 基本使用

```go
package main

import (
    "fmt"
    "github.com/TZJ-BYTE/RediGo/internal/persistence"
)

func main() {
    // 打开数据库
    options := persistence.DefaultOptions()
    engine, err := persistence.OpenLSMEnergy("/tmp/mydb", options)
    if err != nil {
        panic(err)
    }
    defer engine.Close()
    
    // 写入数据
    err = engine.Put([]byte("key1"), []byte("value1"))
    if err != nil {
        panic(err)
    }
    
    // 读取数据
    val, found := engine.Get([]byte("key1"))
    if found {
        fmt.Printf("Got: %s\n", string(val))
    }
    
    // 删除数据
    err = engine.Delete([]byte("key1"))
    if err != nil {
        panic(err)
    }
}
```

### 2. 编译代码

```bash
# 编译 persistence 包
cd /home/tzj/GoLang/RediGo
go build ./internal/persistence/...

# 运行所有测试
go test ./internal/persistence -v

# 运行基准测试
go test ./internal/persistence -bench=. -benchmem
```

### 3. 配置选项

```go
options := &persistence.Options{
    BlockSize:        4096,           // Block 大小
    MemTableSize:     4 << 20,        // MemTable 最大 4MB
    MaxOpenFiles:     500,            // 最大打开文件数
    BloomFPRate:      0.01,           // Bloom Filter 假阳性率
    CacheSize:        8 << 20,        // Cache 大小 8MB
    Compression:      SnappyCompression, // 启用 Snappy 压缩
}
```

***

## 🔍 故障排查

### 常见问题

#### 1. 重启后数据丢失

**症状**: 重启后无法读取之前的数据
**检查步骤**:

1. 检查数据目录 `ls -la data/db_0/sstable/`
2. 查看日志 `tail -f logs/redigo.log | grep -E "(LSM|SSTable|Recover)"`
   **解决方案**:

- 检查 SSTable 文件是否存在且非空
- 验证 MANIFEST 文件完整性

#### 2. 写入性能下降

**症状**: 写入速度明显变慢
**可能原因**:

- MemTable 频繁刷写
- Compaction 过于频繁
- 磁盘 I/O 瓶颈
  **优化方案**:
- 增大 `mem_table_size`
- 增大 `sstable_size`
- 调整 `level0_file_threshold`

#### 3. 内存占用过高

**症状**: 服务器内存持续增长
**检查步骤**:

- 查看 Block Cache 命中率
  **优化方案**:
- 限制 `block_cache_size`
- 使用懒加载策略 (`lazy_load`)

***

## 📊 监控指标

### 推荐监控项

1. **MemTable 大小**: `lsm_memtable_size_bytes`
2. **SSTable 数量**: `lsm_sstable_count{level}`
3. **Compaction 队列**: `lsm_compaction_queue_length`
4. **Block Cache 命中率**: `lsm_blockcache_hit_rate`
5. **WAL 文件大小**: `lsm_wal_size_bytes`
6. **写入放大系数**: `lsm_write_amplification`

### 日志关键字

```bash
# MemTable 刷写
grep "\[FLUSH\]" logs/redigo.log

# SSTable 创建
grep "\[SSTABLE\]" logs/redigo.log

# Compaction 活动
grep "\[COMPACTION\]" logs/redigo.log

# 恢复过程
grep -E "(Recover|LoadAllKeys)" logs/redigo.log
```

***

### 🚀 未来演进：Hybrid-Tiered Storage Engine

为了进一步提升性能和适应云原生环境，RediGo 计划在未来版本中引入以下创新架构：

#### 1. 智能冷热分层 (AI-driven Hot/Cold Tiering)

- **痛点**: 传统 Compaction 策略对数据热度无感知，导致热数据频繁下沉，影响读取性能。
- **创新**: 引入**访问频率感知的 Compaction**。
  - 在 Block Cache 中统计 Key 的访问热度。
  - **Pinning**: 极热的 SSTable 直接“钉”在 Block Cache 中，永不淘汰。
  - **Lazy Merge**: 暂缓热数据合并到更底层，减少读取时的层级穿透。

#### 2. Key-Value 分离 (WiscKey 风格)（已实现）

- **目标**: 大 Value (如 JSON, Blob) 降低写放大，减少 Compaction 搬运成本。
- **当前实现**: **Value Log (vLog)** 机制。
  - LSM Tree 只存储 `Key -> <FileID, Offset, Size>` 的元数据指针。
  - 大 Value 直接追加写入 append-only 的 `vLog` 文件。
  - 通过 `Options.ValueThreshold` 控制是否写入 vLog（默认 64 字节）。
  - 已包含 vLog GC（基于 key 校验与重写）。

#### 3. 存算分离架构 (Cloud Native Tiering)

- **痛点**: 本地磁盘容量有限，扩容复杂且昂贵。
- **创新**: **基于 S3/MinIO 的冷数据卸载**。
  - **Level 0-1 (热层)**: 驻留本地 NVMe SSD，保证极速读写。
  - **Level 2+ (冷层)**: 自动异步上传到对象存储 (S3)。
  - **按需加载**: 本地仅保留冷数据的元数据和 Bloom Filter，读取时按需从 S3 拉取 Block。
  - **收益**: 实现“无限容量”的 Redis，成本极低，完美适配 Serverless 架构。

***

## 📄 许可证

MIT License

***

## 👥 作者

TZJ-BYTE

***

*最后更新时间：2026-03-14*
