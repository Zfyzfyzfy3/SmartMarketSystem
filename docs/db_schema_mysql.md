# MySQL 表结构设计总览

该文档描述电商 + AI 微服务系统中需要持久化到 MySQL 的核心业务数据表结构、命名规范、公共列、索引与约束、以及与事件驱动/幂等的配合策略。

## 1. 设计原则
- 使用 InnoDB 引擎，UTF8MB4 字符集。
- 主键优先使用 BIGINT UNSIGNED（雪花或自增），跨服务引用使用同类型，或使用 UUID (CHAR(36)) 在需要防枚举的场景。
- 金额类字段使用 DECIMAL(10,2)。
- 时间字段统一使用 `TIMESTAMP(3)` 或 `DATETIME(3)` UTC，应用层转换。
- 所有表包含审计列：`created_at`, `updated_at`；可选软删列：`deleted_at`（NULL 表示有效）。
- 频繁查询的条件添加组合索引，避免过多单列索引冗余。
- 保持范式与必要的反范式平衡（如订单冗余下单时商品名称快照）。

公共列约定：
```
created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
deleted_at DATETIME(3) NULL DEFAULT NULL
```

## 2. 用户与认证 (user-service)
### 2.1 users
| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | BIGINT UNSIGNED | PK | 用户ID |
| username | VARCHAR(64) | UNIQUE NOT NULL | 用户名 |
| email | VARCHAR(128) | UNIQUE NOT NULL | 邮箱 |
| mobile | VARCHAR(20) | UNIQUE NULL | 手机号，可选 |
| password_hash | VARCHAR(128) | NOT NULL | PBKDF2/bcrypt 哈希 |
| status | TINYINT | NOT NULL DEFAULT 1 | 1=active,0=disabled |
| last_login_at | DATETIME(3) NULL | 最近登录时间 |
| created_at | DATETIME(3) |  |  |
| updated_at | DATETIME(3) |  |  |
| deleted_at | DATETIME(3) |  |  |

索引：
- UNIQUE(username), UNIQUE(email), UNIQUE(mobile)
- INDEX(status)

### 2.2 user_address
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| user_id | BIGINT UNSIGNED | FK users(id) | 所属用户 |
| recipient | VARCHAR(64) | NOT NULL | 收件人 |
| phone | VARCHAR(20) | NOT NULL | 联系电话 |
| province | VARCHAR(64) | NOT NULL |
| city | VARCHAR(64) | NOT NULL |
| district | VARCHAR(64) | NOT NULL |
| detail | VARCHAR(255) | NOT NULL |
| is_default | TINYINT | NOT NULL DEFAULT 0 | 默认地址 |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |
| deleted_at | DATETIME(3) | |

索引：INDEX(user_id), INDEX(user_id,is_default)

## 3. 商品与类目 (product-service)
### 库存快照设计说明
`products.stock` 字段并非库存权威来源，而是一个"展示/筛选用的近实时快照"。权威库存数据在 `inventory_items` 中维护（包括 total/reserved/locked/available 及 version 乐观锁），下单/扣减/回滚等强一致逻辑只依赖 `inventory_items`。保留冗余快照的原因：
- 列表/搜索/推荐位高频读取需要快速判断是否有货，避免对每条记录做跨表 JOIN 或 RPC。
- 允许读写路径解耦（CQRS）：库存写入复杂、读侧宽表加速。
- 方便商品缓存与搜索索引携带库存信息，减少额外聚合。

同步策略：
1. 事件驱动：消费 `inventory.updated` 事件更新对应 `products.stock` 与 `updated_stock_at`。
2. 合并写：对爆品可在 Redis 中聚合增量，间隔 N 秒批量刷新。
3. 定期对账：任务扫描 `inventory_items.available` 与 `products.stock` 差异，超过阈值（如 >5）记录差异并修复。
4. 幂等保障：事件内带版本/变更序号，更新时校验不回滚到旧值。

风险与缓解：
| 风险 | 描述 | 缓解 |
|------|------|------|
| 快照滞后 | 列表显示库存不准确 | 下单时仍二次校验权威库存；监控滞后阈值 |
| 高频写放大 | 热点商品频繁更新 products | Redis 合并增量 + 批处理 |
| 一致性丢失 | 事件丢失导致永久偏差 | Outbox + 重试 + 定期对账修正 |

