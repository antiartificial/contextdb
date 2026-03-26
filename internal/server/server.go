package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"

	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/pkg/client"
)

// Config configures the unified server.
type Config struct {
	GRPCAddr    string // default: ":7700"
	RESTAddr    string // default: ":7701"
	ObserveAddr string // default: ":7702"
}

func (c Config) withDefaults() Config {
	if c.GRPCAddr == "" {
		c.GRPCAddr = ":7700"
	}
	if c.RESTAddr == "" {
		c.RESTAddr = ":7701"
	}
	if c.ObserveAddr == "" {
		c.ObserveAddr = ":7702"
	}
	return c
}

// Server manages gRPC, REST, and observability listeners.
type Server struct {
	db     *client.DB
	reg    *observe.Registry
	config Config
	logger *slog.Logger

	grpcServer *grpc.Server
	restServer *http.Server
	obsServer  *http.Server
}

// New creates a new Server.
func New(db *client.DB, reg *observe.Registry, cfg Config, logger *slog.Logger) *Server {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		db:     db,
		reg:    reg,
		config: cfg,
		logger: logger,
	}
}

// Start starts all server listeners. Non-blocking.
func (s *Server) Start() error {
	// gRPC server
	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(TenantInterceptor()),
		FormatGRPCCodec(),
	)
	grpcSvc := NewGRPCService(s.db)
	grpcSvc.Register(s.grpcServer)

	grpcLis, err := net.Listen("tcp", s.config.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", s.config.GRPCAddr, err)
	}
	go func() {
		s.logger.Info("gRPC server started", "addr", s.config.GRPCAddr)
		if err := s.grpcServer.Serve(grpcLis); err != nil {
			s.logger.Error("gRPC server error", "error", err)
		}
	}()

	// REST server
	restSvc := NewRESTServer(s.db)
	restHandler := TenantMiddleware(restSvc.Handler())
	s.restServer = &http.Server{
		Addr:    s.config.RESTAddr,
		Handler: restHandler,
	}
	go func() {
		s.logger.Info("REST server started", "addr", s.config.RESTAddr)
		if err := s.restServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("REST server error", "error", err)
		}
	}()

	// Observe server (metrics, pprof, health)
	if s.reg != nil {
		s.obsServer = &http.Server{
			Addr:    s.config.ObserveAddr,
			Handler: observe.Handler(s.reg),
		}
		go func() {
			s.logger.Info("observe server started", "addr", s.config.ObserveAddr)
			if err := s.obsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Error("observe server error", "error", err)
			}
		}()
	}

	return nil
}

// Stop gracefully shuts down all servers.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	if s.restServer != nil {
		s.restServer.Shutdown(ctx)
	}
	if s.obsServer != nil {
		s.obsServer.Shutdown(ctx)
	}
	s.logger.Info("all servers stopped")
}
