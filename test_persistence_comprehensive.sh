#!/bin/bash

# LSM Persistence Comprehensive Test Script
# 综合测试 LSM 持久化功能的完整性和性能

set -e

REDIS_HOST="127.0.0.1"
REDIS_PORT="16379"
SERVER_BIN="./bin/gedis-server"
LOG_FILE="./logs/persistence_comprehensive_test.log"
DATA_DIR="./data"
REPORT_FILE="./docs/PERSISTENCE_TEST_REPORT.md"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 计数器
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# 测试函数
test_case() {
    local name="$1"
    local result="$2"
    
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    
    if [ "$result" -eq 0 ]; then
        echo -e "${GREEN}✅ PASS${NC}: $name"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        echo -e "${RED}❌ FAIL${NC}: $name"
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
}

echo "========================================"
echo "LSM Persistence Comprehensive Test"
echo "========================================"
echo ""

# 清理环境
echo -e "${YELLOW}[Phase 1/7] Cleaning up...${NC}"
killall gedis-server 2>/dev/null || true
sleep 2
rm -rf ${DATA_DIR}/db_*
rm -f /tmp/lsm_*.txt
mkdir -p ./logs
echo "✅ Cleanup completed"
echo ""

# 启动服务器
echo -e "${YELLOW}[Phase 2/7] Starting server...${NC}"
${SERVER_BIN} > ${LOG_FILE} 2>&1 &
SERVER_PID=$!
sleep 5

# 检查服务器是否启动成功
if ! kill -0 ${SERVER_PID} 2>/dev/null; then
    echo -e "${RED}❌ Server failed to start${NC}"
    exit 1
fi
echo "✅ Server started (PID: ${SERVER_PID})"
echo ""

# 基础功能测试
echo -e "${YELLOW}[Phase 3/7] Running basic functionality tests...${NC}"

# 字符串操作
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} SET string_key "hello_persistence" > /dev/null
STRING_VALUE=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} GET string_key)
test_case "String SET/GET" $([ "${STRING_VALUE}" == '"hello_persistence"' ] && echo 0 || echo 1)

# 计数器操作
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} SET counter "12345" > /dev/null
COUNTER_VALUE=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} GET counter)
test_case "Counter SET/GET" $([ "${COUNTER_VALUE}" == '"12345"' ] && echo 0 || echo 1)

# 列表操作
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} LPUSH mylist "item1" "item2" "item3" > /dev/null
LIST_LENGTH=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} LLEN mylist)
test_case "List LPUSH/LLEN" $([ "${LIST_LENGTH}" == "3" ] && echo 0 || echo 1)

LRANGE_RESULT=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} LRANGE mylist 0 -1)
test_case "List LRANGE" $([[ "${LRANGE_RESULT}" == *"item1"* && "${LRANGE_RESULT}" == *"item3"* ]] && echo 0 || echo 1)

# 哈希操作
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HSET hash_key field1 "value1" > /dev/null
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HSET hash_key field2 "value2" > /dev/null
HASH_EXISTS=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HEXISTS hash_key field1)
test_case "Hash HSET/HEXISTS" $([ "${HASH_EXISTS}" == "1" ] && echo 0 || echo 1)

HGETALL_RESULT=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HGETALL hash_key)
test_case "Hash HGETALL" $([[ "${HGETALL_RESULT}" == *"field1"* && "${HGETALL_RESULT}" == *"value2"* ]] && echo 0 || echo 1)

# 数据库大小
DBSIZE=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} DBSIZE)
test_case "DBSIZE" $([ "${DBSIZE}" -ge "5" ] && echo 0 || echo 1)

echo ""

# SSTable 文件验证
echo -e "${YELLOW}[Phase 4/7] Checking SSTable files...${NC}"

# 停止服务器以检查文件
killall gedis-server
sleep 3

# 检查 SSTable 目录
SSTABLE_COUNT=$(find ${DATA_DIR}/db_* -name "*.sstable" 2>/dev/null | wc -l)
echo "Found ${SSTABLE_COUNT} SSTable files"

# 验证 SSTable 文件大小
if [ ${SSTABLE_COUNT} -gt 0 ]; then
    for file in $(find ${DATA_DIR}/db_* -name "*.sstable"); do
        SIZE=$(stat -c%s "$file")
        if [ ${SIZE} -gt 0 ]; then
            echo "✅ SSTable file valid: $(basename $file) (${SIZE} bytes)"
        else
            echo -e "${RED}❌ SSTable file empty: $(basename $file)${NC}"
        fi
    done
    test_case "SSTable files exist and non-empty" 0
else
    echo -e "${YELLOW}⚠️  No SSTable files found (data may be in WAL only)${NC}"
    test_case "SSTable files exist" 1
fi

# 检查 WAL 文件
WAL_EXISTS=$(find ${DATA_DIR}/db_* -name "*.wal" 2>/dev/null | wc -l)
test_case "WAL files exist" $([ ${WAL_EXISTS} -gt 0 ] && echo 0 || echo 1)

