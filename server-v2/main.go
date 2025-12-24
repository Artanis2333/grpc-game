package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/Artanis2333/grpc-game/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

const (
	port              = ":50052" // 使用不同端口避免冲突
	streamTimeout     = 5 * time.Minute
	heartbeatInterval = 30 * time.Second
)

// ActionRequest 动作请求，包含请求和响应流
type ActionRequest struct {
	Request  *pb.GameRequest
	PlayerID string
	Stream   pb.GameService_BidirectionalStreamServer
}

// GameServerV2 实现游戏服务（解耦版本）
type GameServerV2 struct {
	pb.UnimplementedGameServiceServer
	actionChan chan *ActionRequest  // 动作请求 channel
	clients    sync.Map             // 存储活跃的客户端连接
}

// ClientConnection 客户端连接信息
type ClientConnection struct {
	stream     pb.GameService_BidirectionalStreamServer
	playerID   string
	lastActive time.Time
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// NewGameServerV2 创建新的游戏服务器
func NewGameServerV2() *GameServerV2 {
	return &GameServerV2{
		actionChan: make(chan *ActionRequest, 100), // 缓冲 channel
	}
}

// BidirectionalStream 实现双向流式通信
func (s *GameServerV2) BidirectionalStream(stream pb.GameService_BidirectionalStreamServer) error {
	// 创建带超时的 context
	ctx, cancel := context.WithTimeout(stream.Context(), streamTimeout)
	defer cancel()

	var playerID string
	clientConn := &ClientConnection{
		stream:     stream,
		lastActive: time.Now(),
		cancel:     cancel,
	}

	// 启动超时检查协程
	errChan := make(chan error, 1)
	go s.monitorTimeout(ctx, clientConn, errChan)

	// 接收消息
	go func() {
		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
				req, err := stream.Recv()
				if err == io.EOF {
					log.Printf("客户端 %s 关闭了连接", playerID)
					errChan <- nil
					return
				}
				if err != nil {
					log.Printf("接收消息错误: %v", err)
					errChan <- err
					return
				}

				// 更新最后活跃时间
				clientConn.mu.Lock()
				clientConn.lastActive = time.Now()
				clientConn.mu.Unlock()

				// 处理消息
				if err := s.handleRequest(ctx, req, clientConn, &playerID); err != nil {
					errChan <- err
					return
				}
			}
		}
	}()

	// 等待错误或完成
	err := <-errChan
	if playerID != "" {
		s.clients.Delete(playerID)
		log.Printf("玩家 %s 断开连接", playerID)
	}

	return err
}

// handleRequest 处理接收到的请求
func (s *GameServerV2) handleRequest(ctx context.Context, req *pb.GameRequest, conn *ClientConnection, playerID *string) error {
	log.Printf("收到请求 - 玩家: %s, 类型: %v, 内容: %s", req.PlayerId, req.Type, req.Content)

	// 首次连接，保存客户端信息
	if *playerID == "" {
		*playerID = req.PlayerId
		conn.playerID = req.PlayerId
		s.clients.Store(req.PlayerId, conn)
		log.Printf("玩家 %s 已连接", req.PlayerId)

		// 发送欢迎消息
		welcomeMsg := &pb.GameResponse{
			FromPlayerId:  "系统",
			Content:       fmt.Sprintf("欢迎 %s 加入游戏！（V2服务器 - Channel架构）", req.PlayerId),
			Timestamp:     time.Now().Unix(),
			Type:          pb.ResponseType_RESPONSE_SYSTEM,
			SystemMessage: "连接成功",
		}
		if err := conn.stream.Send(welcomeMsg); err != nil {
			return err
		}
	}

	switch req.Type {
	case pb.RequestType_REQUEST_HEARTBEAT:
		// 响应心跳
		response := &pb.GameResponse{
			FromPlayerId: "服务器",
			Content:      "pong",
			Timestamp:    time.Now().Unix(),
			Type:         pb.ResponseType_RESPONSE_HEARTBEAT,
		}
		return conn.stream.Send(response)

	case pb.RequestType_REQUEST_ACTION:
		// 将动作请求发送到 channel，由专门的协程处理
		actionReq := &ActionRequest{
			Request:  req,
			PlayerID: *playerID,
			Stream:   conn.stream,
		}
		
		select {
		case s.actionChan <- actionReq:
			log.Printf("动作请求已发送到处理队列: 玩家=%s, 动作=%s", *playerID, req.Content)
		case <-ctx.Done():
			return ctx.Err()
		default:
			// channel 满了，发送错误响应
			errorMsg := &pb.GameResponse{
				FromPlayerId:  "服务器",
				Content:       "服务器繁忙，请稍后重试",
				Timestamp:     time.Now().Unix(),
				Type:          pb.ResponseType_RESPONSE_ERROR,
				SystemMessage: "动作队列已满",
			}
			return conn.stream.Send(errorMsg)
		}
		return nil

	case pb.RequestType_REQUEST_DISCONNECT:
		log.Printf("玩家 %s 请求断开连接", req.PlayerId)
		return io.EOF

	default:
		log.Printf("未知请求类型: %v", req.Type)
		errorMsg := &pb.GameResponse{
			FromPlayerId:  "服务器",
			Content:       "未知的请求类型",
			Timestamp:     time.Now().Unix(),
			Type:          pb.ResponseType_RESPONSE_ERROR,
			SystemMessage: fmt.Sprintf("错误: 未知请求类型 %v", req.Type),
		}
		return conn.stream.Send(errorMsg)
	}
}

