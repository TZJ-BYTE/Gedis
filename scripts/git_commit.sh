#!/bin/bash

# RediGo Git 清理和提交脚本
# 用法：./scripts/git_commit.sh "提交信息"

set -e

echo "======================================"
echo "RediGo Git 清理和提交工具"
echo "======================================"
echo ""

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 检查是否在 Git 仓库中
if [ ! -d .git ]; then
    echo -e "${RED}错误：当前目录不是 Git 仓库${NC}"
    exit 1
fi

# 1. 清理编译产物和临时文件
echo -e "${YELLOW}[1/5] 清理临时文件...${NC}"
rm -f server
rm -f bin/gedis-server 2>/dev/null || true
echo "✅ 清理完成"
echo ""

# 2. 检查测试
echo -e "${YELLOW}[2/5] 运行快速测试验证...${NC}"
go test ./internal/persistence -run TestLSMEnergy_Recovery -timeout 30s > /dev/null 2>&1 && echo "✅ Persistence 测试通过" || echo -e "${RED}⚠️  Persistence 测试失败（继续执行）${NC}"
go test ./internal/database -run TestLSMRecovery -timeout 30s > /dev/null 2>&1 && echo "✅ Database 测试通过" || echo -e "${RED}⚠️  Database 测试失败（继续执行）${NC}"
echo ""

# 3. 显示变更统计
echo -e "${YELLOW}[3/5] 变更统计:${NC}"
git status --short
echo ""
MODIFIED_COUNT=$(git status --short | grep "^ M" | wc -l)
NEW_COUNT=$(git status --short | grep "^??" | wc -l)
DELETED_COUNT=$(git status --short | grep "^ D" | wc -l)
echo "  修改文件：$MODIFIED_COUNT"
echo "  新增文件：$NEW_COUNT"
echo "  删除文件：$DELETED_COUNT"
echo ""

# 4. 添加到暂存区
echo -e "${YELLOW}[4/5] 添加到暂存区...${NC}"
git add -A
echo "✅ 已添加所有变更"
echo ""

# 5. 提交
COMMIT_MSG="$1"
if [ -z "$COMMIT_MSG" ]; then
    COMMIT_MSG="feat: LSM Tree 持久化功能完整实现"
fi

echo -e "${YELLOW}[5/5] 提交变更...${NC}"
echo "提交信息：$COMMIT_MSG"
git commit -m "$COMMIT_MSG"

echo ""
echo "======================================"
echo -e "${GREEN}✅ 提交成功！${NC}"
echo "======================================"
echo ""

# 提示推送
echo "是否推送到远程仓库？(y/n)"
read -r response
if [[ "$response" =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}推送到远程...${NC}"
    git push origin master
    echo -e "${GREEN}✅ 推送成功！${NC}"
else
    echo "💡 稍后可以手动执行：git push origin master"
fi

echo ""
echo "📊 提交统计:"
git log --oneline -5
