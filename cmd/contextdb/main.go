// Command contextdb starts the ContextDB server.
//
// Configuration is via environment variables:
//
//	CONTEXTDB_MODE                    embedded | standard | remote (default: embedded)
//	CONTEXTDB_DATA_DIR                data directory for embedded+badger (empty = in-memory)
//	CONTEXTDB_DSN                     Postgres DSN for standard mode
//	CONTEXTDB_GRPC_ADDR               gRPC listen address  (default: :7700)
//	CONTEXTDB_REST_ADDR               REST listen address   (default: :7701)
//	CONTEXTDB_OBS_ADDR                observe listen address (default: :7702)
//	CONTEXTDB_LOG_LEVEL               debug | info | warn | error (default: info)
//	CONTEXTDB_FEDERATION_ENABLED      true | false (default: false)
//	CONTEXTDB_FEDERATION_BIND_ADDR    memberlist bind address (default: :7710)
//	CONTEXTDB_FEDERATION_SEED_PEERS   comma-separated list of seed peer addresses
//	CONTEXTDB_FEDERATION_NAMESPACES   comma-separated list of namespaces to federate (empty = all)
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/antiartificial/contextdb/internal/federation"
	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(getenv("CONTEXTDB_LOG_LEVEL", "info")),
	}))
	slog.SetDefault(logger)

	// Open client DB
	opts := client.Options{
		Mode:    client.Mode(getenv("CONTEXTDB_MODE", "embedded")),
		DataDir: os.Getenv("CONTEXTDB_DATA_DIR"),
		DSN:     os.Getenv("CONTEXTDB_DSN"),
		Logger:  logger,
	}

	db, err := client.Open(opts)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	cfg := server.Config{
		GRPCAddr:    getenv("CONTEXTDB_GRPC_ADDR", ":7700"),
		RESTAddr:    getenv("CONTEXTDB_REST_ADDR", ":7701"),
		ObserveAddr: getenv("CONTEXTDB_OBS_ADDR", ":7702"),
		Federation: federation.Config{
			Enabled:    os.Getenv("CONTEXTDB_FEDERATION_ENABLED") == "true",
			BindAddr:   getenv("CONTEXTDB_FEDERATION_BIND_ADDR", ":7710"),
			SeedPeers:  splitComma(os.Getenv("CONTEXTDB_FEDERATION_SEED_PEERS")),
			Namespaces: splitComma(os.Getenv("CONTEXTDB_FEDERATION_NAMESPACES")),
		},
	}

	srv := server.New(db, db.Registry(), cfg, logger)
	if err := srv.Start(); err != nil {
		logger.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	logger.Info("contextdb started",
		"mode", opts.Mode,
		"grpc", cfg.GRPCAddr,
		"rest", cfg.RESTAddr,
		"observe", cfg.ObserveAddr,
	)

	// Wait for shutdown signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logger.Info("shutting down...")
	srv.Stop()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// splitComma splits a comma-separated string into a slice of non-empty trimmed values.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
