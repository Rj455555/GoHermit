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

## 第二轮：深层配置与多端收口

基线为 `d08555c`。本轮只深化 Employee、Tasks 和 Loops/Team/Mission 的展示层；继续复用第一轮 `ConfigProvider` token、React Router、现有 API Client、原生 `EventSource` 和按 Session 共享的 SSE registry。没有新增 API、Task SSE、浏览器执行状态机或后端状态转换。

### 实际迁移页面

- Employee：受控且 URL-owned 的 Overview、Settings、Skills、Knowledge、Memory、Projects、Loops、Tasks、Activity Tabs；刷新与浏览器前进/后退恢复当前 Employee 和 Tab。
- Settings：Ant Design vertical `Form`、真实 model readiness、expected revision、冲突保留、dirty navigation guard、保存去重及 Disable/Enable/Archive 确认；archived 全面只读。
- Skills：Catalog 与 persisted bindings 并集；身份严格使用 `skill_id + version + digest`；Native JSON 就地校验并提交服务端校验，Adapter 固定空配置/零能力，missing/drift 明确移除或升级。
- Knowledge：Source Table/Card、Workspace-only 创建、搜索、refresh/delete 确认与 Citation Drawer；不支持远程 URL 或 Home 扫描。
- Memory：Candidate/Fact Card/List，accept/reject/edit/forget 使用现有 API；保留 provenance 引用，不展示私有推理、原始工具参数或无界输出。
- Projects、Employee Tasks、Activity：只显示当前 Service Workspace、复用全局 Task 状态与 bounded lifecycle/reference metadata。
- Tasks：列表过滤继续 URL-owned；桌面 Table、手机 Card；详情按 Summary → Run Timeline → Plan → Tool → Approval → Verification → Artifacts 排列，手机使用 safe-area sticky action bar；仍只订阅 Session SSE。
- Loops：列表 Table/Card、Definition vertical Form、精确 argv editor、Dry Run、History；TeamTemplate 用 `Form.List`/Role Card，Role 与 Employee 分离，明确 Employee default 与 Mission override；Mission 使用 Progress、Timeline、WorkItem Table/Card、Worker Drawer、Plan/Approval/Verification/Handoff Tabs。
- Hidden Worker：不生成可导航链接、不订阅 hidden Session SSE、不渲染消息、模型输出、Tool、Plan、Approval 或 Employee 私有 Memory。

### 响应式策略

| 数据区域 | 桌面/平板 | 手机 |
|---|---|---|
| Employee Skills | `Table`；Digest 局部省略、复制和 Drawer | `Card/List`；单列配置、全宽 JSON editor |
| Employee Knowledge | `Table`；Citation 由操作进入 Drawer | `Card/List`；操作进入 Dropdown，Citation 近全屏 Drawer |
| Employee Memory | 双列 Candidate/Fact Card | 单列 Card/List，影响性操作垂直排列 |
| Employee Tasks | `Table` | 状态 Card/List，主操作保留 |
| Employee Activity | 单列 `Timeline` | 单列 `Timeline`，引用安全换行 |
| Global Tasks | `Table` | Card/List；筛选单列、详情顺序化 |
| Loop definitions/history | `Table` | Card/List，次要操作收纳 |
| Team roles | 响应式 Role Card 网格 | `Form.List` 单列 Role Card，不使用超宽 Table |
| Mission WorkItems | `Table` + Worker Drawer | Card/List + 近全屏 Worker Drawer |

所有页面使用 Ant Design Grid/Flex/Row/Col 和 CSS media query，不使用 User-Agent 或 `transform: scale()`。内容 padding 为 ≥1440px 24px、1024–1439px 16–24px、768–1023px 16px、360–767px 12–16px；移动端主要目标至少 44px，Drawer/Modal 含 top/bottom safe-area，页面级水平溢出作为 Playwright 断言。

### Design Token 与旧实现清理

第二轮没有创建页面私有颜色、圆角或阴影 token；沿用第一轮全局 primary/background/border/radius/control-height/motion/status token。新增样式只负责 responsive geometry、安全换行、Table→Card 切换和 safe-area action bar。

已移除 Employee 旧详情 renderer，并以唯一的 Ant Design Settings、Skill、Knowledge、Memory、Loop/Team Form、Task projection 和 Timeline 实现代替旧 DOM。旧 API wrapper、Toast、Modal 和 SSE wrapper没有复制；仍保留的共享 shell/Agent/Settings 样式属于第一轮已迁移页面，不在本轮删除范围。

