package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/antiartificial/contextdb/internal/admin"
	"github.com/antiartificial/contextdb/internal/federation"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/pkg/client"
)

// Config configures the unified server.
type Config struct {
	GRPCAddr    string // default: ":7700"
	RESTAddr    string // default: ":7701"
	ObserveAddr string // default: ":7702"
	Federation  federation.Config
	AuthTokens  string
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
	auth   *TokenRegistry

	grpcServer *grpc.Server
	restServer *http.Server
	obsServer  *http.Server
	federation *federation.Federation
}

// New creates a new Server.
func New(db *client.DB, reg *observe.Registry, cfg Config, logger *slog.Logger) *Server {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	auth, _ := NewTokenRegistry(cfg.AuthTokens)
	return &Server{
		db:     db,
		reg:    reg,
		config: cfg,
		logger: logger,
		auth:   auth,
	}
}

// Start starts all server listeners. Non-blocking.
func (s *Server) Start() error {
	if s.config.AuthTokens != "" {
		var err error
		if s.auth, err = NewTokenRegistry(s.config.AuthTokens); err != nil {
			return fmt.Errorf("configure auth: %w", err)
		}
	}
	// Bind every configured listener before serving any request. This makes a
	// port conflict an immediate startup error and avoids a partially running
	// server when REST or observability cannot bind.
	grpcLis, err := net.Listen("tcp", s.config.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", s.config.GRPCAddr, err)
	}
	restLis, err := net.Listen("tcp", s.config.RESTAddr)
	if err != nil {
		_ = grpcLis.Close()
		return fmt.Errorf("listen rest %s: %w", s.config.RESTAddr, err)
	}
	var obsLis net.Listener
	if s.reg != nil {
		obsLis, err = net.Listen("tcp", s.config.ObserveAddr)
		if err != nil {
			_ = restLis.Close()
			_ = grpcLis.Close()
			return fmt.Errorf("listen observe %s: %w", s.config.ObserveAddr, err)
		}
	}

	// Start optional federation before exposing any request listener.
	if s.config.Federation.Enabled {
		graph, vecs, _, log := s.db.Stores()
		f := federation.New(graph, vecs, log, s.config.Federation, s.logger)
		if err := f.Start(context.Background()); err != nil {
			_ = grpcLis.Close()
			_ = restLis.Close()
			if obsLis != nil {
				_ = obsLis.Close()
			}
			return fmt.Errorf("start federation: %w", err)
		}
		s.federation = f
	}

	// gRPC server
	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(s.auth.GRPCInterceptor(), TenantInterceptor()),
		grpc.ChainStreamInterceptor(s.auth.GRPCStreamInterceptor()),
		FormatGRPCCodec(),
	)
	grpcSvc := NewGRPCService(s.db)
	grpcSvc.Register(s.grpcServer)

	go func() {
		s.logger.Info("gRPC server started", "addr", s.config.GRPCAddr)
		if err := s.grpcServer.Serve(grpcLis); err != nil {
			s.logger.Error("gRPC server error", "error", err)
		}
	}()

	// REST server
	restSvc := NewRESTServer(s.db)
	restHandler := s.auth.Middleware(TenantMiddleware(restSvc.Handler()))
	s.restServer = &http.Server{
		Addr:    s.config.RESTAddr,
		Handler: restHandler,
	}
	go func() {
		s.logger.Info("REST server started", "addr", s.config.RESTAddr)
		if err := s.restServer.Serve(restLis); err != nil && err != http.ErrServerClosed {
			s.logger.Error("REST server error", "error", err)
		}
	}()

	// Observe server (metrics, pprof, health)
	if s.reg != nil {
		obsMux := http.NewServeMux()
		obsMux.Handle("/", observe.Handler(s.reg))
		obsMux.Handle("/admin/", admin.New(s.db))
		s.obsServer = &http.Server{
			Addr:    s.config.ObserveAddr,
			Handler: s.auth.Middleware(obsMux),
		}
		go func() {
			s.logger.Info("observe server started", "addr", s.config.ObserveAddr)
			if err := s.obsServer.Serve(obsLis); err != nil && err != http.ErrServerClosed {
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

	var wg sync.WaitGroup
	if s.grpcServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.grpcServer.GracefulStop()
		}()
	}
	if s.restServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.restServer.Shutdown(ctx)
		}()
	}
	if s.obsServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.obsServer.Shutdown(ctx)
		}()
	}

	if s.federation != nil {
		wg.Add(1)
		go func() { defer wg.Done(); s.federation.Stop() }()
	}

	stopped := make(chan struct{})
	go func() {
		wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		if s.grpcServer != nil {
			s.grpcServer.Stop()
		}
		if s.restServer != nil {
			_ = s.restServer.Close()
		}
		if s.obsServer != nil {
			_ = s.obsServer.Close()
		}
	}
	s.logger.Info("all servers stopped")
}
