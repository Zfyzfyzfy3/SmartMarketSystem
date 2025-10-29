# API 接口设计规范

面向外部客户端统一通过 API Gateway 暴露 HTTP/JSON 接口；网关内部与各业务微服务采用 gRPC 调用；微服务之间尽量通过 Kafka 事件驱动异步通信。

---

## 1. 通用规范

- 所有 HTTP 接口遵循 RESTful 风格，资源使用复数命名（`/products`, `/orders`）。
- 请求与响应均使用 `Content-Type: application/json; charset=utf-8`。
- 响应统一结构包含：`code`, `message`, `data`, `request_id`, `timestamp`。
- 认证（后续阶段）：`Authorization: Bearer <JWT>`；当前阶段可 Mock。
- 幂等性：涉及写操作（创建订单、支付、秒杀尝试）需支持 `Idempotency-Key` Header。
- 时区与时间格式：所有时间字段使用 ISO8601 UTC (`2025-10-29T12:34:56Z`)。
- 分页参数：`page`（默认1），`page_size`（默认20，最大100），响应包含 `total`。
- 排序参数：`sort`，如 `sort=price_desc`；多个字段用逗号：`sort=created_at_desc,price_asc`。
- 过滤参数示例：`category=electronics&keyword=laptop&status=active`。

---

## 2. 统一响应结构

```json
{
  "code": 0,
  "message": "success",
  "request_id": "trace-uuid",
  "timestamp": "2025-10-29T12:34:56Z",
  "data": {}
}
```

错误示例：
```json
{
  "code": 2001,
  "message": "库存不足",
  "request_id": "trace-uuid",
  "timestamp": "2025-10-29T12:35:00Z",
  "data": null
}
```

---

## 3. 用户模块（user-service）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| POST | /api/v1/users | 用户注册 | UserService.CreateUser |
| POST | /api/v1/auth/token | 用户登录获取 JWT | AuthService.Login |
| POST | /api/v1/auth/refresh | 刷新令牌 | AuthService.Refresh |
| GET | /api/v1/users/me | 获取当前用户信息 | UserService.GetMe |
| GET | /api/v1/users/{id} | 获取指定用户信息 | UserService.GetUser |
| PUT | /api/v1/users/me | 更新当前用户资料 | UserService.UpdateUser |

注册请求：
```json
{
  "username": "alice",
  "password": "StrongP@ssw0rd",
  "email": "alice@example.com"
}
```

---

## 4. 商品模块（product-service）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| GET | /api/v1/products | 获取商品列表（支持分页/过滤） | ProductService.ListProducts |
| GET | /api/v1/products/{id} | 获取商品详情 | ProductService.GetProduct |
| POST | /api/v1/products | 新增商品（商家） | ProductService.CreateProduct |
| PUT | /api/v1/products/{id} | 更新商品信息（全量） | ProductService.UpdateProduct |
| PATCH | /api/v1/products/{id} | 部分更新（价格/状态等） | ProductService.PatchProduct |
| PATCH | /api/v1/products/{id}/status | 上/下架状态更新 | ProductService.UpdateStatus |

