# 监控与可观测性设计方案

本文档定义电商 + AI 微服务系统的统一可观测性与监控方案，覆盖 Metrics（指标）、Tracing（调用链）、Logging（日志）、Dashboards（仪表盘）、Alerting（告警）以及采集/聚合/传输架构，目标：
- 全系统流量与吞吐可视化（接口 QPS、响应耗时、错误率）。
- 事件流与 Kafka Topic 消费/积压监控。
- 关键业务指标（下单成功率、库存扣减失败率、秒杀资格成功率、推荐响应耗时）。
- 快速定位性能瓶颈（Gateway、单服务、DB、外部依赖）。
- 支撑压测阶段的容量评估与调参

---
## 0. 全链路可追溯平台目标与范围
- 实时洞察：分钟级刷新各模块的请求量、成功率、异常率与资源使用，支持压测观察与生产巡检。
- 请求溯源：任一请求可还原跨 Gateway → 微服务 → Kafka → DB → AI 推理的完整执行链路及耗时分布。
- 事件追踪：支持以 `trace_id/request_id` 为主键联动查询日志、指标、追踪，解决跨系统排障困难。
- 版本对比：在灰度发布与 A/B 测试期间，按版本维度比较性能与错误率，降低回滚时间。
- 运维闭环：告警 → 仪表盘 → Trace → 日志 → 根因定位一体化，缩短 MTTR。

### 0.1 功能模块划分
| 模块 | 职责 | 关键组件 |
|------|------|----------|
| 数据埋点层 | 统一接入 HTTP/gRPC、Kafka、DB、AI 模块指标与 Trace | OpenTelemetry SDK, Prometheus 客户端, 统一日志库 |
| 采集传输层 | 聚合流量、统一协议转换、限流/缓存 | OpenTelemetry Collector, Promtail/Filebeat, Pushgateway（压测场景） |
| 存储与分析层 | 指标时序库、日志索引、Trace 存储与检索 | Prometheus, Loki/ELK, Jaeger/Tempo, ClickHouse（业务事件） |
| 体验层 | 实时仪表盘、请求拓扑、溯源检索、告警联动 | Grafana, 自研链路拓扑服务, Alertmanager |
| 治理与运营 | 标签规范、采样策略、容量规划 | 可观测性运营平台（配置中心 + Dashboard 模板管理） |

```
┌─────────────────────────────────────────────────────────────────────┐
│                           体验层 (Grafana)                       │
│   统一大盘  Trace Map  Log Explorer  告警看板  压测监控                  │
└───────────────▲───────────────────────────────────────────────┘
                │ Query/API
┌───────────────┴───────────────────────────────────────────────┐
│                   存储与分析层                                 │
│  Prometheus  ←→  Mimir(可选) | Jaeger/Tempo | Loki | ClickHouse │
└───────────────▲───────────────▲───────────────────────────────┘
                │ OTLP          │ Log/ETL
┌───────────────┴───────────────┴───────────────────────────────┐
│                   采集传输层                                   │
│  OTEL Collector  | Promtail | Kafka Exporter | DB Exporter      │
└───────────────▲───────────────────────────────────────────────┘
                │ SDK/Sidecar
┌───────────────┴───────────────────────────────────────────────┐
│                     数据埋点层                                │
│  Gateway | 微服务 | AI 服务 | Kafka 消费者 | DB/缓存 | 前端埋点  │
└────────────────────────────────────────────────────────────────┘
```

### 0.2 场景优先级
1. **压测全链路可视化**：压测任务自动打标，开启 100% Trace 采样，实时看吞吐/延迟/错误，并生成回放报表。
2. **生产异常定位**：高延迟或高错误率时，通过告警联动 Trace 入口一键跳转，查看瓶颈节点与异常日志。
3. **业务指标运营**：订单、库存、推荐等关键指标统一可视化，支持按渠道、地区、版本聚合。

### 0.3 非目标
- 不覆盖数据脱敏以外的安全审计流程（由安全组负责）。
- 不取代 APM 工具的深度代码级分析（可与 Pyroscope/pprof 集成）。

### 0.4 能力里程碑
| 里程碑 | 时间 | 核心交付 |
|--------|------|----------|
| M0 基础铺设 | 第 1 月 | Prometheus + Grafana 全面覆盖 /metrics，统一日志格式落地 |
| M1 链路贯通 | 第 2 月 | 关键链路（下单、秒杀、AI 推荐）实现端到端 Trace，链路拓扑展示 |
| M2 实时流量大盘 | 第 3 月 | 实现模块级实时流量视图、Kafka 消费热力图、压测模板大盘 |
| M3 告警闭环 | 第 4 月 | 告警联动 Trace/Log，一键定位；集成企业微信机器人 |
| M4 治理体系 | 第 5 月 | 标签规范、采样策略自动化调优，版本对比与 SLO 报表 |

