# 向量数据库（Milvus）设计方案

该文档定义系统中使用向量数据库（Milvus）的集合（Collection）设计、字段、索引参数、更新与再训练流程，以及与业务事件的集成方式。

## 1. 应用场景概览
- 商品语义检索：根据自然语言描述找到最匹配的商品（用于客户助手推荐召回）。
- FAQ / 知识库问答：客户咨询问题匹配已有知识条目（售后 / 使用指南）。
- 用户画像向量（可选）：根据用户行为生成兴趣向量用于协同过滤/向量近邻查找相似用户。
- 推荐反馈强化：用户点击/购买行为用于更新商品或用户向量权重。

## 2. 集合设计总览
| Collection | 说明 | 基础维度 | 度量 | 分区策略 | 索引类型 |
|------------|------|----------|------|----------|----------|
| product_embeddings | 商品内容/描述向量 | 768 (示例) | COSINE | 按 `category` 可选分区 | HNSW / IVF_FLAT |
| faq_embeddings | FAQ / 知识库条目向量 | 512 | COSINE | 按语言或模块 | HNSW |
| user_profile_embeddings (可选) | 用户兴趣画像向量 | 128/256 | COSINE | 按用户分桶 (hash%N) | HNSW |
| interaction_feedback (稀疏/辅助) | 行为反馈稀疏特征（非必然向量集合） | 变量 | N/A | 无 | 可存储在 MySQL/ClickHouse |

> 维度视所选模型：BERT/ERNIE/SimCSE/文本嵌入模型等。

## 3. 字段设计 (Milvus Schema)
每个向量集合采用如下基础字段：
- `id`：主键，BIGINT 或自增（由应用生成雪花 ID），Milvus 使用 INT64。
- `vector`：FLOAT_VECTOR，长度 = 维度。
- `source_id`：对应业务主键（如商品ID、FAQ条目ID、用户ID）。
- `version`：INT，用于区分嵌入版本（模型迭代）。
- `type`：VARCHAR，表明嵌入类型（title / description / combined 等）。
- `metadata`：JSON（在 Milvus 中可拆为多列，如 category、brand、lang、tags）。
- `updated_at`：INT 或 BIGINT（Unix Epoch 毫秒）。

示例（product_embeddings）字段：
| 字段 | 类型 | 说明 |
|------|------|------|
| id | INT64 | 唯一主键（embedding记录） |
| source_id | INT64 | 对应 products.id |
| version | INT | 嵌入版本 |
| vector | FLOAT_VECTOR[768] | 商品语义向量 |
| type | VARCHAR | e.g. 'title', 'desc', 'mixed' |
| category | VARCHAR | 类目名称/ID 映射 |
| brand | VARCHAR | 品牌 |
| lang | VARCHAR | 语言（'zh','en'） |
| updated_at | INT64 | 毫秒时间戳 |

> FAQ 集合可增加字段：`module`（售后/保修/安装），`priority`（命中结果排序辅助）。

## 4. 索引与参数建议
### 4.1 HNSW
- 适合高召回与低延迟场景。
- 参数：`M=48`, `efConstruction=200`, 查询时 `ef=128`（可调）。
### 4.2 IVF_FLAT
- 较适合批量构建与较大的数据量。
- 参数：`nlist=16384`（随数据规模调整），查询时 `nprobe` 在 8~64 间调优。
### 4.3 选择策略
- 初期数据量 < 100k：HNSW 即可。
- 商品规模预期 > 1M：考虑 IVF + PQ 或 DiskANN（后期）。

## 5. 数据处理流程
### 5.1 商品嵌入生成
1. 商品新增/更新（`product.updated` 事件）。
2. AI 嵌入 Worker 消费事件 → 拉取最新商品文本（标题、描述、类目）。
3. 生成向量（调用嵌入模型服务）。
4. Upsert 到 Milvus：
   - 若存在旧版本：`version++`；保留或删除旧记录依据保留策略。
5. 将 `product_embedding_meta` 表更新（记录模型版本、更新时间）。

### 5.2 FAQ 更新
1. FAQ 文档维护（可由运营后台）。
2. 批量嵌入构建脚本扫描 FAQ 表。
3. 构建向量并 Upsert 到 `faq_embeddings` 集合。
4. 建立或重建索引（定期 / 版本升级后）。

### 5.3 用户画像向量（可选阶段）
1. 定期（每日或小时）汇总行为：浏览、加入购物车、下单、点击推荐。
2. 行为特征聚合 → 生成用户兴趣向量。
3. Upsert 到 `user_profile_embeddings`（同一用户仅保留最新 version）。

