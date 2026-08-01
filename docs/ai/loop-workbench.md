# Loop 工作台与完成通知

GoHermit 的 Loop 不是一段隐藏的提示词，而是一份可审计的契约和一次可恢复的执行记录。

## 核心闭环

```text
触发 → 编排器 → 执行器 → 验证器 → 证据 → state/log → 下一轮进化
```

- **契约**：Loop Definition 的 `contract` 描述目标、边界、SOP、完成标准和停止条件；服务端同时生成 `LOOP.md`。
- **状态**：`state.json` 由权威 Invocation/Session/Run 投影生成，记录最近状态、运行次数、成功次数、失败次数和下次触发时间。
- **编排器**：读取契约和触发信息，创建一次 Invocation；不会创建第二套执行状态机。
- **执行器**：复用已有 Session/Run，保留模型、工具、审批、工作区租约和恢复语义。
- **验证器**：使用 Verification Recipe 和已有测试/验证证据判断是否完成；失败结果不能伪装为成功。
- **证据**：UI 只投影计划、工具、检查、Session/Run 和 Artifact 元数据，不显示私有推理或密钥。
- **进化**：终态投影刷新 state/log；契约改进信号在工作台中显示为边界、SOP、触发器和仪表盘更新。

## 完成通知

每个 Invocation 或 Employee Task 进入 `completed`、`failed`、`blocked`、`skipped` 或 `cancelled` 后，控制平面尝试发送一封精简纯文本邮件。默认收件人为 `1143130628@qq.com`，收件人可通过 `GOHERMIT_NOTIFY_EMAIL_TO` 覆盖。

SMTP 配置只从进程环境读取：

```text
GOHERMIT_SMTP_HOST=smtp.qq.com
GOHERMIT_SMTP_PORT=465
GOHERMIT_SMTP_USERNAME=your-mailbox@qq.com
GOHERMIT_SMTP_PASSWORD=<QQ 邮箱授权码>
GOHERMIT_SMTP_FROM=your-mailbox@qq.com
```

授权码不进入仓库、API 响应、日志或通知正文。`GET /api/settings/notifications` 只返回是否就绪、收件人、发件人、主机和最近一次错误/发送时间。成功发送后，`<loop-store>/notifications/<invocation-id>.json` 作为幂等标记；重启或重复读取不会重复发送已确认的终态通知。

## UI 入口

Loop 详情和 Invocation 详情页的“编排工作台”展示同一条阶段链路，并将触发器、Session 绑定、验证检查和证据摘要与现有运行日志关联。Settings 的“任务完成通知”卡片展示 SMTP readiness；它不会阻塞其他设置页面加载。
