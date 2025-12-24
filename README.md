# gRPC 双向流游戏服务

这是一个使用 Golang 和 gRPC 实现的双向流式通信项目，包含完整的超时管理机制。项目实现了一个简单的多人游戏聊天系统，演示了 gRPC 双向流的实际应用。

## 功能特性

### 🚀 核心功能
- ✅ **gRPC 双向流式通信**：服务器和客户端可以同时发送和接收消息
- ✅ **完整的超时管理**：
  - 连接超时（10秒）
  - 流超时（5分钟）
  - 自动心跳检测（30秒间隔）
- ✅ **Keepalive 机制**：防止空闲连接被中断
- ✅ **多客户端支持**：支持多个客户端同时连接
- ✅ **消息广播**：服务器可以向所有连接的客户端广播消息
- ✅ **优雅断开**：客户端可以正常退出并通知其他用户

### 📋 消息类型
- **CHAT**：聊天消息
- **ACTION**：游戏动作
- **HEARTBEAT**：心跳包
- **DISCONNECT**：断开连接通知

## 项目结构

```
grpc-game/
├── proto/              # Protobuf 定义文件
│   ├── game.proto      # 服务和消息定义
│   ├── game.pb.go      # 生成的 Go 代码（消息）
│   └── game_grpc.pb.go # 生成的 Go 代码（服务）
├── server/             # 服务器实现
│   └── main.go         # 服务器主程序
├── client/             # 客户端实现
│   └── main.go         # 客户端主程序
├── generate.sh         # Protobuf 代码生成脚本
├── Makefile            # 构建脚本
├── go.mod              # Go 模块文件
└── README.md           # 项目文档
```

## 环境要求

- Go 1.16 或更高版本
- Protocol Buffers 编译器 (protoc)
- protoc-gen-go 和 protoc-gen-go-grpc 插件

## 安装依赖

### 1. 安装 Protocol Buffers 编译器

**Ubuntu/Debian:**
```bash
sudo apt update
sudo apt install -y protobuf-compiler
```

**macOS:**
```bash
brew install protobuf
```

**验证安装:**
```bash
protoc --version
```

### 2. 安装 Go 插件

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

确保 `$GOPATH/bin` 在你的 PATH 中：
```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 3. 安装项目依赖

```bash
go mod tidy
```

## 快速开始

### 方法 1: 使用 Makefile（推荐）

```bash
# 生成 protobuf 代码并构建
make all

# 启动服务器（在一个终端）
make server

# 启动客户端（在另一个终端）
make client
```

### 方法 2: 手动运行

```bash
# 1. 生成 protobuf 代码
bash generate.sh

# 2. 运行服务器
go run server/main.go

# 3. 在另一个终端运行客户端
go run client/main.go

