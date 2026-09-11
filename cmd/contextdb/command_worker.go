package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/antiartificial/contextdb/pkg/client"
)

func runWorker(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := executeReviewWorker(ctx, args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// executeReviewWorker calls the running server and never opens its own database.
func executeReviewWorker(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] != "review" {
		return fmt.Errorf("contextdb worker: expected review")
	}
	fs := flag.NewFlagSet("contextdb worker review", flag.ContinueOnError)
	base := fs.String("url", getenv("CONTEXTDB_REST_URL", "http://127.0.0.1:7701"), "server REST URL")
	ns := fs.String("namespace", "default", "namespace")
	once := fs.Bool("once", false, "run one cycle")
	interval := fs.Duration("interval", 5*time.Minute, "cycle interval")
	execute := fs.Bool("execute", false, "apply explicitly allowed actions")
	actions := fs.String("allowed-actions", "", "comma-separated validate, refute, stale, resolve actions")
	evaluator := fs.String("evaluator", "rules", "rules or webhook")
	limit := fs.Int("limit", 25, "maximum items per cycle (1-100)")
	token := fs.String("token", os.Getenv("CONTEXTDB_TOKEN"), "optional bearer token")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *interval <= 0 || *limit < 1 || *limit > 100 || strings.TrimSpace(*ns) == "" {
		return fmt.Errorf("positive interval, non-empty namespace, and limit between 1 and 100 required")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		payload, err := json.Marshal(client.ReviewWorkerRequest{Execute: *execute, Limit: *limit, AllowedActions: splitComma(*actions), Evaluator: *evaluator})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(*base, "/")+"/v1/namespaces/"+url.PathEscape(*ns)+"/review/worker/cycle", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if *token != "" {
			req.Header.Set("Authorization", "Bearer "+*token)
		}
		resp, err := (&http.Client{Timeout: 35 * time.Second}).Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("worker cycle failed: %s", resp.Status)
		}
		var run client.ReviewWorkerRun
		if err := json.Unmarshal(body, &run); err != nil {
			return fmt.Errorf("worker cycle invalid response: %w", err)
		}
		if _, err := fmt.Fprintln(output, string(body)); err != nil {
			return err
		}
		if len(run.Errors) > 0 || run.Status == "needs_attention" {
			return fmt.Errorf("worker cycle %s requires attention", run.RunID)
		}
		if *once {
			return nil
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
