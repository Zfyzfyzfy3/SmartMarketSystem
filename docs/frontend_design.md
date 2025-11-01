# 前端设计方案

## 1. 目标与范围
- 构建统一前端门户，覆盖消费者端（商城 + AI 助手）、商家后台（商品/库存/活动管理）、运维观测面板（指标透出）。
- 与 `API Gateway` 暴露的 REST 接口对接，复用 `request_id/trace_id` 便于联动后端可观测性。
- 优先易于落地：单仓库、单页面应用（SPA），开发体验简单，部署可直接挂在 Nginx 或 S3 静态托管。
- 首阶段聚焦压测与功能验证，后续可按角色拆分子项目。

```
┌───────────────┐      ┌────────────────────┐
│ Web SPA (React)│──API→│ API Gateway (REST) │
│  - Portal      │      │ 统一鉴权/转发       │
│  - Dashboard   │←WS?──│ (gRPC ↔ HTTP)      │
└───────────────┘      └────────────────────┘
        │ Trace/Log Header  │
        ▼                   ▼
  LocalStorage Token   可观测性埋点
```

## 2. 技术选型（简单易实现版）
| 领域 | 选型 | 理由 |
|------|------|------|
| 应用框架 | React 18 + Vite + TypeScript | 社区成熟、热启动快、SSR 非必需；TypeScript 提升接口对接安全性。
| 路由 | React Router v6 | 单页路由控制简单，支持嵌套路由与懒加载。
| UI 组件 | Ant Design 5 + Tailwind CSS（可选） | AntD 提供完善组件；Tailwind 辅助快速布局，可按需启用。
| 状态管理 | React Query（远程数据）+ Zustand（轻量全局状态） | Query 处理缓存/请求重试；Zustand 管理用户信息、权限等。
| 网络请求 | Axios + 自封装 API 客户端 | 支持拦截器注入 Token、`trace_id`；统一错误处理。
| 表格/图表 | AntD Table + ECharts | 满足运营看板需求，易集成动态数据。
| 表单验证 | React Hook Form + Zod | 轻量同时保持类型安全。
| 软件质量 | ESLint + Prettier + Husky（可选） | 保证代码风格一致，预提交检查。
| 测试 | Vitest + React Testing Library | 与 Vite 集成简单；覆盖关键页面。
| 部署 | Vite 构建产物 → Nginx 静态目录（`/opt/smartmarket/web`） | 满足容器部署，支持 CDN。

## 3. 项目结构约定
```
frontend/
├─ src/
│  ├─ app/             # 根组件、路由、全局布局
│  ├─ modules/
│  │   ├─ auth/
│  │   ├─ catalog/     # 商品浏览
│  │   ├─ cart/
│  │   ├─ order/
│  │   ├─ promotion/
│  │   ├─ ai/
│  │   ├─ admin/       # 商家、运维大盘
│  │   └─ shared/      # 通用组件
│  ├─ api/             # Axios 实例、接口封装
│  ├─ hooks/           # React Query Hooks
│  ├─ stores/          # Zustand store
│  ├─ utils/           # 工具（format、logging）
│  ├─ assets/
│  └─ index.tsx
├─ public/
└─ vite.config.ts
```

## 4. 路由与页面规划
| 路由 | 角色 | 功能点 | 关联系统接口 |
|------|------|--------|---------------|
| `/login` | 所有 | 登录/Mock 用户选择 | `POST /api/v1/auth/token`（可选）或内部 Mock |
| `/` | 消费者 | 首页、Banner、AI 助手入口 | `GET /api/v1/products`、AI 接口 |
| `/products/:id` | 消费者 | 商品详情、推荐 | `GET /api/v1/products/{id}`；`POST /api/v1/ai/customer/recommend` |
| `/cart` | 消费者 | 购物车管理 | Cart 系列接口 |
| `/checkout` | 消费者 | 订单确认/支付 | `POST /api/v1/orders`、`POST /api/v1/orders/{id}/payment` |
| `/orders` | 消费者 | 订单列表与详情 | `GET /api/v1/orders`、`GET /api/v1/orders/{id}` |
| `/seckill` | 消费者 | 秒杀列表、倒计时 | Promotion 接口 |
| `/seckill/:id` | 消费者 | 秒杀详情、参与结果 | `POST /api/v1/seckills/{id}/attempt`、`GET /api/v1/seckills/{id}/result` |
| `/merchant/products` | 商家 | 商品管理、批量导入 | Product CRUD |
| `/merchant/inventory` | 商家 | 库存监控、补货建议 | Inventory、AI Forecast 接口 |
| `/merchant/promotions` | 商家 | 活动配置、秒杀维护 | Promotion 管理接口（预留） |
| `/ops/overview` | 运维 | 指标大盘、实时 QPS | Prometheus Proxy / 自研 Observability API |
| `/ops/traces` | 运维 | 链路检索跳转 | 与后端可观测平台联动（Trace 链接） |

