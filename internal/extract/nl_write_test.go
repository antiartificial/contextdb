package extract

import (
	"context"
	"testing"
)

type mockNLProvider struct {
	response string
}

func (m *mockNLProvider) Chat(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: m.response}, nil
}

func (m *mockNLProvider) Embed(_ context.Context, _ []string, _ string) ([][]float32, error) {
	return nil, nil
}

func TestNLWriter_Parse(t *testing.T) {
	mock := &mockNLProvider{
		response: `{"content":"The standup meeting is cancelled tomorrow","labels":["meetings","standup"],"confidence":0.9,"epistemic_type":"assertion"}`,
	}

	w := NewNLWriter(mock)
	result, err := w.Parse(context.Background(), NLWriteRequest{
		Text:      "Remember that the standup is cancelled tomorrow",
		Namespace: "test",
		SourceID:  "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "The standup meeting is cancelled tomorrow" {
		t.Errorf("content = %q", result.Content)
	}
	if result.Confidence != 0.9 {
		t.Errorf("confidence = %v", result.Confidence)
	}
	if result.EpistemicType != "assertion" {
		t.Errorf("type = %q", result.EpistemicType)
	}
	if len(result.Labels) != 2 {
		t.Errorf("labels = %v", result.Labels)
	}
}

func TestNLWriter_NilProvider(t *testing.T) {
	w := NewNLWriter(nil)
	_, err := w.Parse(context.Background(), NLWriteRequest{Text: "test"})
	if err == nil {
		t.Error("expected error with nil provider")
	}
}

func TestNLWriter_DefaultConfidence(t *testing.T) {
	mock := &mockNLProvider{
		response: `{"content":"some fact","labels":["test"]}`,
	}
	w := NewNLWriter(mock)
	result, err := w.Parse(context.Background(), NLWriteRequest{Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Confidence != 0.7 {
		t.Errorf("expected default confidence 0.7, got %v", result.Confidence)
	}
	if result.EpistemicType != "assertion" {
		t.Errorf("expected default type assertion, got %q", result.EpistemicType)
	}
}
