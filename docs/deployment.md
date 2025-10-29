
---

## ⚙️ **3️⃣ docs/deployment.md**

```markdown
# 部署与运行说明

## 一、部署模式

项目支持以下三种部署方式：

1. **本地开发模式**
   - 使用 `docker-compose` 一键启动所有服务。
   - 适合单机开发与调试。

2. **生产部署模式**
   - 使用 `Kubernetes` + `Helm` 进行容器编排。
   - 所有服务通过 Kafka 进行通信。

3. **混合部署**
   - 电商服务（Go）与 AI 服务（Python）分别部署在不同节点。
   - 使用公共 Kafka 总线与数据库集群。

---

## 二、基础依赖

| 组件 | 说明 |
|------|------|
| Kafka / RabbitMQ | 消息队列 |
| MySQL / PostgreSQL | 持久化数据库 |
| Redis | 缓存与分布式锁 |
| Milvus / FAISS | 向量数据库（AI检索） |
| Nginx / API Gateway | 请求入口 |
| Docker / K8s | 部署与编排 |

---

## 三、使用 Docker Compose 启动

```bash
# 构建镜像
make build

# 启动全部服务
docker-compose up -d

# 查看运行状态
docker ps
四、Kubernets 部署

kubectl apply -f deploy/k8s/kafka-deploy.yaml
kubectl apply -f deploy/k8s/order-deploy.yaml
kubectl apply -f deploy/k8s/ai-deploy.yaml
kubectl apply -f deploy/k8s/ingress.yaml


监控与日志：

Prometheus + Grafana：指标监控

Jaeger：分布式调用链追踪

Loki：日志聚合


五、配置文件说明

每个服务均有 config.yaml：

server:
  port: 8081

database:
  dsn: "root:123456@tcp(mysql:3306)/ecommerce"

mq:
  brokers:
    - "kafka:9092"
  topics:
    order_created: "order_events"