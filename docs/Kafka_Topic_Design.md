| Topic 名称           | 描述          | 发布者               | 消费者                                              |
| ------------------ | ----------- | ----------------- | ------------------------------------------------ |
| `order_events`     | 订单创建 / 支付事件 | order-service     | inventory-service, promotion-service, ai-service |
| `inventory_events` | 库存变化事件      | inventory-service | merchant_ai, ai-service                          |
| `product_events`   | 商品变更事件      | product-service   | cart-service, ai-service                         |
| `promotion_events` | 秒杀 / 活动事件   | promotion-service | order-service, ai-service                        |
| `ai_events`        | AI 计算结果     | ai-service        | order-service, product-service                   |
(1) Order Created / Paid
{
  "event": "order.created",
  "order_id": "ORD12345",
  "user_id": 1001,
  "items": [
    {"sku_id": "SKU123", "quantity": 2, "price": 1299.0}
  ],
  "total": 2598.0,
  "status": "CREATED",
  "timestamp": 1730001234
}
(2) Inventory Updated
{
  "event": "inventory.updated",
  "sku_id": "SKU123",
  "change": -2,
  "new_stock": 98,
  "timestamp": 1730001250
}
(3) Product Update
{
  "event": "product.updated",
  "sku_id": "SKU123",
  "name": "ThinkPad E14",
  "price": 1299.0,
  "stock": 98,
  "status": "AVAILABLE",
  "timestamp": 1730001300
}
(4)Promotion Event
{
  "event": "promotion.seckill.success",
  "activity_id": "ACT20251029",
  "user_id": 1001,
  "sku_id": "SKU123",
  "quantity": 1,
  "timestamp": 1730001350
}
(5) AI Recommendation Result
{
  "event": "ai.recommendation.ready",
  "user_id": 1001,
  "recommendations": ["SKU888", "SKU999"],
  "timestamp": 1730001400
}