使用规范：业务禁止用 `products.stock` 做扣减决策，只用于 UI 展示、排序、过滤。
### 3.1 products
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| name | VARCHAR(128) | NOT NULL |
| spu | VARCHAR(64) | UNIQUE NULL | 标准产品单元，可选 |
| description | TEXT | NULL | 描述（长文本） |
| category_main_id | BIGINT UNSIGNED | FK product_category(id) NULL | 主类目 |
| brand | VARCHAR(64) | NULL | 品牌 |
| price | DECIMAL(10,2) | NOT NULL | 当前价格 |
| currency | CHAR(3) | NOT NULL DEFAULT 'CNY' |
| status | TINYINT | NOT NULL DEFAULT 1 | 1=active,0=inactive |
| stock | INT | NOT NULL DEFAULT 0 | 冗余库存快照（可与 inventory 分离） |
| updated_stock_at | DATETIME(3) | NULL | 最近库存更新时间 |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |
| deleted_at | DATETIME(3) | |

索引：INDEX(status), INDEX(category_main_id), INDEX(price), FULLTEXT(description) (如需搜索)

### 3.2 product_category
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| name | VARCHAR(64) | NOT NULL UNIQUE |
| parent_id | BIGINT UNSIGNED | FK self(id) NULL | 父类目 |
| level | TINYINT | NOT NULL | 层级缓存 |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |
| deleted_at | DATETIME(3) | |

索引：INDEX(parent_id), INDEX(level)

### 3.3 product_price_history
| id | BIGINT UNSIGNED | PK |
| product_id | BIGINT UNSIGNED | FK products(id) | 商品 |
| old_price | DECIMAL(10,2) | NOT NULL |
| new_price | DECIMAL(10,2) | NOT NULL |
| changed_at | DATETIME(3) | NOT NULL | 变更时间 |
| operator | BIGINT UNSIGNED | NULL | 操作人 |

索引：INDEX(product_id, changed_at DESC)

## 4. 库存 (inventory-service)
### 4.1 inventory_items
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| product_id | BIGINT UNSIGNED | UNIQUE FK products(id) | 单商品库存（如更细可拆 sku_id） |
| total | INT | NOT NULL | 总库存 |
| reserved | INT | NOT NULL DEFAULT 0 | 预留（待支付） |
| available | INT | NOT NULL | 可用 = total - reserved - locked |
| locked | INT | NOT NULL DEFAULT 0 | 冻结（争议/风控） |
| version | INT | NOT NULL DEFAULT 0 | 乐观锁版本 |
| updated_at | DATETIME(3) | |
| created_at | DATETIME(3) | |

索引：UNIQUE(product_id), INDEX(available)

### 4.2 inventory_log
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| product_id | BIGINT UNSIGNED | FK products(id) |
| order_id | BIGINT UNSIGNED | NULL | 关联订单 |
| change | INT | NOT NULL | 变化量（负扣减，正回滚） |
| type | TINYINT | NOT NULL | 1=reserve 2=deduct 3=rollback 4=lock 5=unlock |
| reason | VARCHAR(128) | NULL |
| trace_id | VARCHAR(64) | NULL | 链路追踪 |
| created_at | DATETIME(3) | |

索引：INDEX(product_id, created_at), INDEX(order_id)

## 5. 购物车 (cart-service)
### 5.1 cart_items
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| user_id | BIGINT UNSIGNED | FK users(id) | 用户 |
| product_id | BIGINT UNSIGNED | FK products(id) | 商品 |
| quantity | INT | NOT NULL | 数量 |
| added_at | DATETIME(3) | NOT NULL DEFAULT CURRENT_TIMESTAMP(3) |
| updated_at | DATETIME(3) | |
| created_at | DATETIME(3) | |

唯一约束：UNIQUE(user_id, product_id)
索引：INDEX(user_id), INDEX(product_id)

