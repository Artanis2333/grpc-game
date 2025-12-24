package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	pb "github.com/Artanis2333/grpc-game/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	serverAddr        = "localhost:50051"
	connectionTimeout = 10 * time.Second // 连接超时时间
	streamTimeout     = 5 * time.Minute  // 流超时时间
	heartbeatInterval = 30 * time.Second // 心跳间隔
)

// GameClient 游戏客户端
type GameClient struct {
	client   pb.GameServiceClient
	playerID string
	stream   pb.GameService_BidirectionalStreamClient
	done     chan bool
}

// NewGameClient 创建新的游戏客户端
func NewGameClient(playerID string) (*GameClient, error) {
	// 配置 keepalive 参数
	kacp := keepalive.ClientParameters{
		Time:                10 * time.Second, // 发送 keepalive ping 的间隔
		Timeout:             3 * time.Second,  // 等待 keepalive ping 响应的超时时间
		PermitWithoutStream: true,             // 允许无流时发送 keepalive
	}

	// 创建带超时的连接 context
	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()

	// 连接到服务器
	conn, err := grpc.DialContext(
		ctx,
		serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(kacp),
		grpc.WithBlock(), // 阻塞直到连接建立
	)
	if err != nil {
		return nil, fmt.Errorf("连接服务器失败: %v", err)
	}

	log.Printf("成功连接到服务器 %s", serverAddr)

	client := pb.NewGameServiceClient(conn)

	return &GameClient{
		client:   client,
		playerID: playerID,
		done:     make(chan bool),
	}, nil
}

// Start 启动客户端
func (c *GameClient) Start() error {
	// 创建带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	// 建立双向流
	stream, err := c.client.BidirectionalStream(ctx)
	if err != nil {
		return fmt.Errorf("创建流失败: %v", err)
	}
	c.stream = stream

	// 发送初始连接请求
	if err := c.sendRequest(pb.RequestType_REQUEST_CHAT, "我已加入游戏"); err != nil {
		return fmt.Errorf("发送初始请求失败: %v", err)
	}

	// 启动接收消息的协程
	go c.receiveMessages()

	// 启动心跳协程
	go c.heartbeat(ctx)

	// 启动用户输入处理
	c.handleUserInput()

	return nil
}

// receiveMessages 接收来自服务器的消息
func (c *GameClient) receiveMessages() {
	for {
		resp, err := c.stream.Recv()
		if err == io.EOF {
			log.Println("服务器关闭了连接")
			c.done <- true
			return
		}
		if err != nil {
			log.Printf("接收消息错误: %v", err)
			c.done <- true
			return
		}

		// 显示消息（除了心跳响应）
		if resp.Type != pb.ResponseType_RESPONSE_HEARTBEAT {
			timestamp := time.Unix(resp.Timestamp, 0).Format("15:04:05")
			
			switch resp.Type {
			case pb.ResponseType_RESPONSE_CHAT:
				fmt.Printf("[%s] %s: %s\n", timestamp, resp.FromPlayerId, resp.Content)
				
			case pb.ResponseType_RESPONSE_ACTION:
				fmt.Printf("[%s] 🎮 %s 执行了动作: %s\n", timestamp, resp.FromPlayerId, resp.Content)
				
			case pb.ResponseType_RESPONSE_SYSTEM:
				if resp.SystemMessage != "" {
					fmt.Printf("[%s] 📢 系统: %s (%s)\n", timestamp, resp.Content, resp.SystemMessage)
				} else {
					fmt.Printf("[%s] 📢 系统: %s\n", timestamp, resp.Content)
				}
				
			case pb.ResponseType_RESPONSE_ERROR:
				fmt.Printf("[%s] ❌ 错误: %s\n", timestamp, resp.Content)
				if resp.SystemMessage != "" {
					fmt.Printf("    详情: %s\n", resp.SystemMessage)
				}
				
			default:
				fmt.Printf("[%s] %s: %s\n", timestamp, resp.FromPlayerId, resp.Content)
			}
		}
	}
}

// heartbeat 定期发送心跳
func (c *GameClient) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("心跳协程退出: context 已取消")
			return
		case <-c.done:
			log.Println("心跳协程退出: 连接已关闭")
			return
		case <-ticker.C:
			if err := c.sendRequest(pb.RequestType_REQUEST_HEARTBEAT, "ping"); err != nil {
				log.Printf("发送心跳失败: %v", err)
				c.done <- true
				return
			}
			log.Println("💓 发送心跳")
		}
	}
}

// handleUserInput 处理用户输入
func (c *GameClient) handleUserInput() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\n=== 游戏客户端已启动 ===")
	fmt.Println("命令:")
	fmt.Println("  /action <动作> - 执行游戏动作")
	fmt.Println("  /quit - 退出游戏")
	fmt.Println("  其他文本 - 发送聊天消息")
	fmt.Println("========================\n")

	for {
		select {
		case <-c.done:
			fmt.Println("\n连接已关闭，客户端退出")
			return
		default:
			fmt.Print("> ")
			if !scanner.Scan() {
				if scanner.Err() != nil {
					log.Printf("读取输入错误: %v", scanner.Err())
				}
				return
			}

			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}

			if err := c.processInput(input); err != nil {
				log.Printf("处理输入错误: %v", err)
				if err == io.EOF {
					return
				}
			}
		}
	}
}

// processInput 处理用户输入
func (c *GameClient) processInput(input string) error {
	// 处理命令
	if strings.HasPrefix(input, "/") {
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		switch command {
		case "/quit":
			if err := c.sendRequest(pb.RequestType_REQUEST_DISCONNECT, "再见"); err != nil {
				return err
			}
			c.done <- true
			return io.EOF

		case "/action":
			if len(parts) < 2 {
				fmt.Println("用法: /action <动作描述>")
				return nil
			}
			return c.sendRequest(pb.RequestType_REQUEST_ACTION, parts[1])

		default:
			fmt.Printf("未知命令: %s\n", command)
			return nil
		}
	}

	// 普通聊天消息
	return c.sendRequest(pb.RequestType_REQUEST_CHAT, input)
}

// sendRequest 发送请求
func (c *GameClient) sendRequest(reqType pb.RequestType, content string) error {
	req := &pb.GameRequest{
		PlayerId:  c.playerID,
		Content:   content,
		Timestamp: time.Now().Unix(),
		Type:      reqType,
	}

	return c.stream.Send(req)
}

func main() {
	// 获取玩家 ID
	var playerID string
	if len(os.Args) > 1 {
		playerID = os.Args[1]
	} else {
		fmt.Print("请输入你的玩家名称: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			playerID = strings.TrimSpace(scanner.Text())
		}
		if playerID == "" {
			playerID = fmt.Sprintf("Player_%d", time.Now().Unix()%1000)
			fmt.Printf("使用默认名称: %s\n", playerID)
		}
	}

	// 创建客户端
	client, err := NewGameClient(playerID)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	log.Printf("玩家 '%s' 准备加入游戏", playerID)
	log.Printf("连接超时: %v, 流超时: %v, 心跳间隔: %v",
		connectionTimeout, streamTimeout, heartbeatInterval)

	// 启动客户端
	if err := client.Start(); err != nil {
		log.Fatalf("启动客户端失败: %v", err)
	}
}
