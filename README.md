# RediGo - 高性能 Redis 兼容服务器


## 📖 项目简介

RediGo 是一个使用 Go 语言实现的高性能、Redis 协议兼容的键值存储服务器。支持内存模式和 LSM Tree 持久化模式，提供灵活的数据持久化选项。

### ✨ 核心特性

- ✅ **Redis 协议兼容** - 支持 33+ 个常用 Redis 命令
- ✅ **高性能网络层** - 支持标准库 net 和 **gnet (Reactor 模式)** 双引擎切换
- ✅ **双模式运行** - 内存模式 / LSM Tree 持久化模式
- ✅ **极致并发** - **分段锁 (Sharded Locks)** 设计，显著减少锁竞争
- ✅ **低分配协议链路** - RESP 解析 `args` 走 `[][]byte`，控制拷贝时机
- ✅ **热点快路径** - GET/SET/INCR 绕过通用分发与通用编码，降低延迟与分配
- ✅ **高性能存储** - LevelDB/RocksDB 风格的 LSM Tree 引擎
- ✅ **并发安全** - 完整的读写锁机制
- ✅ **数据过期** - 支持 TTL/PTTL 精确过期控制
- ✅ **多数据库** - 16 个独立数据库（db\_0 \~ db\_15）
- ✅ **批量操作** - MSET/MGET 原子批量操作
- ✅ **原子增减** - INCR/DECR 原子计数器

***

## 🚀 快速开始

### 安装依赖

```bash
go mod download
```

### 编译项目

```bash
make build
# 或者
go build -o bin/redigo-server cmd/server/main.go
```

### 运行与控制（推荐）

推荐优先使用 `redigo` 脚本进行启动/停止/查看状态/查看日志。

**Linux/macOS**

```bash
chmod +x ./redigo
./redigo start
./redigo status
./redigo logs --follow --tail 200
./redigo client 127.0.0.1 16379
./redigo stop
```

**Windows (PowerShell)**

```powershell
.\redigo start
.\redigo status
.\redigo logs --follow --tail 200
.\redigo client 127.0.0.1 16379
.\redigo stop
```

### 手动启动（可选）

```bash
./bin/redigo-server
```

### 使用客户端连接

```bash
redis-cli -h 127.0.0.1 -p 16379
```

### 测试连接

```bash
PING
# Output: PONG

SET mykey "Hello RediGo"
GET mykey
# Output: "Hello RediGo"
```

***

## 📦 项目结构

```
RediGo/
├── README.md                 # 主说明文档（本文件）
├── Makefile                  # 构建脚本
├── go.mod                    # Go 模块定义
│
├── cmd/                      # 命令行入口
│   ├── server/              # 服务器入口
│   └── client/              # 客户端入口
│   └── bench/               # 基准工具入口
│
├── config/                   # 配置管理
│   └── config.go            # 配置定义和加载
│
├── internal/                 # 内部核心包
│   ├── command/             # Redis 命令实现
│   │   └── basic.go         # 基础命令实现
│   │   └── registry.go      # 命令注册表
│   │
│   ├── database/            # 数据库核心
│   │   └── database.go      # 数据库实现
│   │   └── db_manager.go    # 数据库管理器
│   │
│   ├── datastruct/          # 数据结构
│   │   └── data.go          # DataValue 定义
│   │
│   ├── persistence/         # LSM Tree 持久化引擎
│   │   ├── README.md        # 持久化模块详细文档
│   │   ├── lsm_engine.go    # LSM 引擎主逻辑
│   │   ├── memtable.go      # MemTable (跳表)
│   │   ├── sstable.go       # SSTable 读写
│   │   ├── bloom_filter.go  # Bloom Filter
│   │   ├── block_cache.go   # Block Cache (LRU)
│   │   ├── wal.go           # Write-Ahead Logging
│   │   ├── compaction.go    # Compaction 合并
│   │   └── ...              # 其他组件
│   │
│   ├── protocol/            # Redis 协议解析
│   │   └── parser.go        # RESP 协议解析器
│   │
│   └── server/              # 服务器实现
│       ├── server_std.go    # 标准库 net 服务器
│       └── server_gnet.go   # gnet 服务器
│
├── pkg/                      # 公共工具包
│   ├── logger/              # 日志包
│
│
├── scripts/                  # 辅助脚本
│   ├── build.ps1            # Windows 构建脚本
│   ├── clean.ps1            # Windows 清理脚本
│   ├── test.ps1             # Windows 测试脚本
│   └── clean.sh             # Linux/macOS 清理脚本
│
├── redigo                   # Linux/macOS 命令行入口
├── redigo.ps1               # Windows 命令行入口
├── redigo.cmd               # Windows 包装器（转发到 redigo.ps1）
│
├── bin/                      # 编译输出（gitignore）
│   └── redigo-server
│
├── data/                     # 数据目录（gitignore）
│   └── db_*/                # 各数据库的 LSM 文件
│
└── logs/                     # 日志目录（gitignore）
    ├── redigo.pid            # 后台服务 PID（脚本生成）
    ├── redigo.log            # 服务端日志（默认）
    ├── server.out.log        # stdout（脚本重定向）
    └── server.err.log        # stderr（脚本重定向）
```