## 6. 查询模式
### 6.1 商品语义检索
- 输入：自然语言查询（"办公轻薄笔记本"）。
- 步骤：
  1. 将查询文本做向量化（同模型）。
  2. 在 `product_embeddings` 上执行 Top-K 相似度搜索：`metric=COSINE`，`k=50`。
  3. 过滤条件：`category in (...) AND status='active'`（通过 Attribute filter）。
  4. 结果再打分融合（向量相似度 + 销量热度 + 库存充足度）。

### 6.2 FAQ 问答
- 同商品检索，但对候选结果增加置信度阈值（similarity ≥ 0.75），否则 fallback LLM。

### 6.3 用户相似推荐（后期）
- 给定用户向量 → 近邻用户 → 统计他们最近高频购买商品 → 排序推荐。

## 7. 事件集成
| 事件 | 影响 | 操作 |
|------|------|------|
| product.updated | 商品语义向量更新 | 触发嵌入再生成与 Upsert |
| order.paid | 用户行为画像更新 | 累积特征，可能触发用户画像向量重建 |
| recommendation.feedback | 调整用户偏好权重 | 标记交互，进入画像聚合任务 |
| seckill.succeeded | 增强热点度特征 | 影响商品热度评分（非直接影响原向量） |

## 8. 向量版本与回滚策略
- 每个集合保留最近 `N` 个版本（默认 2）：`current_version` 与 `previous_version`。
- 回滚场景：新模型效果回归 → 切换查询时指定 `version` 过滤。
- Milvus 查询：`filter="version = <current_version>"`。

## 9. 删除与过期
- 商品下架：`status` 过滤，不物理删除向量（保留历史分析）。
- FAQ 失效：标记 `active=false`（若使用属性列），查询过滤。
- 用户画像：长期未登录（>180天）可归档至冷存储或删除向量以节省资源。

## 10. 性能与容量规划（初版）
| 集合 | 预计规模 | 向量维度 | 存储估算 | 备注 |
|------|----------|----------|----------|------|
| product_embeddings | 1M | 768 | ~1M * 768 * 4B ≈ 3GB (不含索引) | 索引额外 ~30-60% |
| faq_embeddings | 50k | 512 | ~50k * 512 * 4B ≈ 100MB | 低频更新 |
| user_profile_embeddings | 5M | 256 | ~5M * 256 * 4B ≈ 5GB | 可分批更新 |

> 数据量增大需考虑分片与多节点部署。

## 11. 索引重建策略
- 每次嵌入模型大版本升级后进行全量重建。
- 步骤：
  1. 新模型批量生成所有向量，使用临时集合 `product_embeddings_tmp`。
  2. 构建索引并验证质量（召回率对比/在线 A/B）。
  3. 切换查询指向新集合或用版本字段过滤。
  4. 删除旧集合或降级为冷备。

## 12. 安全与访问控制
- 仅 AI 服务与检索网关拥有写权限（生成/更新向量）。
- 用户请求只能通过推荐/搜索接口间接访问结果，不直接暴露原始向量。
- 审计：记录向量重建任务的执行日志（存 MySQL `embedding_rebuild_log`）。

## 13. 监控与可观测性
- 指标：查询延迟P95、QPS、召回率（需要离线评估）、更新失败次数。
- 监控采集：Milvus 自带 metrics + 自定义埋点（Prometheus PushGateway）。

## 14. 风险与回避
| 风险 | 说明 | 对策 |
|------|------|------|
| 向量漂移 | 模型升级导致推荐不稳定 | 保留旧版本对比，逐步灰度 |
| 大批量更新阻塞 | 全量重建期间影响查询性能 | 使用临时集合，完成后原子切换 |
| 冷启动 | 新商品/新用户无向量 | Fallback 基于规则/热门榜 |

## 15. 示例伪代码（生成商品向量）
```python
# 消费 product.updated 事件
for event in product_updated_events:
    product = load_product(event.product_id)
    text = f"{product.name} {product.brand} {product.description}"[:4000]
    embedding = embedding_model.encode(text)  # returns list[float] length 768
    milvus_client.upsert(
        collection="product_embeddings",
        data={
            "id": generate_id(),
            "source_id": product.id,
            "version": CURRENT_EMBEDDING_VERSION,
            "vector": embedding,
            "type": "mixed",
            "category": product.category_main_id,
            "brand": product.brand,
            "lang": "zh",
            "updated_at": int(time.time() * 1000)
        }
    )
```

## 16. 后续扩展
- 多模态：加入图片向量集合（`product_image_embeddings`）。
- Rerank 阶段：用轻量 LLM 对 Top-K 商品进一步打分。
- 在线特征融合：向量相似度 + 协同过滤分数 + 规则分数加权。

---
文档版本：v1
更新时间：2025-10-29
