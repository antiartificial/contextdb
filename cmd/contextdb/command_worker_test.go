package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerOnceFailuresAndEscaping(t *testing.T) {
	for _, code := range []int{200, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.EscapedPath(), "team%2Fproject") {
				t.Errorf("unescaped path %s", r.URL.EscapedPath())
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"status":"completed","errors":[]}`))
		}))
		err := executeReviewWorker(context.Background(), []string{"review", "--once", "--url", srv.URL, "--namespace", "team/project"}, io.Discard)
		srv.Close()
		if (err == nil) != (code == 200) {
			t.Fatalf("status %d error %v", code, err)
		}
	}
	if executeReviewWorker(context.Background(), []string{"review", "--interval=0"}, io.Discard) == nil {
		t.Fatal("zero interval accepted")
	}
	if executeReviewWorker(context.Background(), []string{"unknown"}, io.Discard) == nil {
		t.Fatal("unknown command accepted")
	}
}