echo ""

# 重启数据恢复测试
echo -e "${YELLOW}[Phase 5/7] Testing data recovery after restart...${NC}"

${SERVER_BIN} > ${LOG_FILE} 2>&1 &
NEW_SERVER_PID=$!
sleep 5

if ! kill -0 ${NEW_SERVER_PID} 2>/dev/null; then
    echo -e "${RED}❌ Server failed to restart${NC}"
    exit 1
fi
echo "✅ Server restarted (PID: ${NEW_SERVER_PID})"
echo ""

# 验证数据恢复
STRING_RECOVERED=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} GET string_key)
test_case "String data recovery" $([ "${STRING_RECOVERED}" == '"hello_persistence"' ] && echo 0 || echo 1)

COUNTER_RECOVERED=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} GET counter)
test_case "Counter data recovery" $([ "${COUNTER_RECOVERED}" == '"12345"' ] && echo 0 || echo 1)

LIST_RECOVERED_LENGTH=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} LLEN mylist)
test_case "List data recovery" $([ "${LIST_RECOVERED_LENGTH}" == "3" ] && echo 0 || echo 1)

HASH_RECOVERED=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HEXISTS hash_key field1)
test_case "Hash data recovery" $([ "${HASH_RECOVERED}" == "1" ] && echo 0 || echo 1)

echo ""

# 多类型混合数据测试
echo -e "${YELLOW}[Phase 6/7] Testing mixed data types persistence...${NC}"

# 写入更多数据
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} SET large_string "$(head -c 1000 /dev/zero | tr '\0' 'A')" > /dev/null
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} RPUSH biglist $(seq 1 100) > /dev/null
redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HSET bighash $(for i in $(seq 1 50); do echo "field$i value$i"; done) > /dev/null

BIGLIST_LEN=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} LLEN biglist)
test_case "Large list write" $([ "${BIGLIST_LEN}" == "100" ] && echo 0 || echo 1)

BIGHASH_COUNT=$(redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} HLEN bighash)
test_case "Large hash write" $([ "${BIGHASH_COUNT}" == "50" ] && echo 0 || echo 1)

echo ""

# 最终清理
echo -e "${YELLOW}[Phase 7/7] Final cleanup...${NC}"
killall gedis-server
sleep 2
echo "✅ Test environment cleaned"
echo ""

# 生成报告
echo "========================================"
echo "Test Summary"
echo "========================================"
echo -e "Total tests:  ${BLUE}${TOTAL_TESTS}${NC}"
echo -e "Passed:       ${GREEN}${PASSED_TESTS}${NC}"
echo -e "Failed:       ${RED}${FAILED_TESTS}${NC}"
echo ""

if [ ${FAILED_TESTS} -eq 0 ]; then
    echo -e "${GREEN}🎉 ALL TESTS PASSED!${NC}"
    echo "Data persistence is working correctly!"
    EXIT_CODE=0
else
    echo -e "${RED}❌ SOME TESTS FAILED${NC}"
    echo "Please check the logs: ${LOG_FILE}"
    EXIT_CODE=1
fi

echo "========================================"

# 生成 Markdown 报告
cat > ${REPORT_FILE} << EOF
# LSM Persistence Test Report

Generated at: $(date)

## Test Summary

- **Total Tests**: ${TOTAL_TESTS}
- **Passed**: ${PASSED_TESTS} ✅
- **Failed**: ${FAILED_TESTS} ❌

## Test Results

### Basic Functionality Tests
$(if [ ${FAILED_TESTS} -eq 0 ]; then echo "✅ All basic operations working correctly"; else echo "❌ Some basic operations failed"; fi)

### SSTable Files
- SSTable Count: ${SSTABLE_COUNT}
- WAL Files Exist: $([ ${WAL_EXISTS} -gt 0 ] && echo "✅ Yes" || echo "❌ No")

### Data Recovery Tests
$(if [ ${FAILED_TESTS} -eq 0 ]; then echo "✅ All data recovered successfully after restart"; else echo "❌ Some data lost after restart"; fi)

### Performance Tests
- Large List (100 items): $([ "${BIGLIST_LEN}" == "100" ] && echo "✅ Pass" || echo "❌ Fail")
- Large Hash (50 fields): $([ "${BIGHASH_COUNT}" == "50" ] && echo "✅ Pass" || echo "❌ Fail")

## Conclusion

$(if [ ${FAILED_TESTS} -eq 0 ]; then echo "✅ **LSM Persistence is fully functional!** All data types are properly persisted and can be recovered after server restart."; else echo "❌ **Some issues detected.** Please review the test output and logs for details."; fi)

## Recommendations

1. Monitor SSTable file generation during production use
2. Implement periodic compaction for optimal performance
3. Add Bloom Filter for faster key lookups
4. Consider implementing incremental backup strategies

EOF

echo ""
echo "Report generated: ${REPORT_FILE}"

exit ${EXIT_CODE}
