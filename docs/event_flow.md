
---

## 🔄 **4️⃣ docs/event_flow.md**

```markdown
# 消息事件流转说明 (Event Flow)

## 一、核心思想

系统基于 **事件驱动架构（EDA）** 构建。  
各模块不直接调用彼此接口，而是通过 **Kafka Topic** 传递消息事件，实现解耦与异步化。

---

## 二、Topic 划分

| Topic 名称 | 描述 | 发布者 | 订阅者 |
|-------------|------|--------|--------|
| `order_events` | 订单创建/支付事件 | order-service | inventory-svc, ai-service, promotion-svc |
| `inventory_events` | 库存变化事件 | inventory-svc | merchant_ai |
| `product_events` | 商品变更事件 | product-svc | cart-svc, ai-service |
| `promotion_events` | 秒杀活动事件 | promotion-svc | order-svc, ai-service |
| `ai_events` | AI 计算结果事件 | ai-service | order-svc, product-svc |

---

## 三、典型事件流转

### 1️⃣ 下单流程
User -> order-service
order-service -> publish(order.created)
→ Kafka[order_events]
→ inventory-service 消费 -> 扣库存
→ ai-service 消费 -> 学习购买偏好，生成推荐

### 2️⃣ 秒杀流程
promotion-service 发布活动 → Kafka[promotion_events]
用户点击抢购 → 进入限流队列
抢购成功 → publish(seckill.success)
→ order-service 创建订单 → publish(order.created)
→ inventory-service 扣库存

### 3️⃣ 商家进货预测流程
inventory-service → publish(inventory.updated)
→ merchant_ai 消费
→ 运行销量预测模型 (Prophet / LSTM)
→ 生成 forecast 结果
→ publish(ai.forecast.ready)

---

## 四、消息格式示例

### order.created
```json
{
  "event": "order.created",
  "order_id": "ORD12345",
  "user_id": 1001,
  "items": [{"sku_id": "SKU123", "quantity": 2}],
  "total": 2599.0,
  "timestamp": 1730001234
}
inventory.updated
{
  "event": "inventory.updated",
  "sku_id": "SKU123",
  "change": -2,
  "new_stock": 98,
  "timestamp": 1730001250
}
ai.recommendation.ready
{
  "event": "ai.recommendation.ready",
  "user_id": 1001,
  "recommendations": ["SKU888", "SKU999"]
}
五、可靠性与一致性

使用 Kafka事务机制 保证 exactly-once 投递

消费端需实现 幂等机制（基于 message_id 或业务主键）

对关键事务采用 Outbox Pattern 确保事件不丢失

六、事件可视化与监控

推荐集成：

Kafka UI：查看 topic 消息流

Prometheus Exporter：统计消费速率

Jaeger Trace：追踪跨服务事件链路