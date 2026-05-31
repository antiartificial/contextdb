package acquisition

import "testing"

func TestCollectURLsFromText(t *testing.T) {
	urls := collectURLsFromText("See https://docs.example.com/runbook.")
	if len(urls) != 1 || urls[0] != "https://docs.example.com/runbook" {
		t.Fatalf("urls=%#v", urls)
	}
}
