#!/bin/bash

# ===============================
# 一键初始化电商+AI微服务项目目录结构
# 作者: 张德华
# ===============================

PROJECT_NAME=${1:-ecommerce-ai-platform}

echo "🚀 正在创建项目目录结构: $PROJECT_NAME ..."

# 创建主目录
# mkdir -p $PROJECT_NAME
# cd $PROJECT_NAME || exit 1

# 顶层目录
mkdir -p {docs,gateway,common,deploy,test,services}

# 文档目录
mkdir -p docs
touch docs/{architecture.md,event_flow.md,api_spec.md,deployment.md}

# 通用模块
mkdir -p common/{pkg/{logger,mq,config,trace},proto}

# Gateway
mkdir -p gateway/{router,middleware}
touch gateway/{main.go,config.yaml}

# 服务目录
SERVICES=(user product cart order inventory promotion)
for svc in "${SERVICES[@]}"; do
  mkdir -p services/$svc/{cmd,internal/{handler,service,dao,model},mq,config}
  touch services/$svc/cmd/main.go
  touch services/$svc/config/config.yaml
done

# AI 服务
mkdir -p services/ai/{customer_assistant,merchant_ai,common}
touch services/ai/requirements.txt

# 客户智能助手
mkdir -p services/ai/customer_assistant
touch services/ai/customer_assistant/{main.py,consumer_order.py,recommender.py,retriever.py,llm_agent.py}

# 商家智能助手
mkdir -p services/ai/merchant_ai
touch services/ai/merchant_ai/{main.py,consumer_inventory.py,forecast_model.py,data_loader.py}

# AI 公共模块
mkdir -p services/ai/common
touch services/ai/common/{mq_adapter.py,config.py,utils.py}

# 部署目录
mkdir -p deploy/{docker,k8s,scripts}
touch deploy/docker/{Dockerfile.user,Dockerfile.order,Dockerfile.ai,Dockerfile.gateway}
touch deploy/k8s/{order-deploy.yaml,ai-deploy.yaml,kafka-deploy.yaml,ingress.yaml}
touch deploy/scripts/init_topics.sh

# 测试目录
mkdir -p test/{integration,unit,loadtest}

# 顶层文件
touch {README.md,Makefile,docker-compose.yml}

echo "✅ 项目目录创建完成！"
tree -L 3 .
