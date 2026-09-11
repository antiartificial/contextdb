package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/antiartificial/contextdb/internal/acquisition"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func runConnectors(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb connectors: expected serve")
		os.Exit(2)
	}
	switch args[0] {
	case "serve":
		runConnectorsServe(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb connectors: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runConnectorsServe(args []string) {
	fs := flag.NewFlagSet("contextdb connectors serve", flag.ExitOnError)
	addr := fs.String("addr", getenv("CONTEXTDB_CONNECTOR_ADDR", ":7780"), "connector listen address")
	providers := fs.String("providers", getenv("CONTEXTDB_CONNECTOR_PROVIDERS", "openai,xai,anthropic"), "comma-separated providers to expose")
	allowedDomains := fs.String("allowed-domains", os.Getenv("CONTEXTDB_CONNECTOR_ALLOWED_DOMAINS"), "comma-separated domains allowed for provider web search")
	blockedDomains := fs.String("blocked-domains", os.Getenv("CONTEXTDB_CONNECTOR_BLOCKED_DOMAINS"), "comma-separated domains blocked for provider web search")
	_ = fs.Parse(args)

	configs := map[string]acquisition.ProviderConfig{}
	for _, provider := range splitComma(*providers) {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		cfg := acquisition.ProviderConfig{
			Provider:       provider,
			AllowedDomains: splitComma(*allowedDomains),
			BlockedDomains: splitComma(*blockedDomains),
		}
		switch provider {
		case acquisition.ProviderOpenAI:
			cfg.APIKey = os.Getenv("OPENAI_API_KEY")
			cfg.Model = getenv("CONTEXTDB_OPENAI_CONNECTOR_MODEL", "gpt-5")
			cfg.BaseURL = getenv("CONTEXTDB_OPENAI_BASE_URL", "https://api.openai.com/v1")
		case acquisition.ProviderXAI:
			cfg.APIKey = os.Getenv("XAI_API_KEY")
			cfg.Model = getenv("CONTEXTDB_XAI_CONNECTOR_MODEL", "grok-4.3")
			cfg.BaseURL = getenv("CONTEXTDB_XAI_BASE_URL", "https://api.x.ai/v1")
		case acquisition.ProviderAnthropic:
			cfg.APIKey = os.Getenv("ANTHROPIC_API_KEY")
			cfg.Model = getenv("CONTEXTDB_ANTHROPIC_CONNECTOR_MODEL", "claude-sonnet-4-20250514")
			cfg.BaseURL = getenv("CONTEXTDB_ANTHROPIC_BASE_URL", "https://api.anthropic.com")
			cfg.MaxUses = parseEnvInt("CONTEXTDB_ANTHROPIC_WEB_SEARCH_MAX_USES", 3)
		default:
			fmt.Fprintf(os.Stderr, "contextdb connectors serve: unsupported provider %q\n", provider)
			os.Exit(2)
		}
		if strings.TrimSpace(cfg.APIKey) == "" {
			fmt.Fprintf(os.Stderr, "contextdb connectors serve: missing API key for %s\n", provider)
			os.Exit(2)
		}
		configs[provider] = cfg
	}
	if len(configs) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb connectors serve: no providers configured")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(getenv("CONTEXTDB_LOG_LEVEL", "info")),
	}))
	slog.SetDefault(logger)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           acquisition.Server{Providers: configs}.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("contextdb acquisition connectors started", "addr", *addr, "providers", strings.Join(mapKeys(configs), ","))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("connector server stopped", "error", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
