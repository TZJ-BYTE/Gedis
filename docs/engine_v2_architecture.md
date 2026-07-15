# RediGo Engine v2 架构说明

## 目标

Engine v2 不再沿用旧引擎的 `WAL + LSM + vLog + offload` 组合，而是先收敛成一个更贴近实际负载的内核：

- 主要面向 Redis 风格的小 KV 点查场景
- 优先保证实现简单、恢复清晰、后续易扩展
- 先做独立嵌入式库，再考虑接回 RediGo

当前路线：

- **主存储结构**：`segment log + 内存索引 + checkpoint`
- **恢复方式**：加载 checkpoint，再回放增量 segment
- **后台治理**：后续再补 cleaner / compaction，而不是一开始就上 LSM

## 当前目录

新引擎代码位于：

- `engine_v2/`

核心源码：

- `src/engine.rs`：引擎入口与对外 API
- `src/options.rs`：配置项
- `src/record.rs`：记录编码格式
- `src/segment.rs`：segment 扫描、读取与恢复
- `src/checkpoint.rs`：checkpoint 读写
- `src/iterator.rs`：快照式迭代器
- `src/error.rs`：错误定义

## 当前能力

第一版已经实现：

- `open`
- `put`
- `get`
- `delete`
- `write_batch`
- `compact_once`
- `compact_until_stable`
- `snapshot`
- `contains_key`
- `iter`
- `sync`
- `checkpoint`
- `stats`
- `close`

同时具备这些基础能力：

- append-only segment 写入
- 内存索引维护最新值位置
- segment 达到阈值后自动轮转
- 启动时扫描 segment 重建索引
- checkpoint 加速恢复
- 遇到尾部半条记录时自动截断恢复

## 文件布局

当前磁盘布局如下：

```text
data_dir/
  segments/
    0000000001.seg
    0000000002.seg
  meta/
    checkpoint.bin
```

说明：

- `segments/` 存放 append-only 数据段
- `checkpoint.bin` 存放内存索引快照和恢复起点

## 写入路径

`put/delete` 的执行流程：

1. 将记录编码为二进制 record
2. 追加写入当前 active segment
3. 按配置执行 `flush` 或 `sync_data`
4. 更新内存索引
5. 达到阈值后触发 checkpoint
6. active segment 超过大小阈值后轮转

这里没有单独的 WAL，因为：

- segment 本身就是主追加日志
- 不再重复写一份“日志再写一份表”
- 更贴近当前项目的小 KV 和点查负载

## 读路径

`get` 的执行流程：

1. 先查内存索引
2. 拿到 `segment_id + value_offset + value_len`
3. 直接从对应 segment 读取 value

当前读路径是最小实现，后续可以继续补：

- block cache
- mmap 读取
- prefix iterator / range scan 优化

## Reader Cache

当前已经实现第一版 segment reader cache：

- 按 `segment_id` 缓存已打开的只读文件句柄
- `get` 时不会每次重新 `open` 文件
- 读取时通过 `try_clone()` 获取独立句柄，避免共享 seek 位置互相干扰
- 旧 segment 被 cleaner 回收时，会同步从 cache 中驱逐

这一版还没有做容量限制和 LRU，策略非常简单：

- 先优先解决热读反复打开文件的问题
- 等读路径更稳定之后，再决定是否需要做上限控制或分层缓存

## Cleaner / CompactOnce

当前已经实现第一版 cleaner，入口为：

- `compact_once`

行为如下：

1. 只处理当前 active segment 之前的旧 segment
2. 扫描旧 segment 中的记录
3. 如果某条 Put 记录仍然是当前索引指向的最新版本，就把它重写到新的 active segment
4. 完成 checkpoint 后删除已经回收的旧 segment

当前这版 cleaner 的特点是：

- **同步执行**：由调用方主动触发
- **优先正确性**：先保证 live entry 不丢、旧段可回收
- **支持延迟回收**：如果旧 segment 仍被 snapshot 引用，就先挂起删除，等 snapshot 释放后再回收
- **还没做调度**：没有后台线程、没有回收预算、没有分级 compaction

这正是当前阶段想要的取舍：先把 segment log 的闭环做出来，再考虑复杂策略。

另外当前已经提供：

- `compact_until_stable(max_rounds)`

它会在限定轮数内重复执行 cleaner，直到 segment 数量收敛或本轮没有更多 live entry 需要重写。这个接口更适合作为当前阶段的维护入口。

## Snapshot / 只读视图

当前已经实现第一版 snapshot：

- snapshot 创建时会固定当前索引视图
- snapshot 内部会 pin 住自己引用到的 segment
- cleaner 遇到仍被 snapshot 使用的旧 segment 时，不会立刻删除
- snapshot 释放后，挂起的旧 segment 会自动尝试回收

这版 snapshot 的目标不是多版本事务，而是先保证：

- cleaner 不会误删正在被只读视图使用的数据
- 读路径和空间回收之间有清晰边界

## Stats

当前引擎已提供基础统计接口，主要包括：

- key 数量
- active segment id
- segment 总数
- reader cache 中文件数
- 被 snapshot pin 住的 segment 数
- 挂起等待回收的 segment 数

## Record 格式

当前 record 采用定长头 + 变长 payload：

```text
magic(4) + version(1) + kind(1) + reserved(2)
+ key_len(4) + value_len(4) + checksum(4)
+ key bytes + value bytes
```

其中：

- `kind = 1` 表示 Put
- `kind = 2` 表示 Delete
- `checksum` 用于检测尾部损坏或不完整写入

## Checkpoint 设计

checkpoint 保存三类信息：

- 当前 active segment id
- 写入到哪个 offset 为止
- 当前 key 到 value location 的索引映射

恢复时流程是：

1. 先尝试加载 checkpoint
2. 从 checkpoint 记录的位置继续扫描后续 segment
3. 如果 active segment 尾部有半条记录，则截断到最后一个有效 offset

## 现在故意没做的内容

这几项是刻意暂缓，而不是忘了做：

- 后台 cleaner 调度
- 多版本事务级 snapshot / MVCC
- batch 原子提交语义
- TTL 与过期键
- 压缩
- bloom filter
- block 化读取
- 跨语言 FFI

原因很直接：

- 第一阶段先把“可写、可读、可恢复”做扎实
- 避免像旧引擎一样过早堆太多能力，导致复杂度先失控

## 下一步计划

下一阶段我建议按这个顺序推进：

### 1. 后台 Cleaner 调度

增加旧 segment 清理器：

- 识别无效旧记录
- 重写 live entries
- 回收陈旧 segment
- 增加 budget、节流和触发条件

### 2. Block Cache / 页级缓存

在 reader cache 之上继续做更细粒度的数据缓存。

### 3. Snapshot / Iterator 完善

继续补强 snapshot 与迭代器的只读一致性边界。

### 4. FFI 边界

如果后续确定要给 Go 或其他语言复用，再设计稳定的 C ABI。

## 当前状态结论

Engine v2 已经不是空壳，而是一个能跑通基本闭环的原型内核。

它现在适合继续沿着这条路线迭代，不建议再回头扩展旧 LSM 引擎。后续工作应当集中在：

- 写入批处理
- cleaner
- 缓存
- 更稳定的快照与迭代器
- 再之后才考虑接回 RediGo