## 6. 订单 (order-service)
### 6.1 orders
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| user_id | BIGINT UNSIGNED | FK users(id) |
| status | TINYINT | NOT NULL | 1=created 2=paid 3=canceled 4=refunding 5=completed |
| total_amount | DECIMAL(10,2) | NOT NULL |
| currency | CHAR(3) | NOT NULL DEFAULT 'CNY' |
| pay_method | VARCHAR(32) | NULL | 支付方式（mock） |
| address_snapshot | JSON | NOT NULL | 下单时地址快照 |
| user_snapshot | JSON | NOT NULL | 下单时用户信息（冗余） |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |
| canceled_at | DATETIME(3) | NULL |
| paid_at | DATETIME(3) | NULL |
| trace_id | VARCHAR(64) | NULL |

索引：INDEX(user_id, created_at DESC), INDEX(status), INDEX(paid_at)

### 6.2 order_items
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| order_id | BIGINT UNSIGNED | FK orders(id) | 订单 |
| product_id | BIGINT UNSIGNED | FK products(id) | 商品 |
| product_name | VARCHAR(128) | NOT NULL | 名称快照 |
| unit_price | DECIMAL(10,2) | NOT NULL |
| quantity | INT | NOT NULL |
| total_price | DECIMAL(10,2) | NOT NULL | unit_price * quantity |
| created_at | DATETIME(3) | |

索引：INDEX(order_id), INDEX(product_id)

### 6.3 order_payment
| id | BIGINT UNSIGNED | PK |
| order_id | BIGINT UNSIGNED | UNIQUE FK orders(id) |
| pay_status | TINYINT | NOT NULL | 1=pending 2=success 3=failed |
| pay_channel | VARCHAR(32) | NULL |
| transaction_id | VARCHAR(64) | NULL | 第三方流水（模拟） |
| amount | DECIMAL(10,2) | NOT NULL |
| paid_at | DATETIME(3) | NULL |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |

索引：UNIQUE(order_id), INDEX(pay_status)

### 6.4 order_status_history
| id | BIGINT UNSIGNED | PK |
| order_id | BIGINT UNSIGNED | FK orders(id) |
| from_status | TINYINT | NOT NULL |
| to_status | TINYINT | NOT NULL |
| changed_at | DATETIME(3) | NOT NULL |
| operator | BIGINT UNSIGNED | NULL |
| reason | VARCHAR(128) | NULL |

索引：INDEX(order_id, changed_at)

## 7. 促销与秒杀 (promotion-service)
### 7.1 seckill_events
| 字段 | 类型 | 约束 | 说明 |
| id | BIGINT UNSIGNED | PK |
| product_id | BIGINT UNSIGNED | FK products(id) |
| start_time | DATETIME(3) | NOT NULL |
| end_time | DATETIME(3) | NOT NULL |
| initial_stock | INT | NOT NULL |
| status | TINYINT | NOT NULL | 1=scheduled 2=running 3=ended 4=canceled |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |

索引：INDEX(product_id), INDEX(status), INDEX(start_time, end_time)

### 7.2 seckill_inventory
| id | BIGINT UNSIGNED | PK |
| seckill_id | BIGINT UNSIGNED | UNIQUE FK seckill_events(id) |
| available | INT | NOT NULL |
| reserved | INT | NOT NULL DEFAULT 0 |
| version | INT | NOT NULL DEFAULT 0 |
| updated_at | DATETIME(3) | |
| created_at | DATETIME(3) | |

索引：UNIQUE(seckill_id), INDEX(available)

### 7.3 seckill_participants
| id | BIGINT UNSIGNED | PK |
| seckill_id | BIGINT UNSIGNED | FK seckill_events(id) |
| user_id | BIGINT UNSIGNED | FK users(id) |
| request_id | VARCHAR(64) | NOT NULL | 幂等/追踪 |
| status | TINYINT | NOT NULL | 1=queued 2=won 3=lost |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |

索引：UNIQUE(seckill_id, user_id), UNIQUE(request_id), INDEX(status)

### 7.4 seckill_result (如与 participants 合并则可省略)
| id | BIGINT UNSIGNED | PK |
| seckill_id | BIGINT UNSIGNED | FK seckill_events(id) |
| user_id | BIGINT UNSIGNED | FK users(id) |
| order_id | BIGINT UNSIGNED | NULL | 自动下单生成的订单 |
| success | TINYINT | NOT NULL | 1=success 0=failure |
| created_at | DATETIME(3) | |

索引：INDEX(seckill_id), INDEX(user_id), INDEX(order_id)

