# 电商 + AI 微服务系统架构说明

# 目前所有模块不考虑权限与安全问题

## 一、总体架构概览

本项目是一个基于 **Go（电商部分）** 与 **Python（AI部分）** 的混合微服务系统，通过 **消息队列（Kafka）** 实现异步解耦通信。系统具备高扩展性、高可维护性和高并发承载能力，适合部署在容器化环境（Docker / Kubernetes）中。

---

## 二、架构总览图
┌────────────────────────────────────────────┐
│ API Gateway │
│ 路由转发 / 认证 / 限流 / 负载均衡 │
└────────────────┬───────────────────────────┘
│
┌───────────────▼────────────────────────────────────────────────┐
│ Go 微服务层 │
│ ┌────────────┬──────────┬──────────┬──────────┬──────────┬────┐ │
│ │ user-svc │ product │ order   │  cart │ promo    │ inv│ │
│ │ 用户中心  │商品中心 │ 订单服务 │ 购物车 │ 活动秒杀 │ 库存│ │
│ └────────────┴──────────┴──────────┴──────────┴──────────┴────┘ │
└──────────────────┬──────────────────────────────────────────────┘
│
┌────────▼────────┐
│ Kafka / MQ 总线 │ ← 所有服务的异步通信主干
└────────┬────────┘
│
┌────────────▼─────────────┐
│ Python AI 服务层 │
│ ┌──────────────────────┐ │
│ │ customer_assistant │ │ → 面向客户的智能助手
│ │ merchant_ai │ │ → 面向商家的进货预测
│ └──────────────────────┘ │
└────────────┬─────────────┘
│
┌──────────────▼────────────────┐
│ 数据与存储层 │
│ MySQL / Redis / Milvus / etcd │
└───────────────────────────────┘

---

## 三、模块职责说明

| 模块 | 技术栈 | 职责说明 |
|------|----------|-----------|
| **API Gateway** | Go / Nginx / gRPC-Gateway | 提供统一入口，路由分发到各微服务；负责认证、限流、负载均衡、监控等 |
| **user-service** | Go | 用户注册、登录、资料维护、鉴权服务 |
| **product-service** | Go | 商品上架、详情、分类、价格、库存接口；向 MQ 推送 `product.update` 事件 |
| **cart-service** | Go | 用户购物车增删改查；监听 `product.update` 事件更新缓存 |
| **order-service** | Go | 下单、支付、订单状态流转；向 MQ 发布 `order.created` / `order.paid` 事件 |
| **inventory-service** | Go | 库存预扣、扣减、回滚；消费 `order.created` 事件并发出 `inventory.updated` |
| **promotion-service** | Go | 活动管理与秒杀逻辑；限流、异步处理高并发下单请求 |
| **AI Service Hub** | Python (FastAPI) | 负责智能助手逻辑，包括客户对话推荐和商家预测分析 |
| **customer_assistant** | Python + LangChain | 面向客户的智能问答系统，基于商品知识库与向量搜索实现问答与推荐 |
| **merchant_ai** | Python + Prophet / LSTM | 面向商家的智能助手，基于销量与库存预测进货需求 |
| **Message Queue** | Kafka / RabbitMQ | 各服务间通信中枢，解耦依赖，实现事件驱动架构 |
| **Database 层** | MySQL / Redis / Milvus | MySQL 保存业务数据，Redis 提供缓存与分布式锁，Milvus 提供向量检索能力 |

---

## 四、架构特性

- **事件驱动架构（EDA）**：各模块通过 MQ 解耦通信，系统更加松耦合和可扩展。
- **异构多语言服务**：电商核心用 Go 实现，AI 智能部分用 Python 实现。
- **高并发与弹性扩展**：服务可水平扩展，Kafka 支撑高吞吐事件流。
- **可观测性完善**：支持 Prometheus 监控与 Jaeger 调用链追踪。
- **AI 智能能力集成**：内置 LangChain + LLM + 向量数据库，提供智能推荐与预测。

---

## 五、通信模型与协议

| 场景 | 协议 / 方式 | 说明 |
|------|-------------|------|
| 客户端 → 网关 | HTTP/1.1 或 HTTP/2 + REST/JSON | 对外统一暴露 RESTful 接口（后续可支持 WebSocket 订阅通知）。 |
| 网关 → 业务微服务 | gRPC (HTTP/2) | 网关基于服务发现（etcd / Kubernetes Service DNS）动态解析后端实例；使用 protobuf 定义接口，提升性能与类型安全。 |
| 业务服务之间（同步调用尽量避免） | 尽量通过事件异步化 | 鼓励事件驱动，减少直接 RPC 耦合；如确需同步再用 gRPC。 |
| 业务服务之间（异步） | Kafka 事件流 | 通过 Topic 分区设计保障有序性；关键事件使用 Outbox + 幂等消费。 |
| 业务服务 → AI 服务 | gRPC 或 HTTP/JSON | 初期可用 HTTP；后期高频调用（批量推荐）可演进为 gRPC。 |
| Kafka 消费者回调链路 Trace | W3C Trace Context / 自定义 Header | 在消息 Header 中透传 trace_id、span_id 以支持端到端追踪。 |