***

## 🔧 配置说明

服务端默认从项目根目录的 `config.yaml` 加载配置；也可以在启动时通过 `-config` 指定路径。

```bash
go run ./cmd/server -config ./config.yaml
```

如配置文件不存在或解析失败，会回退到内置默认值；环境变量 `REDIGO_*` 会覆盖配置文件中的同名字段。

***

## 📋 支持的 Redis 命令

### 连接测试

- `PING [message]` - 测试服务器连接

### 字符串操作

- `SET key value` - 设置键值
- `GET key` - 获取键值
- `DEL key [key ...]` - 删除键
- `EXISTS key` - 检查键是否存在
- `EXPIRE key seconds` - 设置过期时间
- `TTL key` - 查看剩余时间（秒）
- `PTTL key` - 查看剩余时间（毫秒）
- `INCR key` - 原子递增 1
- `DECR key` - 原子递减 1
- `MSET key value [key value ...]` - 批量设置
- `MGET key [key ...]` - 批量获取
- `RENAME old_key new_key` - 重命名键
- `RENAMENX old_key new_key` - 条件重命名
- `KEYS pattern` - 查询键列表
- `DBSIZE` - 数据库大小
- `FLUSHDB` - 清空数据库

### 列表操作

- `LPUSH key value [value ...]` - 左侧压入
- `RPUSH key value [value ...]` - 右侧压入
- `LPOP key` - 左侧弹出
- `RPOP key` - 右侧弹出
- `LLEN key` - 列表长度
- `LRANGE key start stop` - 范围查询

### 哈希操作

- `HSET key field value` - 设置字段
- `HGET key field` - 获取字段
- `HMSET key field value [field value ...]` - 批量设置字段
- `HMGET key field [field ...]` - 批量获取字段
- `HDEL key field [field ...]` - 删除字段
- `HLEN key` - 字段数量
- `HEXISTS key field` - 检查字段
- `HKEYS key` - 获取所有字段名
- `HVALS key` - 获取所有字段值
- `HGETALL key` - 获取所有字段和值
- `HINCRBY key field increment` - 字段原子递增
- `HINCRBYFLOAT key field increment` - 字段浮点递增

### 数据库管理

- `SELECT index` - 切换数据库
- `FLUSHDB` - 清空当前库
- `DBSIZE` - 查询大小

**命令完成率**: \~85% （核心命令全覆盖）

***

## 🏗️ 架构设计

### 整体架构