### 7.5 promotion_campaign
| id | BIGINT UNSIGNED | PK |
| name | VARCHAR(128) | NOT NULL |
| type | TINYINT | NOT NULL | 1=discount 2=coupon 3=full_reduction |
| start_time | DATETIME(3) | NOT NULL |
| end_time | DATETIME(3) | NOT NULL |
| status | TINYINT | NOT NULL | 1=scheduled 2=running 3=ended |
| meta | JSON | NULL | 灵活字段 |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |

索引：INDEX(type), INDEX(status), INDEX(start_time, end_time)

## 8. 用户画像与推荐元数据 (ai / shared)
### 8.1 user_profile_features
| id | BIGINT UNSIGNED | PK |
| user_id | BIGINT UNSIGNED | UNIQUE FK users(id) |
| feature_version | INT | NOT NULL DEFAULT 1 |
| structured_json | JSON | NULL | 标签、偏好等结构化特征 |
| updated_at | DATETIME(3) | |
| created_at | DATETIME(3) | |

索引：UNIQUE(user_id)

### 8.2 product_embedding_meta
| id | BIGINT UNSIGNED | PK |
| product_id | BIGINT UNSIGNED | UNIQUE FK products(id) |
| embedding_version | INT | NOT NULL |
| dimension | INT | NOT NULL |
| model | VARCHAR(64) | NOT NULL |
| updated_at | DATETIME(3) | |
| created_at | DATETIME(3) | |

索引：UNIQUE(product_id), INDEX(model, embedding_version)

## 9. 幂等与事件支持 (shared)
### 9.1 idempotency_keys
| id | BIGINT UNSIGNED | PK |
| key | VARCHAR(64) | UNIQUE NOT NULL |
| service | VARCHAR(32) | NOT NULL |
| request_hash | VARCHAR(128) | NOT NULL | 请求体 hash |
| response_snapshot | JSON | NULL | 返回缓存 |
| status | TINYINT | NOT NULL | 1=processing 2=completed 3=failed |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |

索引：UNIQUE(key), INDEX(service, status)

### 9.2 outbox_events
| id | BIGINT UNSIGNED | PK |
| aggregate_type | VARCHAR(32) | NOT NULL | 例如 order |
| aggregate_id | BIGINT UNSIGNED | NOT NULL |
| event_type | VARCHAR(64) | NOT NULL | order.created |
| payload | JSON | NOT NULL |
| status | TINYINT | NOT NULL DEFAULT 0 | 0=pending 1=published 2=error |
| attempt_count | INT | NOT NULL DEFAULT 0 |
| created_at | DATETIME(3) | |
| updated_at | DATETIME(3) | |

索引：INDEX(status, created_at), INDEX(aggregate_type, aggregate_id)

### 9.3 processed_events (可选，去重)
| id | BIGINT UNSIGNED | PK |
| event_id | VARCHAR(64) | UNIQUE NOT NULL | 来自消息 header 唯一 ID |
| consumer_service | VARCHAR(32) | NOT NULL |
| processed_at | DATETIME(3) | NOT NULL |

索引：UNIQUE(event_id), INDEX(consumer_service)

## 10. 命名与规范补充
- 表名：snake_case + 业务语义（复数或约定统一）。
- 不在同一表混合不同语义的状态码（拆分 history 表）。
- JSON 字段用于低频检索/灵活拓展，避免高频查询过滤 JSON 字段。
- 乐观锁：库存与秒杀库存表包含 version 字段，通过 `UPDATE ... WHERE version=?`。

## 11. 迁移与版本管理
- 使用 Flyway / Liquibase 或自定义迁移工具；迁移脚本命名：`V1__init_users.sql`。
- 每次结构变化需更新该文档与迁移脚本同步。

## 12. 与 Kafka 事件的映射
- 订单创建：`orders` + `order_items` 持久化成功 → 写入 `outbox_events` → 发布 `order.created`。
- 支付：更新 `order_payment` 与 `orders.status` → 写入 `order.paid` 事件。
- 秒杀资格：写入 `seckill_participants.status=won` → 发布 `seckill.succeeded`。
- 商品更新：`products` 变更或 `product_price_history` 插入 → 发布 `product.updated`。

## 12.1 库存快照与对账任务
为保证 `products.stock` 与权威库存的最终一致性，需要一个周期性对账任务（Reconciliation Job）：