商品列表响应示例：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": 101,
        "name": "ThinkPad E14",
        "price": 4999.00,
        "currency": "CNY",
        "status": "active",
        "inventory": 57,
        "updated_at": "2025-10-29T12:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 153
  },
  "request_id": "trace-uuid",
  "timestamp": "2025-10-29T12:00:01Z"
}
```

事件：创建/更新商品会发布 `product.updated` 到 Kafka。

---

## 5. 购物车模块（cart-service）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| GET | /api/v1/carts/me | 获取当前用户购物车 | CartService.GetCart |
| POST | /api/v1/carts/me/items | 添加商品到购物车 | CartService.AddItem |
| PATCH | /api/v1/carts/me/items/{item_id} | 修改商品数量 | CartService.UpdateItem |
| DELETE | /api/v1/carts/me/items/{item_id} | 移除商品 | CartService.RemoveItem |

添加请求：
```json
{
  "product_id": 101,
  "quantity": 2
}
```

缓存：购物车信息可缓存于 Redis；商品更新事件 `product.updated` 触发局部缓存失效。

---

## 6. 订单模块（order-service）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| POST | /api/v1/orders | 创建订单（支持 Idempotency-Key） | OrderService.CreateOrder |
| GET | /api/v1/orders/{id} | 获取订单详情 | OrderService.GetOrder |
| GET | /api/v1/orders | 获取当前用户订单列表 | OrderService.ListOrders |
| POST | /api/v1/orders/{id}/payment | 支付订单 | PaymentService.PayOrder |
| PATCH | /api/v1/orders/{id}/cancel | 取消订单 | OrderService.CancelOrder |

创建订单请求：
```json
{
  "items": [
    {"product_id": 101, "quantity": 2},
    {"product_id": 202, "quantity": 1}
  ],
  "address_id": 55,
  "payment_method": "mock"
}
```

事件：
- 成功创建 → `order.created`
- 支付完成 → `order.paid`
- 取消订单 → `order.canceled`

这些事件用于驱动库存扣减或回滚、AI 用户行为分析等。

---

## 7. 库存模块（inventory-service）

（通常后台内部，不一定对外暴露全部接口，可只暴露查询）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| GET | /api/v1/inventory/{product_id} | 查询商品当前可用库存 | InventoryService.GetInventory |

事件消费：
- 消费 `order.created` → 预扣库存，发布 `inventory.updated`
- 消费 `order.canceled` → 回滚库存，发布 `inventory.updated`

---

## 8. 秒杀与活动模块（promotion-service）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| GET | /api/v1/seckills | 获取秒杀活动列表 | PromotionService.ListSeckills |
| GET | /api/v1/seckills/{id} | 获取活动详情 | PromotionService.GetSeckill |
| POST | /api/v1/seckills/{id}/attempt | 尝试参与秒杀（幂等） | PromotionService.AttemptSeckill |
| GET | /api/v1/seckills/{id}/result | 查询用户参与结果 | PromotionService.GetResult |

事件：
- 请求入队 → `seckill.attempted`
- 成功获得资格 → `seckill.succeeded`
- 活动结束 → `seckill.closed`

限流：网关令牌桶 + Redis 预减。

---

## 9. AI 智能助手（ai-service, Python）

| 方法 | 路径 | 描述 | gRPC 方法 |
|------|------|------|-----------|
| POST | /api/v1/ai/customer/chat | 客户对话问答+推荐 | AIService.CustomerChat |
| POST | /api/v1/ai/customer/recommend | 定制化推荐（基于画像） | AIService.CustomerRecommend |
| POST | /api/v1/ai/merchant/forecast | 商家进货需求预测 | AIService.MerchantForecast |

Chat 请求：
```json
{
  "user_id": 123,
  "session_id": "sess-uuid",
  "query": "学生用笔记本电脑推荐"
}
```
Chat 响应：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "reply": "为学生推荐以下机型...",
    "recommendations": [
      {"product_id": 101, "name": "ThinkPad E14"},
      {"product_id": 202, "name": "MacBook Air M2"}
    ]
  },
  "request_id": "trace-uuid",
  "timestamp": "2025-10-29T12:40:00Z"
}
```

