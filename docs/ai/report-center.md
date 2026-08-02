# 汇报中心与 OpenClaw 微信投递

GoHermit 的员工任务和工作流终态会生成一条 bounded `ReportRecord`。执行层只负责写入报告，不直接依赖微信；汇报中心负责投递、幂等、失败记录和人工重试。

## 投递链路

`Loop/EmployeeTask terminal → loopstore/reports/{id}.json → OpenClaw /hooks/agent → openclaw-weixin → 微信`

OpenClaw 负责微信二维码登录、配对和渠道凭据。GoHermit 只发送结构化终态摘要，不保存微信登录凭据、提示词、工具参数、原始工具输出或私有推理。OpenClaw 通道由环境变量配置：

```text
GOHERMIT_OPENCLAW_URL=http://host.docker.internal:18789/hooks/agent
GOHERMIT_OPENCLAW_HOOK_TOKEN=<dedicated hooks token>
GOHERMIT_OPENCLAW_CHANNEL=openclaw-weixin
GOHERMIT_OPENCLAW_TO=<微信目标>
GOHERMIT_OPENCLAW_AGENT_ID=<optional allowlisted agent>
```

OpenClaw Gateway 必须启用独立 `hooks.token`，不要复用 Gateway auth token；微信插件通过 `openclaw channels login --channel openclaw-weixin` 完成 QR 登录。未配置 OpenClaw 时，现有 SMTP 邮件通道仍可作为 fallback。

## API 与 UI

- `GET /api/reports?limit=100`：汇报中心最近记录。
- `POST /api/reports/{id}/retry`：只重试投递，不创建新的 Task、Session 或 Run。
- `GET /api/settings/notifications`：只返回邮件/OpenClaw readiness 与最近发送错误，不返回 token。
- Web 路由 `/reports`：展示投递状态、来源、摘要、错误和重试入口。

报告文件上限 12 KiB，单个 owner store 最多 512 条。记录状态 `pending`、`sent`、`failed`；已发送记录通过现有 notification marker 和 report delivery evidence 双重幂等。所有错误显示为 bounded 文本。
