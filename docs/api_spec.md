# API 接口设计规范

## 一、统一规范

- 所有接口遵循 RESTful 风格。
- 请求与响应均采用 `application/json`。
- 认证使用 JWT Token。
- 微服务之间异步通信使用 Kafka 消息事件。

---

## 二、公共返回结构

```json
{
  "code": 0, // 状态码
  "message": "success", // 错误或提示信息
  "data": {} // 返回的数据
}
## 三、主要接口定义
1️⃣ 用户模块（user-service）
方法	路径	描述
POST	/api/v1/user/register	用户注册
POST	/api/v1/user/login	用户登录
GET	/api/v1/user/profile	获取用户信息
2️⃣ 商品模块（product-service）
方法	路径	描述
GET	/api/v1/products	获取商品列表
GET	/api/v1/product/:id	获取商品详情
POST	/api/v1/product	新增商品（商家）
PUT	/api/v1/product/:id	更新商品信息
3️⃣ 购物车模块（cart-service）
方法	路径	描述
GET	/api/v1/cart	获取购物车
POST	/api/v1/cart/add	添加商品
POST	/api/v1/cart/remove	移除商品
4️⃣ 订单模块（order-service）
方法	路径	描述
POST	/api/v1/order/create	创建订单
GET	/api/v1/order/:id	获取订单详情
POST	/api/v1/order/pay	模拟支付
5️⃣ 活动与秒杀模块（promotion-service）
方法	路径	描述
GET	/api/v1/seckill/list	获取活动列表
POST	/api/v1/seckill/:id/join	参与秒杀活动
6️⃣ 智能助手（ai-service, Python）
（1）客户智能助手
方法	路径	描述
POST	/api/v1/ai/customer/chat	客户问答接口
POST	/api/v1/ai/customer/recommend	智能推荐商品

请求示例：

{
  "user_id": 123,
  "query": "学生用笔记本电脑推荐"
}


响应示例：

{
  "code": 0,
  "data": {
    "recommendations": [
      {"product_id": 101, "name": "ThinkPad E14"},
      {"product_id": 202, "name": "MacBook Air M2"}
    ]
  }
}
（2）商家智能助手
方法	路径	描述
POST	/api/v1/ai/merchant/forecast	获取进货建议
四、错误码规范
Code	含义
0	成功
1001	参数错误
1002	认证失败
2001	库存不足
3001	秒杀结束
4001	AI 服务不可用