- 路由采用懒加载，减少首屏包体。
- 角色控制：前端根据用户角色（`consumer` / `merchant` / `ops`）决定可访问菜单。

## 5. 组件与 UI 规范
- **布局**：AntD Layout + 自定义简洁主题（主色 `#1E88E5`）。
- **表单**：统一使用 AntD Form 与 React Hook Form 联动，所有输入字段提供 placeholder 与校验信息。
- **按钮与反馈**：提交操作统一提供 Loading 状态，操作成功使用 `message.success`，失败弹出带 `request_id` 的错误提示。
- **图表**：ECharts 嵌入自适应容器，异步数据加载时显示 Skeleton。
- **可访问性**：提供基础 aria 标签、键盘焦点样式。

## 6. 数据与状态流
- **React Query** 负责数据获取与缓存，默认自动重试 2 次，缓存时间 5 分钟。
- **Zustand** 保持轻量全局状态：`authStore`（token、用户信息、角色）、`uiStore`（主题、菜单折叠）。
- **乐观更新**：购物车、订单支付等关键操作使用 React Query Mutation + 乐观更新确保体验；失败回滚。
- **并行请求**：页面加载时通过 `Promise.all` 同步获取多个数据源，减少渲染次数。

## 7. 鉴权流程
1. 登录成功后保存 `access_token` 到 `localStorage` + 内存。
2. Axios 请求拦截器：
   - 自动附加 `Authorization: Bearer token`。
   - 注入 `x-request-id`（UUID），配合后端生成 `trace_id`。
3. 响应拦截器：
   - 401 → 清理本地状态，跳转登录。
   - 429/5xx → 全局弹窗，展示 `request_id` 便于排查。
4. Mock 模式：若未启用真实鉴权，提供本地用户选择器，直接设置角色并生成伪 token。

## 8. 错误与异常处理
- 对象级错误提示包含 `request_id`。
- 接口级错误分类映射（参考 `docs/api_spec.md` 错误码）。
- 全局错误边界捕获不可恢复异常，展示友好页面。
- 对 `seckill` 等高频接口增加退避与熔断（React Query 配置 `retryDelay`）。

## 9. 可观测性埋点
- 收集 Web Vitals（CLS、LCP、FID）送至后端 `/observability/webvitals`（预留）。
- 自定义事件：
  - `ai_chat_invoke`（包含成功/失败、耗时）。
  - `seckill_attempt`（包含结果）。
  - `order_create`。 
- 在 Axios 拦截器中打印结构化日志（仅 DEV），方便本地调试。
- 前端日常日志不落盘，必要时通过后端 API 上传。

## 10. 性能与体验优化
- 首屏资源：使用 Vite SplitChunks + CDN；AntD 采用按需加载（babel-plugin-import）。
- 图片处理：统一走 CDN，使用懒加载与 `srcset`。
- AI 对话框采用流式响应（后端支持时），前端使用 EventSource 或 WebSocket。
- 秒杀页面使用 `requestAnimationFrame` 更新倒计时，避免 setInterval 漂移。

## 11. 测试与质量
- 单元测试：组件逻辑、hooks。
- 集成测试：关键用户流程（登录 → 下单 → 支付）。
- E2E（后续）：可选 Playwright。
- CI：执行 `pnpm lint` + `pnpm test`；压测阶段可构建 Docker 镜像。

## 12. 构建与部署
- 包管理：`pnpm`。
- 环境变量：`.env`、`.env.staging`、`.env.production`；通过 `import.meta.env` 使用。
- 构建命令：`pnpm build`（产物在 `dist/`）。
- 部署方式：Nginx 容器（`gateway/frontend`），配置反向代理 `/api` → API Gateway；静态路由回退至 `index.html`。
- CDN（可选）：上传 `dist` 至对象存储（OSS/S3），开启 gzip/brotli。

## 13. 迭代规划
| Sprint | 重点 | 说明 |
|--------|------|------|
| S0 | 基础框架搭建 | Vite + 路由 + 全局主题，Mock 接口 |
| S1 | 消费者流程闭环 | 商品浏览 → 购物车 → 下单 → 支付（Mock） |
| S2 | 秒杀与 AI 集成 | 秒杀倒计时、AI 对话/推荐对接真实接口 |
| S3 | 商家后台 | 商品/库存管理、预测展示 |
| S4 | 运维看板 | 可观测性大盘、Trace 跳转 |
| S5 | 优化与测试 | E2E、性能调优、无障碍提升 |

## 14. 风险与缓解
- **接口变动频繁**：通过 TypeScript 接口定义 + 自动生成（后续可接入 OpenAPI 生成器）。
- **角色权限复杂**：保持前端逻辑简单，后端负责授权；前端仅用于隐藏入口。
- **AI 响应延迟**：界面提供 Streaming/loading 状态，超时 10s 回退提示。
- **观测数据量大**：运营看板仅展示关键指标，复杂分析跳转 Grafana。

---
本方案旨在快速上线 MVP，同时为后续拆分（例如消费者独立 App、商家独立控制台）保留扩展空间。