Forecast 请求：
```json
{
  "merchant_id": 88,
  "products": [101, 202],
  "horizon_days": 30
}
```
Forecast 响应：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "forecasts": [
      {"product_id": 101, "expected_demand": 120, "confidence": 0.87},
      {"product_id": 202, "expected_demand": 45, "confidence": 0.76}
    ]
  }
}
```

事件：
- 消费用户行为（浏览、下单）事件改进画像：`user.behavior`
- 预测结果（可选）发布：`forecast.generated`

---

## 10. HTTP ↔ gRPC 映射说明

统一通过 Proto 定义服务：`user.proto`, `product.proto`, `order.proto`, `promotion.proto`, `ai.proto`。
网关在收到 HTTP 请求后：
1. 解析路径与方法 → 匹配到内部 gRPC 方法。
2. 附加 `request_id`, `trace_id` 到 gRPC Metadata。
3. 返回 gRPC 响应后封装为统一 JSON。

示例映射：
| HTTP | gRPC |
|------|------|
| POST /api/v1/orders | OrderService.CreateOrder |
| POST /api/v1/orders/{id}/payment | PaymentService.PayOrder |
| GET /api/v1/products/{id} | ProductService.GetProduct |
| POST /api/v1/ai/customer/chat | AIService.CustomerChat |

---

## 11. 与事件驱动的关系

| 触发接口 | 发布事件 | 说明 |
|----------|----------|------|
| POST /api/v1/orders | order.created | 订单主数据写入后 Outbox 投递 |
| POST /api/v1/orders/{id}/payment | order.paid | 支付成功后用于库存最终确认、用户画像更新 |
| PATCH /api/v1/orders/{id}/cancel | order.canceled | 回滚库存、营销补偿 |
| POST /api/v1/products | product.updated | 新增商品也归类为 updated 事件 version=1 |
| PUT/PATCH /api/v1/products/{id} | product.updated | 更新商品缓存/搜索索引 |
| POST /api/v1/seckills/{id}/attempt | seckill.attempted | 用户尝试参与（排队） |
| 内部资格确认流程 | seckill.succeeded | 资格成功，可能后续自动下单 |
| POST /api/v1/ai/merchant/forecast | forecast.generated | 预测结果可异步分发（可选） |

事件统一字段示例：
```json
{
  "event": "order.created",
  "version": 1,
  "trace_id": "trace-uuid",
  "payload": {
    "order_id": 12345,
    "user_id": 678,
    "total_amount": 199.00,
    "items": [
      {"product_id": 101, "qty": 2},
      {"product_id": 202, "qty": 1}
    ],
    "created_at": "2025-10-29T12:34:56Z"
  },
  "ts": "2025-10-29T12:34:56Z"
}
```

---

## 12. 错误码规范

| Code | 分类 | 含义 |
|------|------|------|
| 0 | 通用 | 成功 |
| 1001 | 参数 | 参数错误/校验失败 |
| 1002 | 参数 | 资源不存在（Not Found） |
| 2001 | 认证 | 未认证（需要登录） |
| 2002 | 权限 | 权限不足 |
| 3001 | 业务 | 库存不足 |
| 3002 | 业务 | 秒杀结束 |
| 3003 | 业务 | 订单状态非法 |
| 3004 | 业务 | 幂等冲突（重复请求） |
| 4001 | 外部 | AI 服务不可用 |
| 4290 | 限流 | 请求被限流 |
| 5000 | 系统 | 内部服务器错误 |
| 5001 | 系统 | 外部依赖故障（DB/Kafka） |

---

## 13. 安全与幂等说明（当前阶段）

当前阶段为性能/压测预备：
- 鉴权可 Mock：网关若检测到测试 Header（如 `X-Debug-User`）则注入用户上下文。
- 后续切换至真实 JWT 验证：公钥缓存 + 过期校验。
- 幂等处理：
  - 客户端发送 `Idempotency-Key`（UUID）。
  - 服务端使用 Redis / DB 记录（key → 结果），重复请求直接返回首个结果。

---

## 14. 后续扩展占位

- GraphQL / gRPC-Web 支持（可选）。
- 订阅/通知：WebSocket / SSE 推送订单状态、秒杀结果。
- 批量接口：批量查询商品 `/api/v1/products/batch`。
- 数据一致性：Saga / Outbox 已在架构层说明，后续补独立文档。

---

（本文件后续将与 `architecture.md` 中的通信与事件章节联动维护；新增 Proto/事件 Schema 后同步更新。）