// monitorTimeout 监控连接超时
func (s *GameServerV2) monitorTimeout(ctx context.Context, conn *ClientConnection, errChan chan error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conn.mu.Lock()
			elapsed := time.Since(conn.lastActive)
			conn.mu.Unlock()

			if elapsed > streamTimeout {
				log.Printf("玩家 %s 连接超时 (无活动时间: %v)", conn.playerID, elapsed)
				errChan <- status.Error(codes.DeadlineExceeded, "连接超时")
				return
			}
		}
	}
}

// processActionRequests 处理动作请求的专用协程
func (s *GameServerV2) processActionRequests(ctx context.Context) {
	log.Println("🎮 动作处理协程已启动")
	
	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 动作处理协程退出")
			return
			
		case actionReq := <-s.actionChan:
			// 处理动作请求
			s.handleAction(actionReq)
		}
	}
}

// handleAction 处理具体的动作
func (s *GameServerV2) handleAction(actionReq *ActionRequest) {
	log.Printf("⚡ 处理动作: 玩家=%s, 内容=%s", actionReq.PlayerID, actionReq.Request.Content)
	
	// 模拟处理时间（可以是复杂的游戏逻辑）
	time.Sleep(100 * time.Millisecond)
	
	// 构造响应
	response := &pb.GameResponse{
		FromPlayerId:  "游戏引擎",
		Content:       fmt.Sprintf("已处理动作: %s", actionReq.Request.Content),
		Timestamp:     time.Now().Unix(),
		Type:          pb.ResponseType_RESPONSE_ACTION,
		SystemMessage: fmt.Sprintf("玩家 %s 的动作已执行", actionReq.PlayerID),
	}
	
	// 发送响应给请求的客户端
	if err := actionReq.Stream.Send(response); err != nil {
		log.Printf("❌ 发送动作响应失败: 玩家=%s, 错误=%v", actionReq.PlayerID, err)
		return
	}
	
	log.Printf("✅ 动作响应已发送: 玩家=%s", actionReq.PlayerID)
	
	// 可选：广播给所有其他客户端
	s.broadcastAction(actionReq.PlayerID, response)
}

// broadcastAction 广播动作给所有其他客户端
func (s *GameServerV2) broadcastAction(excludePlayerID string, resp *pb.GameResponse) {
	s.clients.Range(func(key, value interface{}) bool {
		playerID := key.(string)
		if playerID == excludePlayerID {
			return true // 跳过发起者
		}

		conn := value.(*ClientConnection)
		conn.mu.Lock()
		err := conn.stream.Send(resp)
		conn.mu.Unlock()

		if err != nil {
			log.Printf("广播失败: 玩家=%s, 错误=%v", playerID, err)
			s.clients.Delete(playerID)
		}
		return true
	})
}

func main() {
	// 创建 TCP 监听器
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	// 配置 keepalive 参数
	kaep := keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second,
		PermitWithoutStream: true,
	}

	kasp := keepalive.ServerParameters{
		MaxConnectionIdle:     2 * time.Minute,
		MaxConnectionAge:      30 * time.Minute,
		MaxConnectionAgeGrace: 5 * time.Second,
		Time:                  10 * time.Second,
		Timeout:               3 * time.Second,
	}

	// 创建 gRPC 服务器
	grpcServer := grpc.NewServer(
		grpc.KeepaliveEnforcementPolicy(kaep),
		grpc.KeepaliveParams(kasp),
	)

	// 创建游戏服务器实例
	gameServer := NewGameServerV2()
	pb.RegisterGameServiceServer(grpcServer, gameServer)

	// 启动动作处理协程
	ctx := context.Background()
	go gameServer.processActionRequests(ctx)

	log.Println("========================================")
	log.Printf("🚀 服务器 V2 启动在端口 %s", port)
	log.Printf("📊 架构: Channel 解耦模式")
	log.Printf("⏰ 流超时时间: %v", streamTimeout)
	log.Printf("💓 心跳间隔: %v", heartbeatInterval)
	log.Printf("🎮 动作处理: 专用协程")
	log.Println("========================================")

	// 启动服务器
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