# 4. 运行更多客户端（可选，使用不同名称）
go run client/main.go Alice
go run client/main.go Bob
```

## 使用说明

### 服务器

服务器启动后会监听在 `localhost:50051`，并显示以下信息：
```
服务器启动在端口 :50051
流超时时间: 5m0s
心跳间隔: 30s
```

服务器会自动：
- 接受客户端连接
- 广播消息给所有连接的客户端
- 监控连接超时
- 响应心跳请求
- 处理客户端断开连接

### 客户端

客户端启动时可以指定玩家名称：
```bash
go run client/main.go YourName
```

或者在启动后输入名称。

#### 可用命令：

- **发送聊天消息**：直接输入文本
  ```
  > 大家好！
  ```

- **执行游戏动作**：使用 `/action` 命令
  ```
  > /action 攻击敌人
  ```

- **退出游戏**：使用 `/quit` 命令
  ```
  > /quit
  ```

### 示例会话

**客户端 1 (Alice):**
```
> 大家好！
[15:30:45] Alice: 大家好！
[15:30:50] 系统: 玩家 Bob 加入了游戏
[15:30:52] Bob: 你好 Alice！
> /action 打招呼
[15:30:55] 🎮 Alice 执行了动作: 打招呼
```

**客户端 2 (Bob):**
```
[15:30:50] 系统: 欢迎 Bob 加入游戏！
[15:30:45] Alice: 大家好！
> 你好 Alice！
[15:30:52] Bob: 你好 Alice！
[15:30:55] 🎮 Alice 执行了动作: 打招呼
```

## 超时管理详解

### 1. 连接超时 (Connection Timeout)
- **时长**：10 秒
- **说明**：客户端连接到服务器的最大等待时间
- **触发条件**：网络问题或服务器不可达

### 2. 流超时 (Stream Timeout)
- **时长**：5 分钟
- **说明**：整个双向流会话的最大持续时间
- **触发条件**：超过指定时间未发送任何消息
- **注意**：心跳会重置此超时

### 3. 心跳机制 (Heartbeat)
- **间隔**：30 秒
- **说明**：客户端定期发送心跳包保持连接活跃
- **作用**：
  - 检测连接是否存活
  - 重置流超时计时器
  - 及时发现网络问题

### 4. Keepalive 参数

**服务器端：**
- `MaxConnectionIdle`: 15 秒 - 空闲连接最大时间
- `MaxConnectionAge`: 30 分钟 - 连接最大生存时间
- `Time`: 5 秒 - keepalive ping 间隔
- `Timeout`: 1 秒 - keepalive 超时

**客户端：**
- `Time`: 10 秒 - 发送 ping 的间隔
- `Timeout`: 3 秒 - 等待 pong 的超时时间
- `PermitWithoutStream`: true - 允许无流时发送 keepalive

## Makefile 命令

```bash
make help         # 显示所有可用命令
make proto        # 生成 protobuf 代码
make build        # 构建服务器和客户端
make build-server # 只构建服务器
make build-client # 只构建客户端
make server       # 运行服务器
make client       # 运行客户端
make deps         # 安装/更新依赖
make clean        # 清理构建产物
```

## 技术架构

### gRPC 双向流工作原理

1. **客户端连接**：客户端调用 `BidirectionalStream` 方法建立流
2. **并发通信**：
   - 服务器和客户端各有一个 goroutine 负责接收消息
   - 发送消息不阻塞接收
3. **消息路由**：服务器接收到消息后广播给所有连接的客户端
4. **连接管理**：使用 sync.Map 存储活跃连接，支持并发访问

### 关键实现点

**服务器端：**
- 使用 `sync.Map` 管理多个客户端连接
- 独立的 goroutine 监控每个客户端的超时状态
- 消息广播采用线程安全的遍历方式
- Context 用于优雅关闭和超时控制

**客户端端：**
- 三个主要 goroutine：
  1. 接收消息
  2. 发送心跳
  3. 处理用户输入
- 使用 channel 协调 goroutine 之间的通信
- Context 传递超时和取消信号

## 故障排查

### 问题：无法连接到服务器

**解决方法：**
1. 确认服务器正在运行
2. 检查端口 50051 是否被占用
3. 查看防火墙设置

### 问题：连接频繁断开

**解决方法：**
1. 调整超时参数（在代码中修改常量）
2. 检查网络稳定性
3. 查看服务器和客户端日志

### 问题：心跳失败

**解决方法：**
1. 确认网络连接正常
2. 检查 keepalive 参数配置
3. 查看是否有代理或防火墙干扰

## 扩展建议

1. **添加认证**：实现 JWT 或 TLS 认证
2. **消息持久化**：将消息保存到数据库
3. **房间系统**：支持多个游戏房间
4. **状态同步**：同步游戏状态和玩家位置
5. **断线重连**：实现自动重连机制
6. **监控面板**：添加 Web 界面查看在线用户

## 性能优化

- 使用消息池减少内存分配
- 实现消息批量发送
- 添加消息压缩
- 优化锁的使用范围
- 实现连接池

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 联系方式

如有问题，请提交 GitHub Issue。
