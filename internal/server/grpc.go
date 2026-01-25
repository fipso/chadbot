package server

import (
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/plugin"
)

// GRPCServer wraps the gRPC server for plugin communication
type GRPCServer struct {
	pb.UnimplementedPluginServiceServer
	server  *grpc.Server
	handler *plugin.Handler
	socket  string
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(handler *plugin.Handler, socket string) *GRPCServer {
	return &GRPCServer{
		handler: handler,
		socket:  socket,
	}
}

// Start starts the gRPC server on the Unix socket
func (s *GRPCServer) Start() error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(s.socket); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		return err
	}

	// Set socket permissions
	if err := os.Chmod(s.socket, 0666); err != nil {
		log.Printf("[GRPC] Warning: failed to set socket permissions: %v", err)
	}

	s.server = grpc.NewServer()
	pb.RegisterPluginServiceServer(s.server, s)

	// Enable reflection for debugging with grpcurl
	reflection.Register(s.server)

	log.Printf("[GRPC] Server listening on %s", s.socket)
	return s.server.Serve(listener)
}

// Stop gracefully stops the gRPC server
func (s *GRPCServer) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
		log.Printf("[GRPC] Server stopped")
	}
}

// Connect implements the bidirectional streaming RPC
func (s *GRPCServer) Connect(stream pb.PluginService_ConnectServer) error {
	return s.handler.HandleConnection(stream)
}
