# LSM 持久化功能使用说明

## 快速开始

### 1. 启动服务器

```bash
cd /home/tzj/GoLang/RediGo
./bin/gedis-server
```

### 2. 连接测试

```bash
redis-cli -h 127.0.0.1 -p 16379
```

### 3. 基本操作

```redis
# 字符串
SET mykey "hello world"
GET mykey

# 列表
LPUSH mylist "item1" "item2" "item3"
LRANGE mylist 0 -1

# 哈希
HSET myhash field1 "value1"
HGET myhash field1
```

### 4. 验证持久化

```bash
# 查看数据库大小
DBSIZE

# 重启服务器
killall gedis-server
./bin/gedis-server

# 验证数据恢复
GET mykey
LRANGE mylist 0 -1
```

## 配置选项

### 冷启动策略

在 `config/config.yml` 中配置：

```yaml
cold_start_strategy: "load_all"   # 启动时全量加载
# cold_start_strategy: "lazy_load"  # 按需加载（推荐）
# cold_start_strategy: "no_load"    # 不加载（默认）
```

### LSM 引擎选项

```go
options := &LSMOptions{
    MemTableSize:    4 << 20,      // 4MB
    SSTableSize:     64 << 20,     // 64MB
    CacheSize:       128 << 20,    // 128MB
    UseBloomFilter:  true,         // 使用 Bloom Filter
}
```

## 运行测试

### 综合测试脚本

```bash
chmod +x test_persistence_comprehensive.sh
./test_persistence_comprehensive.sh
```

### 测试覆盖

- ✅ 基础功能测试（7 项）
- ✅ SSTable 文件验证（2 项）
- ✅ 数据恢复测试（4 项）
- ✅ 性能压力测试（2 项）

### 查看测试报告

```bash
cat docs/PERSISTENCE_TEST_REPORT.md
```

## 监控和调试

### 查看日志

```bash
tail -f logs/gedis.log
```

### 检查 SSTable 文件

```bash
find data/db_* -name "*.sstable" -ls
```

### 检查 WAL 文件

```bash
find data/db_* -name "*.wal" -ls
```

## 关键特性

### 1. 强制刷写

关闭服务器时，无论 MemTable 大小，都会强制刷写到 SSTable：

```
[INFO] Forcing flush of MemTable with size 123 bytes...
[INFO] MemTable flushed successfully (forced)
```

### 2. SSTable 验证

启动时自动验证所有 SSTable 文件：

```
[INFO] Scanning SSTable files in ./data/db_0/sstable
[INFO] Found 2 SSTable files
[INFO] SSTable 000001.sstable: size=1024 bytes, keys=5
[INFO] SSTable 000002.sstable: size=2048 bytes, keys=10
```

### 3. 异步关闭

Listener 关闭带超时机制，避免阻塞：

```
[INFO] Closing listener with timeout...
[INFO] Listener closed successfully
```

## 故障排查

### 问题 1: 重启后数据丢失

**原因**: 冷启动策略配置为 `no_load`

**解决**: 修改为 `load_all` 或 `lazy_load`

### 问题 2: SSTable 文件为空

**原因**: 数据还在 WAL 中，未触发刷写

**解决**: 正常关闭服务器会强制刷写

### 问题 3: 端口被占用

**解决**: 
```bash
killall gedis-server
sleep 2
./bin/gedis-server
```

## 性能调优

### 写入优化

- 增大批量写入大小
- 启用 Snappy 压缩
- 调整 MemTable 大小

### 读取优化

- 启用 Block Cache
- 使用 Bloom Filter
- 选择合适的冷启动策略

### 空间优化

- 定期执行 Compaction
- 启用压缩算法
- 清理过期数据

## 最佳实践

1. **生产环境**: 使用 `lazy_load` 策略
2. **测试环境**: 使用 `load_all` 策略
3. **开发环境**: 使用 `no_load` 策略

4. **定期备份**: 结合 RDB 和 AOF
5. **监控指标**: SSTable 数量、Compaction 频率
6. **告警设置**: 磁盘空间、写入延迟

## 参考资料

- [PERSISTENCE_FIX_REPORT.md](./PERSISTENCE_FIX_REPORT.md) - 修复报告
- [PERSISTENCE_OPTIMIZATION_REPORT.md](./PERSISTENCE_OPTIMIZATION_REPORT.md) - 优化报告
- [LSM Engine 实现文档](../internal/persistence/README_IMPLEMENTATION.md)
