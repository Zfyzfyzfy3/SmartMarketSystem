package events

import "time"

// OrderCreatedEvent 下单事件
type OrderCreatedEvent struct {
    Event     string       `json:"event"`
    OrderID   string       `json:"order_id"`
    UserID    int64        `json:"user_id"`
    Items     []OrderItem  `json:"items"`
    Total     float64      `json:"total"`
    Status    string       `json:"status"`
    Timestamp int64        `json:"timestamp"`
}

type OrderItem struct {
    SKUId    string  `json:"sku_id"`
    Quantity int     `json:"quantity"`
    Price    float64 `json:"price"`
}

// InventoryUpdatedEvent 库存更新事件
type InventoryUpdatedEvent struct {
    Event     string `json:"event"`
    SKUId     string `json:"sku_id"`
    Change    int    `json:"change"`
    NewStock  int    `json:"new_stock"`
    Timestamp int64  `json:"timestamp"`
}

// AIRecommendationEvent AI 推荐结果
type AIRecommendationEvent struct {
    Event          string   `json:"event"`
    UserID         int64    `json:"user_id"`
    Recommendations []string `json:"recommendations"`
    Timestamp      int64    `json:"timestamp"`
}