[🖼️ 在线查看详细架构图 / View Architecture Diagram Online](https://TZJ-BYTE.github.io/RediGo/docs/architecture.html)

### LSM Tree 架构

详见：[`internal/persistence/README.md`](internal/persistence/README.md)

### 智能冷热分层（现状与演进）

- **文件热度（已实现）**：以 SSTable 文件为粒度统计读热度，在 Compaction 选择输入时优先合并冷文件、在容量压力可控时延迟合并热文件，降低热数据被频繁重写带来的写放大与抖动。
- **Block 热度 + SLRU/Pinning（已实现）**：Block Cache 采用 SLRU（probation/protected）并对高命中 block 做 pinning 倾向；缓存为引擎级共享，减少跨表碎片与重复缓存。
- **L0 细粒度策略（已实现）**：L0 compaction 输入不再全量选择，改为基于 key-range 重叠的最小集合并迭代扩展，减少不必要的“全量搬运”与抖动。
- **Key 热度 Top-K（已实现基础统计）**：对读命中的 key 做 Top-K（heavy hitters）统计，当前用于可观测性与后续策略扩展。
- **调度与背压（已实现基础版）**：后台任务采用预算调度并联动写入压力与缓存命中率；写入繁忙/缓存命中偏低时会延后 compaction/offload，L0 风险升高时优先 compaction，避免 CPU/IO 突刺影响前台延迟。

***

## 📊 性能指标

### 写入性能（LSM Mode）

| 指标     | 目标值          | 实测值          |
| ------ | ------------ | ------------ |
| 吞吐量    | > 200K ops/s | \~150K ops/s |
| WAL 延迟 | < 1ms        | < 0.5ms      |
| 压缩率    | > 50%        | \~60%        |

### 读取性能

| 场景              | 目标延迟    | 实测延迟    |
| --------------- | ------- | ------- |
| 缓存命中            | < 0.5ms | < 0.3ms |
| 缓存未命中           | < 10ms  | < 5ms   |
| Bloom Filter 过滤 | O(1)    | O(1)    |

### 内存效率

- MemTable 大小：4MB（可配置）
- Block Cache：100MB（可配置，默认启用；引擎级共享 SLRU）
- Bloom Filter：10 bits/key（可配置）

***

## ☁️ 存算分离 (MinIO/S3)

RediGo 支持将冷数据自动卸载到 MinIO 或兼容 S3 的对象存储，实现“容量按需扩展、计算与存储解耦”。

### 意义

- **容量解耦**：SSTable 可以落在对象存储，避免本地磁盘成为容量瓶颈；扩容从“加盘”变为“扩 bucket”。
- **成本优化**：热数据留在本地 SSD，冷数据放对象存储；在大数据量场景下，单位成本通常更低。
- **弹性与运维**：计算节点可水平扩展/缩容；数据不必跟着计算节点一起迁移，适配云原生/短生命周期实例。
- **容灾与持久性**：对象存储天然提供较高的持久性与跨区域能力（取决于你的对象存储配置）。

### 当前实现范围

- **文件级别卸载**：以 SSTable 文件为单位上传/下载（不是 Block 级别按需拉取）。
- **回源读取**：本地缺失 SSTable 时，会自动从对象存储下载回本地再打开继续读。
- **触发时机**：flush/compaction 产出的 SSTable 会按策略尝试卸载。

### 权衡

- **延迟**：命中本地 SSD 依然快；但回源下载会显著增加尾延迟，适合“冷数据低频访问”的场景。
- **一致性与可观测性**：对象存储读写失败、网络抖动需要额外监控与重试策略（基础版已支持失败返回）。
- **带宽/费用**：跨网/跨地域访问会引入带宽占用与可能的出网费用。

### 1. 启动 MinIO

使用 Docker 快速启动 MinIO：

```bash
docker run -p 9000:9000 -p 9001:9001 \
  -e "MINIO_ROOT_USER=<access_key>" \
  -e "MINIO_ROOT_PASSWORD=<secret_key>" \
  minio/minio server /data --console-address ":9001"
```

### 2. 配置 RediGo

存算分离默认关闭（`OffloadEnabled=false`）。需要时可在 `config/config.go` 或通过环境变量（`REDIGO_OFFLOAD_*`）显式启用卸载：

```go
// config.go (DefaultConfig)
OffloadEnabled:   true,
OffloadBackend:   "minio",
OffloadEndpoint:  "127.0.0.1:9000",
OffloadAccessKey: "<access_key>",
OffloadSecretKey: "<secret_key>",
OffloadBucket:    "redigo-data",
OffloadRegion:    "us-east-1",
OffloadBasePrefix: "redigo/",
```

等价的环境变量写法：

```bash
export REDIGO_OFFLOAD_ENABLED=true
export REDIGO_OFFLOAD_BACKEND=minio
export REDIGO_OFFLOAD_ENDPOINT=127.0.0.1:9000
export REDIGO_OFFLOAD_ACCESS_KEY=<access_key>
export REDIGO_OFFLOAD_SECRET_KEY=<secret_key>
export REDIGO_OFFLOAD_BUCKET=redigo-data
export REDIGO_OFFLOAD_REGION=us-east-1
export REDIGO_OFFLOAD_BASE_PREFIX=redigo/
```

### 3. 验证

启动 RediGo 后，SSTable 文件会被上传到 MinIO 的 Bucket 中；当本地 SSTable 文件缺失时，读取会自动从对象存储回源下载后继续读。

也可以直接跑测试验证：

```bash
go test ./internal/persistence -run TestOffloading_FSBackend_ReadBack -v
go test ./internal/persistence -run TestOffloading_MinIOBackend_ReadBack -v
```

***

## 🧪 测试

### 运行单元测试

```bash
go test -count=1 ./...
```

### 运行竞态检测（推荐）

```bash
go test -race -count=1 ./...
```

在 Windows 上 `-race` 需要启用 CGO 并确保 `gcc` 可用（例如 MSYS2 UCRT64 的 `C:\msys64\ucrt64\bin` 在 PATH 中）。

### 运行特定包测试

```bash
go test ./internal/persistence -v
go test ./internal/database -v
go test ./internal/command -v
```

### 性能基准测试

```bash
go test ./internal/persistence -bench=. -benchmem
```

***

## 🛠️ 开发指南

### 添加新的 Redis 命令

1. 在 [`internal/command/basic.go`](internal/command/basic.go) 中实现命令：

```go
type MyCommand struct{}

func (c *MyCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
    // 实现逻辑
    return protocol.MakeSimpleString("OK")
}
```

1. 在 [`internal/command/registry.go`](internal/command/registry.go) 中注册：

```go
DefaultRegistry.Register("MYCMD", &MyCommand{})
```

### 修改配置

编辑 [`config/config.go`](config/config.go) 添加新的配置项。

### 调试技巧

```bash
# 查看详细日志
./bin/redigo-server --log-level debug

# 查看内存使用
ps aux | grep redigo-server

# 监控连接数
netstat -an | grep 16379
```

***

## 📚 学习资源

### 核心文档

- **主文档**: [`README.md`](README.md)（本文件）
- **持久化模块**: [`internal/persistence/README.md`](internal/persistence/README.md) - 包含 LSM Tree 的详细设计、配置、故障排查和最佳实践。

### 外部参考

- [Redis Protocol Specification](https://redis.io/topics/protocol)
- [LevelDB Paper](https://leveldb.appspot.com/)
- [The Log-Structured Merge-Tree](https://www.cs.umb.edu/~poneil/lsmtree.pdf)

***

## 🤝 贡献指南

### 提交代码

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

### 代码规范

- 遵循 Go 语言规范
- 添加必要的注释
- 编写单元测试
- 保持代码整洁

***

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

***

## 🎯 路线图

### v1.0 (已完成)

- ✅ 基础 Redis 命令支持
- ✅ LSM Tree 持久化引擎
- ✅ gnet 高性能网络层
- ✅ 分段锁并发优化
- ✅ 多数据库支持
- ✅ 过期键管理

### v1.1 (进行中)

- ✅ 智能冷热分层 (Hot/Cold Tiering)
- ✅ Key-Value 分离 (WiscKey / vLog)
- ✅ 存算分离 (S3/MinIO Offloading)

### v2.0 (未来)

- [ ] Serverless 架构支持
- [ ] 更智能冷热分层：Key 热度统计与策略
- [ ] 更智能冷热分层：Block 热度与 Cache Pinning/分区缓存
- [ ] 更智能冷热分层：L0 细粒度 Compaction（按重叠范围切分）
- [ ] 更智能冷热分层：后台调度与背压（Compaction/Offload 联动）
- [ ] 分布式事务
- [ ] 监控 Dashboard

***

## 👥 作者

TZJ-BYTE

***

## 📞 联系方式

- **项目地址**: <https://github.com/TZJ-BYTE/RediGo>
- **问题反馈**: <https://github.com/TZJ-BYTE/RediGo/issues>

***

**RediGo** - 让 Redis 协议实现更简单！ 🚀

*最后更新时间：2026-03-14*
