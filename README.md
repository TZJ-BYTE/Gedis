# RediGo - 高性能 Redis 兼容服务器

## 📖 项目简介

RediGo 是一个使用 Go 语言实现的高性能、Redis 协议兼容的键值存储服务器。当前项目主线已经转向**自主研发存储引擎**：上层服务仍由 Go 实现，底层持久化内核由 Rust 编写并通过桥接层接回 Go，而不是继续沿用旧版 LSM 路线或直接封装现成存储库。

### ✨ 核心特性

- ✅ **Redis 协议兼容** - 支持 33+ 个常用 Redis 命令
- ✅ **高性能网络层** - 支持标准库 net 和 **gnet (Reactor 模式)** 双引擎切换
- ✅ **双模式运行** - 内存模式 / 自研持久化引擎模式
- ✅ **极致并发** - **分段锁 (Sharded Locks)** 设计，显著减少锁竞争
- ✅ **低分配协议链路** - RESP 解析 `args` 走 `[][]byte`，控制拷贝时机
- ✅ **热点快路径** - GET/SET/INCR 绕过通用分发与通用编码，降低延迟与分配
- ✅ **自主研发引擎** - 面向 RediGo 负载重新设计的数据链路与恢复机制
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
│   ├── persistence/         # 旧版 LSM 持久化实现（已禁用，仅保留作迁移参考）
│   │   └── ...              # 迁移参考代码
│   │
│   ├── rustengine/          # Go <-> Rust 引擎桥接层
│   │   └── engine.go        # DLL 加载与调用封装
│   │
│   ├── ..\..\engine_v2\     # Rust 新存储引擎
│   │   ├── src/engine.rs    # 引擎主逻辑
│   │   ├── src/segment.rs   # segment log 读写
│   │   ├── src/snapshot.rs  # snapshot / 延迟回收
│   │   └── src/ffi.rs       # C ABI / DLL 导出
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
│   └── db_*/                # 各数据库的新引擎数据文件
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

### 当前持久化路线

当前默认持久化主线已经切换为 `engine_v2`，而且这条路线的目标不是“换一个现成库”，而是**自主研发 RediGo 自己的存储引擎**。

这条路线的边界很明确：

- 允许借鉴成熟思想，但**不直接依赖现成存储引擎库**
- 不再把旧版 `internal/persistence` LSM 实现作为未来主线
- 不机械复刻 `LSM-Tree / B+Tree` 的标准教材结构
- 以 RediGo 的真实负载、键模式、恢复要求和演进目标为第一约束

当前实现状态：

- 核心实现使用 Rust 编写
- Go 侧通过 `internal/rustengine` 在运行时加载 Rust DLL
- 数据库层已经接入 `put/get/delete/load_all/reset/close/stats`
- 旧 `internal/persistence` LSM 实现已默认禁用，仅保留作迁移参考

当前引擎已经具备：

- 崩溃恢复
- 批量写入
- snapshot
- reader cache
- 单轮与多轮压实
- 冷启动 `load_all / lazy_load`

### 自研引擎设计原则

RediGo 的新引擎路线不是“找一个现成方案照抄”，而是按下面的原则自己定义链路：

- **从业务场景出发**：先看 Redis-like 负载、键前缀模式、TTL、冷热数据分布，再定内核结构
- **先做最小但完整的闭环**：写入、读取、恢复、回收都必须先成立，再谈复杂优化
- **持久化链路自己掌控**：数据格式、恢复语义、checkpoint、cleaner 都由项目自己定义
- **允许吸收成熟思想**：可以借鉴 ART、checkpoint、COW、snapshot、segment cleaner，但不直接嵌入现成数据库
- **面向后续演进**：后面可以继续升级索引结构、blob 管理、TTL 清理和分布式场景支持

### 当前架构抽象

目前已经落地的主链路可以概括成：

`Go 数据库层 -> Rust 引擎桥接层 -> engine_v2 -> segment log / checkpoint / snapshot / cleaner`

也就是说，RediGo 现在走的是“**上层 Redis 兼容服务 + 自研底层存储引擎**”这条路线。

***

## 📊 性能指标

### 写入性能（当前目标）

| 指标    | 目标值          | 实测值          |
| ----- | ------------ | ------------ |
| 吞吐量   | > 200K ops/s | \~150K ops/s |
| 持久化延迟 | < 1ms        | 持续优化中        |
| 恢复闭环  | 可恢复          | 已具备基础能力      |

### 读取性能

| 场景              | 目标延迟    | 实测延迟    |
| --------------- | ------- | ------- |
| 缓存命中            | < 0.5ms | < 0.3ms |
| 缓存未命中           | < 10ms  | < 5ms   |
| Bloom Filter 过滤 | O(1)    | O(1)    |

### 内存效率

- 当前 reader cache 已可用，后续会继续补更细粒度缓存
- 大 value 分离、索引结构升级、空间回收策略仍在持续演进

对象存储 offload 属于默认关闭的可选实验能力，不是 RediGo 的核心依赖；当前主线仍然是把**自研本地存储引擎**先做扎实，日常开发、测试和大多数单机场景都不需要引入 MinIO。

***

## 🧪 测试

### 运行单元测试

```bash
go test ./cmd/... ./config ./internal/... ./pkg/... ./tests/...
```

### 运行竞态检测（推荐）

```bash
go test -race ./cmd/... ./config ./internal/... ./pkg/... ./tests/...
```

在 Windows 上 `-race` 需要启用 CGO 并确保 `gcc` 可用（例如 MSYS2 UCRT64 的 `C:\msys64\ucrt64\bin` 在 PATH 中）。

### 运行特定包测试

```bash
go test ./internal/rustengine -v
go test ./internal/database -v
go test ./internal/command -v
```

### 性能基准测试

当前性能基准以新引擎链路为主，基准脚本与指标会随 `engine_v2` 的演进持续调整。

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
- **存储引擎评估**: [`docs/storage_engine_rewrite_assessment.md`](docs/storage_engine_rewrite_assessment.md) - 对当前存储引擎的主要问题、是否值得重写、语言选型与落地路径的整理。
- **引擎架构说明**: [`docs/engine_v2_architecture.md`](docs/engine_v2_architecture.md) - 当前自研引擎的模块、恢复、cleaner 与 snapshot 设计说明。
- **问题修复记录**: [`docs/system_issues_fix.md`](docs/system_issues_fix.md) - 现有系统问题、修复思路与迁移过程记录。

### 外部参考

- [Redis Protocol Specification](https://redis.io/topics/protocol)
- [The Adaptive Radix Tree: ARTful Indexing for Main-Memory Databases](https://db.in.tum.de/~leis/papers/ART.pdf)
- [Architecture of a Database System](https://dsf.berkeley.edu/papers/fntdb07-architecture.pdf)

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

### 已完成

- ✅ 基础 Redis 命令支持
- ✅ gnet 高性能网络层
- ✅ 分段锁并发优化
- ✅ 多数据库支持
- ✅ 过期键管理
- ✅ Rust 自研存储引擎主线接回 Go

### 当前主线

- ✅ `engine_v2` 基础读写链路
- ✅ 崩溃恢复 / checkpoint / snapshot / cleaner
- ✅ `load_all` / `lazy_load`
- ✅ Go <-> Rust 桥接层
- 进行中：继续完善自研引擎的数据结构、空间管理、批量语义和缓存体系

### 后续路线

- [ ] 继续深化自研索引结构与内存布局
- [ ] 完善 blob 管理与空间回收
- [ ] 增强批量写入与恢复语义
- [ ] 构建更稳定的后台调度与清理机制
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

*最后更新时间：2026-07-15*