### 0.5 全链路数据建模原则
- **统一标识**：全链路使用 `trace_id`（OTel 标准）与 `request_id`（对外使用）互相映射；Kafka 消息、日志、指标均写入。
- **标签治理**：限定高基数字段，路径统一模板化（`/api/v1/orders/:id`）；对业务维度使用自定义 `tenant/channel/activity_id` 标签。
- **时间同步**：使用 NTP 做节点时间校准；对跨地域部署引入时间漂移监控。
- **压测打标**：通过 Header `x-load-test=true` 注入 `load_test=true` 标签，避免干扰生产指标。

---
## 1. 技术栈组件
| 领域 | 组件 | 说明 |
|------|------|------|
| Metrics & TSDB | Prometheus + Alertmanager | 拉取各服务暴露的 /metrics；规则告警 |
| Tracing | OpenTelemetry SDK + Jaeger (或 Tempo) | 分布式调用链收集、上下游耗时分析 |
| Logging | 结构化日志 (Zap / Logrus / Python logging) + Loki/ELK | 统一 JSON，支持按 trace_id 查询 |
| Visualization | Grafana | 指标/日志/追踪整合视图 |
| Profiling (可选) | Pyroscope / pprof | Go & Python 性能剖析 |
| Kafka 监控 | Exporter (kafka_exporter) | 分区滞后、消费延迟、消息堆积 |
| DB 监控 | mysqld_exporter、redis_exporter | 连接数、慢查询、缓存命中率 |
| 系统资源 | node_exporter | CPU/Mem/Disk/I/O |

---
## 2. 采集架构总览
```
+------------------+      +------------------+
|  API Gateway     | ---> |  gRPC Services   |
|  /metrics        |      |  /metrics        |
+---------+--------+      +--------+---------+
          |                         |
          v                         v
     Prometheus <----- Exporters (Kafka/MySQL/Redis/Node)
          |
          +--> Alertmanager (规则告警)
          |
          +--> Grafana (仪表盘)

Tracing:
Requests -> OpenTelemetry SDK -> OTLP Export -> Collector -> Jaeger

Logging:
Services JSON Logs -> Loki (Promtail) / ELK -> Grafana Explore
```

---
## 3. 指标分类
### 3.1 系统通用指标 (所有服务暴露)
| 指标名 | 类型 | 说明 |
|--------|------|------|
| http_requests_total{service,method,code,path} | Counter | HTTP/gRPC 请求数 |
| http_request_duration_seconds_bucket | Histogram | 请求耗时分布（p50/p95/p99） |
| http_active_requests | Gauge | 当前活动请求（并发） |
| grpc_client_requests_total | Counter | gRPC 调用次数 |
| grpc_client_duration_seconds | Histogram | gRPC 调用耗时 |
| process_cpu_seconds_total | Counter | 进程 CPU 时间 |
| process_resident_memory_bytes | Gauge | 内存占用 |
| go_goroutines / python_threads | Gauge | 运行时线程/协程数 |
| build_info{version,commit} | Gauge(=1) | 部署版本标识 |

### 3.2 Gateway 特有
| 指标 | 说明 |
|------|------|
| gateway_rate_limit_block_total | 被限流次数 |
| gateway_auth_mock_total | 使用 Mock 鉴权次数 |
| gateway_upstream_fail_total | 后端调用失败计数 |
| gateway_request_bytes / response_bytes | 流量大小统计 |

### 3.3 业务领域指标
- 订单：
  - order_create_total{status}（status=success|fail）
  - order_paid_total
  - order_cancel_total
  - order_flow_duration_seconds（创建→支付耗时）
- 库存：
  - inventory_deduct_total{result}（result=success|fail）
  - inventory_rollback_total
  - inventory_version_conflict_total（乐观锁冲突）
- 秒杀：
  - seckill_attempt_total
  - seckill_success_total
  - seckill_queue_lag (Gauge) - 等待队列长度/延迟
- 推荐/AI：
  - ai_chat_request_total
  - ai_chat_latency_seconds (Histogram)
  - ai_recommend_request_total
  - ai_recommend_fallback_total（因向量/模型失败）
- 向量重建：
  - embedding_regen_total{model}
  - embedding_regen_duration_seconds

