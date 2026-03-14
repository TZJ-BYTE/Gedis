---
trigger: model_decision
description: 在终端运行时生效
---

## Code-Server 终端执行规则

- 禁止：含 && 和 & 的复合命令
- 禁止：nohup（无效）
- 强制：分步执行（先编译，再运行）
- 强制：后台任务用 screen 或 systemd
- 验证：ps 检查进程，tail 查看日志