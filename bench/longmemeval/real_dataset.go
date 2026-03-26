package longmemeval

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// HuggingFaceURL is the default URL for the LongMemEval dataset.
const HuggingFaceURL = "https://huggingface.co/datasets/xiaowu0104/LongMemEval/resolve/main/longmemeval.json"

// LoadOrDownload loads the real LongMemEval dataset from the given path.
// If the file does not exist, it attempts to download from HuggingFace.
// The HuggingFace dataset format may differ from the internal format,
// so this function performs format normalisation.
func LoadOrDownload(cachePath string) (*Dataset, error) {
	// Try loading from cache first.
	if _, err := os.Stat(cachePath); err == nil {
		return loadRealDataset(cachePath)
	}

	// Download from HuggingFace.
	if err := download(HuggingFaceURL, cachePath); err != nil {
		return nil, fmt.Errorf("longmemeval: download: %w", err)
	}

	return loadRealDataset(cachePath)
}

// download fetches a URL to a local file path, creating parent dirs.
func download(url, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	resp, err := http.Get(url) //nolint:gosec // known URL
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// rawEntry is the expected structure of each entry in the HuggingFace JSON.
// The real LongMemEval dataset has conversation sessions and evaluation
// questions. We normalise to our internal Dataset format.
type rawEntry struct {
	SessionID string `json:"session_id"`
	Turns     []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"turns"`
	// Query fields (present on evaluation entries)
	QueryID          string   `json:"query_id"`
	Question         string   `json:"question"`
	Answer           string   `json:"answer"`
	Category         string   `json:"category"`
	RequiredSessions []string `json:"required_sessions"`
}

// rawDataset represents the top-level structure which may be an array
// of entries or an object with "sessions" and "queries" keys.
type rawDataset struct {
	Sessions []rawEntry `json:"sessions"`
	Queries  []rawEntry `json:"queries"`
}

// loadRealDataset loads and normalises the real dataset.
func loadRealDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	// Try direct Dataset parse first (matches our internal format).
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err == nil && len(ds.Sessions) > 0 {
		return &ds, nil
	}

	// Try the raw format with sessions+queries object.
	var raw rawDataset
	if err := json.Unmarshal(data, &raw); err == nil && len(raw.Sessions) > 0 {
		return normaliseRawDataset(raw), nil
	}

	// Try as a flat array of entries (mixed sessions and queries).
	var entries []rawEntry
	if err := json.Unmarshal(data, &entries); err == nil && len(entries) > 0 {
		return normaliseEntries(entries), nil
	}

	return nil, fmt.Errorf("longmemeval: unrecognised dataset format")
}

func normaliseRawDataset(raw rawDataset) *Dataset {
	ds := &Dataset{}

	for _, s := range raw.Sessions {
		sess := Session{ID: s.SessionID}
		for _, t := range s.Turns {
			sess.Turns = append(sess.Turns, Turn{Role: t.Role, Content: t.Content})
		}
		ds.Sessions = append(ds.Sessions, sess)
	}

	for _, q := range raw.Queries {
		cat := q.Category
		if cat == "" {
			cat = "single-session"
		}
		ds.Queries = append(ds.Queries, Query{
			ID:               q.QueryID,
			SessionID:        q.SessionID,
			Question:         q.Question,
			GoldAnswer:       q.Answer,
			Category:         cat,
			RequiredSessions: q.RequiredSessions,
		})
	}

	return ds
}

func normaliseEntries(entries []rawEntry) *Dataset {
	ds := &Dataset{}
	seenSessions := make(map[string]bool)

	for _, e := range entries {
		// Entry with turns → session.
		if len(e.Turns) > 0 && !seenSessions[e.SessionID] {
			seenSessions[e.SessionID] = true
			sess := Session{ID: e.SessionID}
			for _, t := range e.Turns {
				sess.Turns = append(sess.Turns, Turn{Role: t.Role, Content: t.Content})
			}
			ds.Sessions = append(ds.Sessions, sess)
		}

		// Entry with question → query.
		if e.Question != "" {
			cat := e.Category
			if cat == "" {
				cat = "single-session"
			}
			ds.Queries = append(ds.Queries, Query{
				ID:               e.QueryID,
				SessionID:        e.SessionID,
				Question:         e.Question,
				GoldAnswer:       e.Answer,
				Category:         cat,
				RequiredSessions: e.RequiredSessions,
			})
		}
	}

	return ds
}