### 3.4 Kafka 指标 (kafka_exporter)
| 指标 | 说明 |
|------|------|
| kafka_consumergroup_lag | 每消费者组分区滞后 |
| kafka_topic_partition_current_offset | 当前分区最新 offset |
| kafka_consumergroup_current_offset | 消费者最新 offset |

### 3.5 数据库/缓存
| 指标 | 说明 |
|------|------|
| mysql_global_status_queries_total | 查询次数 |
| mysql_global_status_slow_queries | 慢查询计数 |
| redis_commands_processed_total | Redis 命令数 |
| redis_keyspace_hits / misses | 命中率 |

---
## 4. 调用链追踪 (Tracing)
### 4.1 采样策略
- 压测阶段：全量采样或 tail-based 保留慢请求与错误请求。
- 生产：基线 5~10% 采样 + 慢请求 (p95 > 阈值) 强制保留；错误全量。

### 4.2 Span 结构标准
| Span 名 | 层级 | 说明 |
|---------|------|------|
| HTTP / gRPC entry | Root | 网关入口请求 |
| service.handler | 子 | 每个业务服务处理函数 |
| db.query | 子 | SQL 查询（包含表名、耗时、影响行数） |
| cache.get / cache.set | 子 | Redis 操作 |
| mq.produce / mq.consume | 子 | Kafka 生产/消费 |
| ai.embedding | 子 | 向量生成调用 |
| external.http | 子 | 外部依赖请求 |

### 4.3 必需的 Trace 属性
- trace_id, span_id
- user_id（如果有）
- request_id（与响应一致）
- service, version
- path / method
- error=true 与 error.message

### 4.4 传播
- HTTP Header: `traceparent`, `tracestate`
- Kafka Message Header: `trace_id`, `span_id`, `parent_span_id`

### 4.5 链路拓扑与实时流量视图
- **拓扑构建**：Collector 将 Trace 数据推送到自研 `trace-graph-builder` 服务，按 `service`、`operation` 聚合生成调用图；对 span 标签 `load_test=true` 单独出图。
- **实时流量面板**：每 15 秒统计各服务入口 Span 的 `requests_per_minute` 与 `error_rate`，写入 Prometheus Pushgateway（或 VictoriaMetrics Stream）供 Grafana 热更新。
- **瓶颈识别**：通过 span duration 计算出 Top N 慢节点，写入 `trace_hotspot` 指标，以雷达图展示。
- **回放能力**：支持根据 `trace_id/request_id` 复原请求序列，包括 Kafka 消费与 AI 推理步骤，界面提供时间轴与关键字段（用户、订单、活动 ID 等）。
- **SLO 对齐**：链路拓扑面板与服务 SLO（可用性/延迟）挂钩，超过阈值时自动将 Trace 样本转存至长期存储以便复盘。

---
## 5. 日志规范
- 结构化 JSON：`{"ts":"...","level":"info","service":"order","trace_id":"...","msg":"Created order","order_id":123}`
- 最小字段集：`ts, level, service, version, trace_id, span_id (可选), msg`。
- 错误日志增加：`error.type`, `error.stack`。
- 禁止在高频日志中打印大 payload；必要时使用哈希。
- 日志等级：DEBUG（仅测试环境） / INFO / WARN / ERROR。

日志采集：Promtail 读取 stdout → Loki；或 Filebeat→ELK。

Retention：
- INFO：7 天
- ERROR/WARN：30 天
- 审计/安全：90 天（单独存储）

---
## 6. 仪表盘设计 (Grafana Panels)
### 6.1 总览 Dashboard (Overview)
模块：
- 全局请求 QPS（按服务堆叠）
- 全局请求 p95/p99 延迟热力图
- 错误率（4xx vs 5xx）
- Kafka 消费滞后趋势
- Top 慢接口列表（基于 duration histogram 合成）
- 订单成功率 / 每分钟下单数 / 支付成功率
- 库存扣减失败率 / 乐观锁冲突次数
- 秒杀实时尝试数 / 成功数 / 队列长度
- 推荐接口平均延迟与失败回退次数

### 6.2 服务详情 Dashboard
- 选择 service=order：
  - QPS / 延迟分布（柱状 + quantile）
  - 错误码分布（pie）
  - DB 查询耗时 Top5
  - 外部依赖调用情况（Redis、Kafka produce、Kafka consume）
  - GC 次数与暂停（Go）/ Python GC 时间

### 6.3 Kafka Dashboard
- 每 Topic 总消息速率
- 每消费者组滞后 (lag) Top N
- 堆积告警标记（阈值线）