执行频率：
- 正常：每 5 分钟扫描差异；高峰期可调为 1 分钟；低谷期放宽到 15 分钟。

步骤：
1. 批量查询差异：
   ```sql
   SELECT p.id AS product_id, p.stock AS snapshot_stock, i.available AS authoritative_stock
   FROM products p
   JOIN inventory_items i ON p.id = i.product_id
   WHERE ABS(p.stock - i.available) > 5
   LIMIT 1000;
   ```
2. 记录差异：写审计日志表（可选 `stock_reconcile_log`）。
3. 更新快照：`UPDATE products SET stock = ?, updated_stock_at = NOW(3) WHERE id = ?`。
4. 若差异频繁（同一商品重复出现）触发告警（Prometheus Counter + Alertmanager）。

伪代码：
```go
// 每隔 interval 运行
rows := QueryDiffProducts(threshold=5, limit=1000)
for _, r := range rows {
    LogDiscrepancy(r.ProductID, r.SnapshotStock, r.AuthStock)
    Exec("UPDATE products SET stock=?, updated_stock_at=NOW(3) WHERE id=?", r.AuthStock, r.ProductID)
}
// metrics: reconcile_count++, corrected_items++
```

可选扩展：
- 为减少全表扫描，可维护一张最近库存变动的候选集合（例如库存服务在变更时写入 `recent_inventory_changes`）。
- 偏差阈值动态调整：热门商品阈值小（2），普通商品阈值大（10）。
- 增加审计表结构：
  | 字段 | 类型 | 说明 |
  |------|------|------|
  | id | BIGINT UNSIGNED | PK |
  | product_id | BIGINT UNSIGNED | 商品ID |
  | snapshot_stock | INT | 修正前快照 |
  | authoritative_stock | INT | 权威库存 |
  | diff | INT | 差值 |
  | reconciled_at | DATETIME(3) | 修正时间 |
  | created_at | DATETIME(3) | 记录时间 |

监控指标建议：
- `stock_reconcile_discrepancies_total` 差异次数
- `stock_reconcile_corrected_total` 修复条目数
- `stock_snapshot_lag` 平均滞后（统计 p.stock 与 i.available 差值分布）

## 13. 索引与查询场景速览
| 表 | 典型查询 | 关键索引 |
|----|----------|----------|
| users | 登录/查用户 | UNIQUE(username), UNIQUE(email) |
| products | 列表/分类筛选 | INDEX(category_main_id), INDEX(status), INDEX(price) |
| inventory_items | 扣减库存 | UNIQUE(product_id) |
| orders | 用户订单分页 | INDEX(user_id, created_at) |
| order_items | 订单明细 | INDEX(order_id) |
| seckill_events | 活动状态筛选 | INDEX(status), INDEX(start_time,end_time) |
| seckill_participants | 查询用户参与结果 | UNIQUE(seckill_id,user_id) |
| outbox_events | 待投递事件扫描 | INDEX(status, created_at) |

## 14. 示例建表 SQL 片段（部分）
```sql
CREATE TABLE users (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  username VARCHAR(64) NOT NULL UNIQUE,
  email VARCHAR(128) NOT NULL UNIQUE,
  mobile VARCHAR(20) UNIQUE,
  password_hash VARCHAR(128) NOT NULL,
  status TINYINT NOT NULL DEFAULT 1,
  last_login_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE products (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  name VARCHAR(128) NOT NULL,
  spu VARCHAR(64) UNIQUE,
  description TEXT,
  category_main_id BIGINT UNSIGNED NULL,
  brand VARCHAR(64),
  price DECIMAL(10,2) NOT NULL,
  currency CHAR(3) NOT NULL DEFAULT 'CNY',
  status TINYINT NOT NULL DEFAULT 1,
  stock INT NOT NULL DEFAULT 0,
  updated_stock_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  INDEX idx_category_main(category_main_id),
  INDEX idx_status(status),
  INDEX idx_price(price)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

> 注：完整 SQL 可根据需要生成迁移文件。

## 15. 后续扩展建议
- 增加 coupon/优惠券相关表：`coupons`, `coupon_claims`。
- 增加用户行为埋点：`user_events`（可选转入 ClickHouse）。
- 引入多租户：在关键表增加 `tenant_id`。

---
文档版本：v1
更新时间：2025-10-29
