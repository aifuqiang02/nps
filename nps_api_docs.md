# NPS Web API 接口文档

## 认证授权

### `/login/register` [POST]
- 功能：用户注册
- 权限：无(需系统开启注册功能)
- 参数：
  - username: 用户名(必填)
  - password: 密码(必填)
  - email: 邮箱(可选)
- 返回：
  - 成功：
    ```json
    {
      "code": 200,
      "msg": "注册成功",
      "data": {
        "id": 用户ID,
        "username": "用户名"
      }
    }
    ```
  - 失败：
    ```json
    {
      "code": 400,
      "msg": "用户名已存在"
    }
    ```

### `/login/index` [POST]
- 功能：用户登录
- 权限：无
- 参数：
  - username: 用户名(必填)
  - password: 密码(必填)
  - captcha: 验证码(当系统开启验证码时必填)
- 返回：
  - 成功：
    ```json
    {
      "code": 200,
      "msg": "登录成功",
      "data": {
        "token": "JWT令牌",
        "user": {
          "id": 用户ID,
          "username": "用户名",
          "role": "角色"
        }
      }
    }
    ```
  - 失败：
    ```json
    {
      "code": 400,
      "msg": "用户名或密码错误"
    }
    ```

### `/login/logout` [GET]
- 功能：退出登录  
- 权限：需要认证
- 返回：操作结果

## 客户端管理

### `/client/list` [GET]
- 功能：获取客户端列表
- 权限：管理员
- 参数：
  - limit: 每页数量(默认10)
  - offset: 偏移量(默认0)
  - search: 搜索关键词(可选)
- 返回：
  ```json
  {
    "code": 200,
    "data": {
      "total": 总数量,
      "list": [
        {
          "id": 客户端ID,
          "remark": "备注",
          "status": 状态,
          "flow": {
            "inlet": 流入流量,
            "export": 流出流量
          },
          "rate": 当前速率
        }
      ]
    }
  }
  ```
  
### `/client/add` [POST]
- 功能：添加客户端
- 权限：管理员
- 参数：
  - remark: 备注(必填)
  - vkey: 访问密钥(可选，不填则自动生成)
  - flow_limit: 流量限制(MB,可选)
  - rate_limit: 速率限制(KB/S,可选)
  - max_conn: 最大连接数(可选)
  - web_username: Web用户名(可选)
  - web_password: Web密码(可选)
- 返回：
  ```json
  {
    "code": 200,
    "msg": "添加成功",
    "data": {
      "id": 客户端ID,
      "vkey": "访问密钥"
    }
  }
  ```
  
## 隧道管理

### `/index/tunnel` [GET]
- 功能：获取隧道列表  
- 权限：需要认证
- 参数：
  - client_id: 客户端ID(必填)
  - type: 隧道类型(tcp/udp/http,可选)
- 返回：
  ```json
  {
    "code": 200,
    "data": [
      {
        "id": 隧道ID,
        "type": "隧道类型",
        "server_port": 服务端口,
        "target": "目标地址",
        "status": 状态,
        "remark": "备注"
      }
    ]
  }
  ```
  
### `/index/addtunnel` [POST]
- 功能：添加隧道
- 权限：管理员
- 参数：
  - type: 类型(tcp/udp/http,必填)
  - server_port: 服务端口(必填)
  - client_id: 客户端ID(必填)
  - target: 目标地址(必填)
  - remark: 备注(可选)
  - password: 认证密码(可选)
- 返回：
  ```json
  {
    "code": 200,
    "msg": "添加成功",
    "data": {
      "id": 隧道ID,
      "port": 服务端口
    }
  }
  ```
  
## 主机管理

### `/index/host` [GET]
- 功能：获取主机列表
- 权限：需要认证
- 参数：
  - client_id: 客户端ID(必填)
  - scheme: 协议类型(http/https,可选)
- 返回：
  ```json
  {
    "code": 200,
    "data": [
      {
        "id": 主机ID,
        "host": "域名",
        "scheme": "协议",
        "location": "路径",
        "target": "目标地址",
        "status": 状态
      }
    ]
  }
  ```
  
### `/index/editHost` [POST]
- 功能：编辑主机
- 权限：管理员
- 参数：
  - id: 主机ID
  - host: 域名

## 系统配置

### `/global/config` [GET]  
- 功能：获取系统配置
- 权限：管理员
- 返回：
  ```json
  {
    "code": 200,
    "data": {
      "web_port": Web管理端口,
      "web_username": "管理员账号",
      "allow_register": 是否允许注册,
      "flow_limit": 流量限制,
      "black_ip_list": ["IP黑名单"]
    }
  }
  ```

### `/global/update` [POST]
- 功能：更新系统配置  
- 权限：管理员
- 参数：
  - key: 配置项(必填)
  - value: 配置值(必填)
- 返回：
  ```json
  {
    "code": 200,
    "msg": "更新成功"
  }
  ```

## 认证方式

1. **Session认证**  
   - 标准web会话认证
   
2. **Token认证**  
   ```
   Authorization: Bearer [token]
   ```

## 错误码

| 代码 | 描述 |
|------|------|
| 401 | 未授权 |
| 403 | 禁止访问 |
| 500 | 服务器错误 |

> 注意：所有时间参数使用Unix时间戳
