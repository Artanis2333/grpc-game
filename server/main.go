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
	port              = ":50051"
	streamTimeout     = 5 * time.Minute  // 流超时时间
	heartbeatInterval = 30 * time.Second // 心跳间隔
)

// GameServer 实现游戏服务
type GameServer struct {
	pb.UnimplementedGameServiceServer
	clients sync.Map // 存储活跃的客户端连接
}

// ClientConnection 客户端连接信息
type ClientConnection struct {
	stream     pb.GameService_BidirectionalStreamServer
	playerID   string
	lastActive time.Time
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// BidirectionalStream 实现双向流式通信
func (s *GameServer) BidirectionalStream(stream pb.GameService_BidirectionalStreamServer) error {
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

		// 广播玩家离开消息
		s.broadcast(&pb.GameResponse{
			FromPlayerId:  "系统",
			Content:       fmt.Sprintf("玩家 %s 离开了游戏", playerID),
			Timestamp:     time.Now().Unix(),
			Type:          pb.ResponseType_RESPONSE_SYSTEM,
			SystemMessage: fmt.Sprintf("玩家 %s 已离线", playerID),
		}, playerID)
	}

	return err
}

// handleRequest 处理接收到的请求
func (s *GameServer) handleRequest(ctx context.Context, req *pb.GameRequest, conn *ClientConnection, playerID *string) error {
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
			Content:       fmt.Sprintf("欢迎 %s 加入游戏！", req.PlayerId),
			Timestamp:     time.Now().Unix(),
			Type:          pb.ResponseType_RESPONSE_SYSTEM,
			SystemMessage: "连接成功",
		}
		if err := conn.stream.Send(welcomeMsg); err != nil {
			return err
		}

		// 广播玩家加入消息
		s.broadcast(&pb.GameResponse{
			FromPlayerId:  "系统",
			Content:       fmt.Sprintf("玩家 %s 加入了游戏", req.PlayerId),
			Timestamp:     time.Now().Unix(),
			Type:          pb.ResponseType_RESPONSE_SYSTEM,
			SystemMessage: fmt.Sprintf("新玩家 %s 已上线", req.PlayerId),
		}, req.PlayerId)
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

	case pb.RequestType_REQUEST_CHAT:
		// 广播聊天消息给所有客户端
		s.broadcast(&pb.GameResponse{
			FromPlayerId: req.PlayerId,
			Content:      req.Content,
			Timestamp:    time.Now().Unix(),
			Type:         pb.ResponseType_RESPONSE_CHAT,
		}, "")
		return nil

	case pb.RequestType_REQUEST_ACTION:
		// 广播动作消息给所有客户端
		s.broadcast(&pb.GameResponse{
			FromPlayerId: req.PlayerId,
			Content:      req.Content,
			Timestamp:    time.Now().Unix(),
			Type:         pb.ResponseType_RESPONSE_ACTION,
		}, "")
		return nil

	case pb.RequestType_REQUEST_DISCONNECT:
		log.Printf("玩家 %s 请求断开连接", req.PlayerId)
		return io.EOF

	default:
		log.Printf("未知请求类型: %v", req.Type)
		// 发送错误响应
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

// broadcast 广播消息给所有客户端（除了排除的客户端）
func (s *GameServer) broadcast(resp *pb.GameResponse, excludePlayerID string) {
	s.clients.Range(func(key, value interface{}) bool {
		playerID := key.(string)
		if playerID == excludePlayerID {
			return true
		}

		conn := value.(*ClientConnection)
		conn.mu.Lock()
		err := conn.stream.Send(resp)
		conn.mu.Unlock()

		if err != nil {
			log.Printf("发送消息给 %s 失败: %v", playerID, err)
			s.clients.Delete(playerID)
		}
		return true
	})
}

// monitorTimeout 监控连接超时
func (s *GameServer) monitorTimeout(ctx context.Context, conn *ClientConnection, errChan chan error) {
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

func main() {
	// 创建 TCP 监听器
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	// 配置 keepalive 参数
	kaep := keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second, // 最小间隔时间
		PermitWithoutStream: true,            // 允许无流时发送 keepalive
	}

	kasp := keepalive.ServerParameters{
		MaxConnectionIdle:     2 * time.Minute, // 最大空闲时间
		MaxConnectionAge:      30 * time.Minute, // 最大连接时间
		MaxConnectionAgeGrace: 5 * time.Second,  // 强制关闭前的宽限时间
		Time:                  10 * time.Second,  // keepalive 间隔
		Timeout:               3 * time.Second,  // keepalive 超时
	}

	// 创建 gRPC 服务器
	s := grpc.NewServer(
		grpc.KeepaliveEnforcementPolicy(kaep),
		grpc.KeepaliveParams(kasp),
	)

	// 注册游戏服务
	gameServer := &GameServer{}
	pb.RegisterGameServiceServer(s, gameServer)

	log.Printf("服务器启动在端口 %s", port)
	log.Printf("流超时时间: %v", streamTimeout)
	log.Printf("心跳间隔: %v", heartbeatInterval)

	// 启动服务器
	if err := s.Serve(lis); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
