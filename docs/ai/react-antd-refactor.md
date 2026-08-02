# GoHermit React + Ant Design 5 重构计划

## 目标与边界

将 GoHermit Workbench 的展示层统一到 React、TypeScript、Ant Design 5、Vite 和现有 React Router。React 只消费现有 Go API 投影；Session/Run、Plan、Approval、Verification、Tool、Recovery、EmployeeTask 和 Session SSE 仍是唯一事实来源。

本次不修改后端状态机、API 契约、持久化 Schema、SSE 协议、Provider 行为、隐藏 Worker 安全边界或浏览器敏感信息存储策略。

## 审计结论

| 区域 | 当前状态 | 重构动作 |
|---|---|---|
| Shell | React 路由和基础 i18n 已存在，但 Layout、移动导航和 session drawer 仍以自定义 DOM/CSS 为主 | 使用 Ant Design `Layout`、`Sider`、`Header`、`Menu`、`Drawer`、`Breadcrumb`，保留 URL 和焦点契约 |
| Dashboard | 真实 API 已接入，但 KPI、表格、readiness 使用自定义 panel/table | 使用 `Card`、`Statistic`、`List`、`Table`、`Badge`、`Alert`、`Skeleton` |
| Employees | 九步向导、详情 tabs、Skills/Knowledge/Memory/Projects/Tasks/Activity 功能完整，视觉组件不统一 | 使用 `Steps`、`Form`、`Tabs`、`Descriptions`、`Table`、`List`、`Tag`、`Result` |
| Tasks | Start/Resume/Cancel、Session SSE 和真实投影已接入，详情布局仍自定义 | 使用 `Table`、`Drawer`、`Timeline`、`Collapse`、`Alert`、`Progress` |
| Agent | Session/Run/SSE 行为完整，消息和控制区域仍混用手写按钮/输入 | 使用 `Layout`、`List`、`Input.TextArea`、`Button`、`Tag`、`Timeline`、`Collapse` |
| Loops | Definition、Team mapping、Invocation 和 SSE 功能完整，表单与状态卡片仍自定义 | 使用 `Form`、`Steps`、`Table`、`Descriptions`、`Alert`、`Timeline` |
| Settings | 部分已使用 Ant Design Button/Input，Provider 卡片和通知区域仍未统一 | 使用 `Card`、`Tabs`、`Form`、`Input`、`Select`、`Alert`、`Descriptions` |

## 设计系统

- 产品类型：本地优先的 AI Engineering Workbench / Operations Dashboard。
- 视觉方向：数据密度适中、专业克制、状态优先；避免后台模板拼接感。
- 8px 间距体系：4/8/12/16/24/32/40/48。
- Ant Design `ConfigProvider` 统一 `colorPrimary`、layout/container 背景、边框、圆角、控件高度、字体、阴影和动效时长。
- 语义颜色统一映射：success、warning、error、processing、default；状态不只依赖颜色，同时显示文字或图标。
- 默认中文，保留即时中英文切换；不加载远程字体、图片或 CDN。
- 长 ID、Digest、路径和错误使用 `Typography.Text` 的省略/复制能力，不撑破网格。
- 交互反馈统一使用 Button loading、`message`/`notification`、`Modal.confirm`、`Alert`、`Skeleton`、`Empty` 和 `Result`。
- 使用 Lucide 或 Ant Design SVG 图标，不使用 Emoji 作为结构图标；尊重 `prefers-reduced-motion`。

## 分阶段迁移

1. **Foundation**：ConfigProvider token、Ant Design Layout/Sider/Header/Menu/Drawer、PageHeader、StatusTag、ErrorState、EmptyState、ConfirmAction、Toast/Notification。
2. **Dashboard + common**：真实 KPI、Workspace/Provider readiness、最近运行、Approval/Verification 摘要，以及统一 loading/error/offline 状态。
3. **Employees**：列表筛选/分页、九步 Steps + Form、详情 Tabs、Skills/Knowledge/Memory/Projects/Tasks/Activity 面板；保留 expected revision、Dry Run、只读和确认行为。
4. **Tasks + SSE**：Table/Drawer/Timeline/Collapse；继续使用 Session SSE、sequence high-water、Run filter 和断线保留。
5. **Agent + Loops + Settings**：统一表单、状态、结果、权限、Team Role/Employee 区分和 Provider readiness。
6. **收尾**：删除已替代的手写 DOM/CSS，更新 Go embed/build/E2E，验证 1024px、1440px、键盘、错误、断线和无敏感缓存。

## 验收矩阵

- React：typecheck、零 warning lint、component tests、API error tests、SSE reconnect/dedup/isolation tests。
- Browser：Dashboard/Employees/Tasks/Loops 关键路径至少重复 10 次；刷新、返回/前进、1024px、无水平滚动、无 Console error。
- Backend 回归：`go test ./...`、race、vet、CLI/Web build；不修改 API/Session/Run。
- 运行：每次接受的代码更新都重新 build 并 force-recreate Mac mini 的 `gohermit-web` 容器，验证 `/api/health`、`/api/info` 和真实页面。
## 当前实现状态

Foundation、Dashboard、Employees 列表、Agent、Settings、Loops 与 Tasks 操作区已接入 Ant Design 组件；后端 API、Session/Run 状态机和 SSE 协议保持不变。完整页面迁移继续以真实 API 投影和现有 Playwright 契约为验收依据。