本轮明确删除了不再引用的 `SessionDrawer.tsx` 与 `PlaceholderPage.tsx`，并从 `EmployeesPage.tsx`、`TasksPage.tsx` 和 `styles.css` 删除已被新页面替代的手写详情 DOM、表单布局、旧状态色与重复 responsive selector。基线中不存在可单独删除的 legacy browser JS 文件；共享 `ConfirmDialog`、ToastRegion、API Client、Session SSE registry 和第一轮已迁移的 Agent/Settings 控件继续保留，因为它们仍是唯一在用实现。

### API 与 SSE 边界证据

- Mac mini 容器现场验收补充证明：Go Web CSP 仅为 Ant Design 运行时样式放开 `style-src 'unsafe-inline'`；`script-src` 继续严格限定为 `'self'`，且不允许 `unsafe-inline` 或 `unsafe-eval`。Docker Playwright 会监听并拒绝 CSP Console violation。
- 业务 API route、DTO、Store Schema、Session/Run/Team 状态机均未修改；唯一 Go 调整是上述静态 Web CSP 样式边界及其测试。
- 新增的 `employee_assignments` 解码只是消费 Go 已返回的 bounded public metadata；严格长度、数量、ID 和 WorkItem 关联校验，不扩大响应面。
- Task、Loop Invocation 时间线继续调用现有 `useSessionEvents(sessionId, runId)`；registry/high-water 仍按 Session，Run 只作 subscriber filter。
- Hidden Worker Session 从不传给 `useSessionEvents`，也不作为 React route/link 输出。

### 验收证据与偏差

- Unit：Employee、Tasks、Loops 聚焦测试覆盖 expected revision、冲突/失败保留、精确 Skill identity、argv round-trip、authoritative Task context、delayed owner request isolation 和 hidden projection。
- Browser projects：`desktop-chrome`、`tablet`、`mobile-chrome`、`mobile-safari`；关键 Employee/Tasks/Loops 用例在桌面和两个手机项目执行 `--repeat-each=10`。
- Frontend：frozen install、typecheck、zero-warning lint、185/185 Vitest 与 coverage 通过；coverage 为 statements/lines 92.96%、branches 80.04%、functions 80.31%。生产构建通过。
- Browser：统一 `pnpm test:e2e` 为 126 passed、14 个按 viewport 条件跳过、0 failed；desktop 为 33/2、tablet 31/4、mobile Chrome 31/4、mobile Safari 31/4。Employee/Tasks/Loops 三项目稳定性复跑为 350 passed、10 个条件跳过、0 failed；修复后的路由 breakpoint 用例额外四项目 40/40 通过。
- Go：`go test ./internal/web -count=1`、`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、`go build ./cmd/hermit`、`go build ./cmd/hermit-web` 与 `docker compose config` 全部通过。
- Dist：两次构建文件集合和 SHA-256 完全一致；排序清单 SHA-256 为 `16d7aaf0809ef89c4a15412bcafb4ab4d07daaf9b9e2d6a80889660d75f84063`，未生成 sourcemap，未包含工作区绝对路径。构建产物按本轮交付约束不进入提交。
- 视觉截图输出到 `/tmp/gohermit-react-antd-round2-screenshots`，不进入 Git；共 28 张，尺寸包含 1440×900、1280×800、1024×768、768×1024、430×932、390×844、375×812、360×800，并覆盖导航 Drawer、Employee 全 Tabs、Tasks、Team/Mission、Approval、Verification、Loading、Empty、Error 与 archived 只读。人工抽查 360px/390px 的页面级横向滚动、长 Digest/路径、安全换行、Card 间距、Tabs/Drawer/Modal、sticky action bar 和 safe-area，未发现阻断问题。
- Browser console/pageerror 监听、200% 文本缩放和 360px 页面级横向溢出断言均通过；测试不使用固定 sleep，而等待具体 request/response、route、SSE 或 DOM 状态。
- 后端当前没有 Employee `description` writable field、Knowledge edit endpoint，亦不持久化“最近一次 Employee Dry Run/Verification”摘要。本轮未伪造这些数据，也未扩大后端契约：Overview 对不可用投影明确显示无权威记录，Knowledge 支持现有 create/refresh/delete。
- Ant Design production bundle 的单 chunk 仍较大，是后续性能候选；本轮不引入新依赖或路由级 code splitting，以避免越过授权边界。

下一轮候选仅记录为：按路由拆包、为现有后端补充可持久化的 Employee 最近 Dry Run/Verification 投影、以及在后端先定义 Knowledge update 契约。未在本轮开始。
