package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookReviewEvaluator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing token")
		}
		w.Write([]byte(`{"action":"stale","confidence":0.9,"reason":"expired"}`))
	}))
	defer srv.Close()
	action, err := evaluateReviewWebhook(context.Background(), ReviewWorkerRequest{WebhookURL: srv.URL, WebhookToken: "secret"}, ReviewItem{})
	if err != nil || action != "stale" {
		t.Fatalf("action=%q err=%v", action, err)
	}
}
func TestWebhookReviewEvaluatorRejectsMalformedAndCancelled(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`bad`)) }))
	defer bad.Close()
	if _, err := evaluateReviewWebhook(context.Background(), ReviewWorkerRequest{WebhookURL: bad.URL}, ReviewItem{}); err == nil {
		t.Fatal("expected malformed response error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := evaluateReviewWebhook(ctx, ReviewWorkerRequest{WebhookURL: bad.URL, Timeout: time.Second}, ReviewItem{}); err == nil {
		t.Fatal("expected cancellation error")
	}
}