### 6.4 秒杀专项
- 秒杀活动 ID 变量选择
- 实时尝试次数（attempt_total 增速）
- 成功资格数
- 平均排队耗时（使用 span 或自定义 Gauge）
- 库存预留/剩余趋势

### 6.5 AI 服务专用
- Chat 请求 QPS / Latency p95/p99
- 推荐召回数量分布
- 向量检索耗时 vs rerank 耗时
- 嵌入生成队列长度（如引入异步队列）

---
## 7. 告警规则 (Alertmanager)
| 规则 | 条件 | 持续 | 严重级别 | 说明 |
|------|------|------|----------|------|
| 高错误率 | http_requests_total{code=~"5.."} / total > 5% | 5m | critical | 服务异常或依赖故障 |
| 高延迟 | p99 延迟 > 2s | 10m | warning | 性能退化 |
| Kafka 滞后 | consumergroup_lag > 10000 | 5m | critical | 消费堆积 |
| 库存扣减失败 | inventory_deduct_total{result="fail"} / 成功 > 2% | 10m | warning | 可能并发/锁冲突 |
| 秒杀队列阻塞 | seckill_queue_lag > 阈值 | 2m | critical | 处理程序跟不上 |
| 推荐回退过高 | ai_recommend_fallback_total / ai_recommend_request_total > 15% | 15m | warning | 模型或向量检索问题 |
| DB 慢查询激增 | mysql_global_status_slow_queries > 基线 *2 | 10m | warning | 索引或流量异常 |
| 内存泄漏迹象 | process_resident_memory_bytes 增长速率异常 | 30m | info | 观察阶段 |
| 节点资源耗尽 | node_exporter CPU>85% 或 Mem>90% | 5m | critical | 需扩容或查热点 |

通知渠道：企业微信 / 钉钉 / Email / PagerDuty（分级）。

---
## 8. 压测支持
压测场景需临时开启：
- 全量 Trace 采样 + 标记压测请求 (Header: `X-Load-Test: true`).
- 专用 Dashboard 展示：吞吐曲线、延迟分位、错误率、资源利用率 (CPU/Mem)、GC 次数。
- 自动生成报告：对比目标 SLA（如 p99 < 800ms，错误率 < 1%）。

---
## 9. 指标埋点规范 (代码层)
Go 中间件示例：
```go
// HTTP metrics middleware pseudo
start := time.Now()
next(w, r)
status := rw.Status()
labels := prometheus.Labels{"service":"order","method":r.Method,"path":normalizePath(r.URL.Path),"code":strconv.Itoa(status)}
httpRequestsTotal.With(labels).Inc()
httpRequestDuration.Observe(time.Since(start).Seconds())
```

Kafka 消费埋点：
```go
msgStart := time.Now()
processMessage(m)
consumeDuration.Observe(time.Since(msgStart).Seconds())
consumerLag.Set(calcLag(m.Topic, m.Partition, m.Offset))
```

---
## 10. 数据质量与去重
- 指标命名统一：`<domain>_<action>_<metric>` 或使用 Prometheus 约定。
- 避免 label 高基数（path 正规化：`/api/v1/products/{id}` → `/api/v1/products/:id`）。
- user_id 不直接作为标签；使用聚合维度（region, channel）。

---
## 11. 安全与隔离
- 区分内部指标与外部暴露：外部仅展示健康与少量聚合统计。
- Trace 中避免敏感数据（隐藏用户密码、token）。
- 日志脱敏：邮箱/手机号局部打码。

---
## 12. 灰度与版本可视化
- 每个请求附带 `service.version` 标签，Dashboard 展示各版本错误率对比。
- 灰度阶段：设置告警规则单独针对新版本（例如版本标签 regex）。

---
## 13. 演进路线
| 阶段 | 能力 | 说明 |
|------|------|------|
| M1 | 基础 Metrics + 简单 Trace | 快速发现明显性能瓶颈 |
| M2 | 全链路 Trace + Kafka 指标 | 深入分析事件驱动耗时 |
| M3 | 业务 KPI 仪表盘 | 与产品/运营联动 |
| M4 | 自适应采样 + Profiling | 节约资源并针对热点优化 |
| M5 | AIOps 预测告警 | 利用历史数据做异常检测 |

---
## 14. 与架构文档的集成建议
- 在 `architecture.md` 中引用本文件，明确可观测性标准化接口：所有服务必须实现：`/healthz`, `/metrics`。gRPC 实现健康检查协议（`grpc.health.v1.Health`）。
- 将 Trace 头透传规范（HTTP/Kafka）列出，避免遗失。

---
文档版本：v1
更新时间：2025-11-1
