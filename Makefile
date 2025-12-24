.PHONY: all proto build server client clean help

# 默认目标
all: proto build

# 生成 protobuf 代码
proto:
	@echo "生成 protobuf 代码..."
	@bash generate.sh

# 构建所有程序
build: build-server build-server-v2 build-client

# 构建服务器
build-server:
	@echo "构建服务器..."
	@go build -o bin/server ./server

# 构建服务器 V2 (Channel架构)
build-server-v2:
	@echo "构建服务器 V2..."
	@go build -o bin/server-v2 ./server-v2

# 构建客户端
build-client:
	@echo "构建客户端..."
	@go build -o bin/client ./client

# 运行服务器
server:
	@echo "启动服务器..."
	@go run ./server/main.go

# 运行服务器 V2 (Channel架构)
server-v2:
	@echo "启动服务器 V2..."
	@go run ./server-v2/main.go

# 运行客户端
client:
	@echo "启动客户端..."
	@go run ./client/main.go

# 清理构建产物
clean:
	@echo "清理..."
	@rm -rf bin/
	@rm -f proto/*.pb.go

# 安装依赖
deps:
	@echo "安装依赖..."
	@go mod tidy

# 显示帮助信息
help:
	@echo "可用的命令:"
	@echo "  make proto          - 生成 protobuf 代码"
	@echo "  make build          - 构建所有程序"
	@echo "  make build-server   - 只构建服务器"
	@echo "  make build-server-v2 - 只构建服务器 V2 (Channel架构)"
	@echo "  make build-client   - 只构建客户端"
	@echo "  make server         - 运行服务器"
	@echo "  make server-v2      - 运行服务器 V2 (Channel架构)"
	@echo "  make client         - 运行客户端"
	@echo "  make deps           - 安装/更新依赖"
	@echo "  make clean          - 清理构建产物"
	@echo "  make help           - 显示此帮助信息"