### gRPC 接口建议目录
`common/proto/<service>.proto` 统一维护，生成代码输出到各服务内部 `internal/` 目录，避免重复定义。

### Kafka 主题初稿
- `order.events`：订单领域事件（created / paid / canceled）
- `inventory.events`：库存扣减 / 回滚 / 更新
- `product.events`：商品信息更新（price_change, detail_update）
- `promotion.events`：活动、秒杀状态广播
- `ai.events`：用户行为、推荐反馈、预测结果

命名规范：`<bounded-context>.events`；消息体包含：`event`, `version`, `payload`, `trace_id`, `ts`。

---

## 六、服务发现与配置


引入 etcd/Consul 注册中心，服务启动注册，网关定期拉取/订阅变化。

健康检查：
- gRPC 健康检查协议（`grpc.health.v1.Health`）
- readiness：依赖（DB、Kafka 连接）完成再标记 SERVING

负载均衡：
- 网关层负责
---

## 七、鉴权与安全（当前阶段说明）



---

## 八、幂等与一致性策略（摘要）

- 关键写操作（创建订单、支付、库存扣减）通过：
	- 客户端：`Idempotency-Key` Header
	- 服务端：持久化请求指纹（Redis / DB）避免重复执行
- 事件发布：Outbox 表（订单库）+ 定时扫描保证至少一次投递
- 消费幂等：消费者侧基于业务主键 + 状态表 / 去重缓存

---

## 九、后续演进路线（里程碑）

| 阶段 | 目标 | 关键交付 |
|------|------|----------|
| M1 | 基础骨架 + Mock 鉴权 + gRPC 通路 | 网关 ↔ user/order/product gRPC 通、Kafka Topic 建立 |
| M2 | 核心业务稳定 + 事件驱动完善 | Outbox、DLQ、监控 & Trace 全链路 |
| M3 | AI 能力可用 | 向量检索、推荐/预测接口接入事件数据 |
| M4 | 安全与权限完善 | 真正 JWT & RBAC、限流、风控策略 |
| M5 | 弹性与灰度 | 自动扩缩容、蓝绿/金丝雀发布、回滚策略 |

---

（本文档后续将补充：事件 Schema、错误码体系、Proto 接口列表）

---

## 附：可观测性与监控

完整监控与可观测性设计（指标、调用链、日志、告警、Dashboard）详见 `docs/monitoring_observability.md`：
 - 所有服务必须暴露：`/healthz`（liveness/readiness），`/metrics`（Prometheus 格式）。
 - Kafka 事件链路需透传 `trace_id`，并在日志中输出。
 - 采样策略：压测阶段全量，生产 5-10% + 慢/错全量。
 - 关键业务指标：订单成功率、库存扣减失败率、秒杀资格成功率、推荐响应耗时、Kafka 消费滞后等。
 - 告警分级：critical（立即处理），warning（观察与调优），info（趋势）。

此处仅做引用，详细字段、指标规范见专用文档。

---

## 十、典型调用链示意

**下单 → 扣库存 → AI 推荐**
1. 用户下单：
User → API Gateway(HTTP) → gRPC: order-service.CreateOrder
order-service(本地事务+Outbox) → 写库 + 记录事件
Outbox Dispatcher → Kafka: `order.created`
inventory-service 消费 `order.created` → 预扣库存 → 发送 `inventory.updated`
AI 服务（可选订阅）消费 `order.created` / `inventory.updated` → 生成行为特征 → 参与后续推荐

2. 推荐召回：
User → Gateway → gRPC/HTTP → ai-service 推荐接口
ai-service 读取特征（Redis / Milvus 向量检索）→ 返回候选商品 →（可异步回写用户画像）

3. 秒杀请求：
User → Gateway（限流 + 令牌桶）→ gRPC: promotion-service.TrySeckill
promotion-service 快速校验库存令牌（Redis 预减）→ 写入排队事件 `promotion.requested`
异步工作进程消费排队事件 → 最终生成订单（调用 order-service 或直接写入并发布事件）
