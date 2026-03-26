// Package longmemeval provides a LongMemEval benchmark runner for contextdb.
//
// LongMemEval evaluates long-term memory retrieval across multi-session
// conversations. This package can load the real LongMemEval dataset from
// JSON or generate a synthetic dataset for quick testing.
//
// Usage:
//
//	ds := longmemeval.GenerateSyntheticDataset()
//	runner := longmemeval.NewRunner(db, longmemeval.Config{})
//	report, err := runner.Run(ctx, ds)
//	runner.PrintReport(report)
package longmemeval

import (
	"encoding/json"
	"fmt"
	"os"
)

// Session represents a conversation session from the LongMemEval dataset.
type Session struct {
	ID    string `json:"session_id"`
	Turns []Turn `json:"turns"`
}

// Turn is a single turn in a conversation.
type Turn struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// Query represents a LongMemEval evaluation query.
type Query struct {
	ID               string   `json:"query_id"`
	SessionID        string   `json:"session_id"`
	Question         string   `json:"question"`
	GoldAnswer       string   `json:"gold_answer"`
	Category         string   `json:"category"`          // "single-session", "multi-session", "temporal"
	RequiredSessions []string `json:"required_sessions"` // session IDs needed
}

// Dataset holds the full LongMemEval benchmark data.
type Dataset struct {
	Sessions []Session `json:"sessions"`
	Queries  []Query   `json:"queries"`
}

// LoadDataset loads a LongMemEval dataset from a JSON file.
func LoadDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("longmemeval: read dataset: %w", err)
	}
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("longmemeval: parse dataset: %w", err)
	}
	return &ds, nil
}

// GenerateSyntheticDataset creates a small synthetic dataset for testing
// when the real LongMemEval data is not available. It produces 10 sessions
// with 3-5 turns each and 10 queries across the three categories.
func GenerateSyntheticDataset() *Dataset {
	sessions := []Session{
		{
			ID: "s01",
			Turns: []Turn{
				{Role: "user", Content: "What is the difference between a goroutine and an OS thread?"},
				{Role: "assistant", Content: "A goroutine is a lightweight thread managed by the Go runtime. It uses a segmented stack starting at around 2KB, whereas OS threads typically use 1-8MB. The Go scheduler multiplexes goroutines onto OS threads using an M:N model."},
				{Role: "user", Content: "How does the scheduler decide which goroutine runs next?"},
				{Role: "assistant", Content: "The Go scheduler uses a work-stealing algorithm. Each processor (P) has a local run queue. When empty, it steals from other Ps or checks the global queue. Since Go 1.14 goroutines can be preempted asynchronously."},
			},
		},
		{
			ID: "s02",
			Turns: []Turn{
				{Role: "user", Content: "Can you explain how Go interfaces work?"},
				{Role: "assistant", Content: "Go interfaces are satisfied implicitly. A type implements an interface by implementing its methods; there is no explicit declaration. Under the hood, an interface value is a pair of pointers: one to the type information and one to the data."},
				{Role: "user", Content: "What is the empty interface and how do type assertions work?"},
				{Role: "assistant", Content: "The empty interface interface{} or any in Go 1.18+ is satisfied by every type. A type assertion x.(T) extracts the concrete value of type T. If x does not hold a T, it panics unless you use the two-value form: v, ok := x.(T), where ok is false on mismatch."},
			},
		},
		{
			ID: "s03",
			Turns: []Turn{
				{Role: "user", Content: "I want to learn about Go's garbage collector."},
				{Role: "assistant", Content: "Go uses a concurrent, tri-color mark-and-sweep garbage collector. It runs concurrently with the application to minimize stop-the-world pauses. The collector was redesigned in Go 1.5 and has been improved in every release since."},
				{Role: "user", Content: "What is the write barrier and how can I tune GC performance?"},
				{Role: "assistant", Content: "The write barrier intercepts pointer writes during the marking phase. Go uses a hybrid write barrier combining Dijkstra and Yuasa approaches since Go 1.8. For tuning, the primary knob is GOGC which controls the heap growth ratio. You can also use debug.SetMemoryLimit in Go 1.19+ to set a soft memory limit."},
			},
		},
		{
			ID: "s04",
			Turns: []Turn{
				{Role: "user", Content: "What are channels in Go and when should I use them?"},
				{Role: "assistant", Content: "Channels are typed conduits for communication between goroutines. Use them when you need to synchronize execution or pass data between goroutines. Unbuffered channels block both sender and receiver until the other side is ready. Buffered channels have a specified capacity and only block when the buffer is full."},
				{Role: "user", Content: "Can I use select with channels?"},
				{Role: "assistant", Content: "Yes, the select statement lets you wait on multiple channel operations simultaneously. It blocks until one case can proceed. If multiple are ready, one is chosen at random. A default case makes it non-blocking."},
				{Role: "user", Content: "What patterns work well with channels?"},
			},
		},
		{
			ID: "s05",
			Turns: []Turn{
				{Role: "user", Content: "How does error handling work in Go?"},
				{Role: "assistant", Content: "Go uses explicit error returns instead of exceptions. Functions return an error as the last return value. Callers check if err != nil to handle errors. The errors package provides Is, As, and Unwrap for error inspection."},
				{Role: "user", Content: "How do I wrap errors and what about panic and recover?"},
				{Role: "assistant", Content: "Use fmt.Errorf with the percent-w verb to wrap errors: fmt.Errorf(\"open config: %w\", err). This preserves the error chain for errors.Is and errors.As. Panic is for unrecoverable errors; recover can catch panics within a deferred function but should not be used for normal control flow."},
			},
		},
		{
			ID: "s06",
			Turns: []Turn{
				{Role: "user", Content: "I need to build a REST API in Go. What packages should I use?"},
				{Role: "assistant", Content: "The standard library net/http is sufficient for most REST APIs. For routing, Go 1.22 added method-based patterns in the default mux. Popular third-party routers include chi and gorilla/mux. For JSON, use encoding/json."},
				{Role: "user", Content: "How do I add middleware and handle graceful shutdown?"},
				{Role: "assistant", Content: "Middleware in Go wraps an http.Handler, performing logic before and after the next handler. For graceful shutdown, use http.Server.Shutdown with a context. Listen for OS signals like SIGINT and SIGTERM, then call Shutdown which waits for in-flight requests to complete."},
			},
		},
		{
			ID: "s07",
			Turns: []Turn{
				{Role: "user", Content: "Explain Go modules and dependency management."},
				{Role: "assistant", Content: "Go modules were introduced in Go 1.11 and became the default in Go 1.16. A module is defined by a go.mod file listing the module path and dependencies. The go.sum file locks dependency checksums for reproducible builds."},
				{Role: "user", Content: "How do I update dependencies and what is the replace directive?"},
				{Role: "assistant", Content: "Use go get -u to update to the latest minor or patch version. Run go mod tidy to clean up. The replace directive in go.mod redirects a module path to another location such as a local directory or fork, useful during development."},
				{Role: "user", Content: "Are there tools for managing module versions in CI?"},
			},
		},
		{
			ID: "s08",
			Turns: []Turn{
				{Role: "user", Content: "How do generics work in Go since 1.18?"},
				{Role: "assistant", Content: "Go 1.18 introduced type parameters. Functions and types can be parameterized with type constraints using square brackets: func Map[T, U any](s []T, f func(T) U) []U. Constraints can be interfaces with type lists."},
				{Role: "user", Content: "When should I use generics versus interfaces?"},
				{Role: "assistant", Content: "Use generics when you need type-safe containers or algorithms that work across types. Use interfaces when you need runtime polymorphism. The comparable constraint allows == and != on type parameters. Often the best code uses both together."},
			},
		},
		{
			ID: "s09",
			Turns: []Turn{
				{Role: "user", Content: "Tell me about testing in Go."},
				{Role: "assistant", Content: "Go has a built-in testing framework in the testing package. Test files end in _test.go and test functions start with Test. Run tests with go test. The framework supports subtests, benchmarks, fuzzing since Go 1.18, and example tests."},
				{Role: "user", Content: "How do table-driven tests work and what about mocking?"},
				{Role: "assistant", Content: "Table-driven tests define test cases as a slice of structs, iterating with t.Run for subtests. This pattern is idiomatic in Go. For mocking, define interfaces for dependencies and use hand-written fakes or gomock and mockery for generated mocks."},
			},
		},
		{
			ID: "s10",
			Turns: []Turn{
				{Role: "user", Content: "What is context.Context in Go and why is it important?"},
				{Role: "assistant", Content: "Context carries deadlines, cancellation signals, and request-scoped values across API boundaries and goroutines. It is the first parameter by convention. Use context.WithTimeout, WithCancel, and WithDeadline to control lifecycle."},
				{Role: "user", Content: "How do I pass values and detect cancellation?"},
				{Role: "assistant", Content: "Use context.WithValue to attach key-value pairs with unexported key types. When a context is cancelled, its Done channel is closed and all child contexts are also cancelled. Functions should select on ctx.Done() and return ctx.Err() which is context.Canceled or context.DeadlineExceeded."},
			},
		},
	}

	queries := []Query{
		// Single-session queries (look up something from one specific session)
		{
			ID:               "q01-single-goroutine",
			SessionID:        "s01",
			Question:         "What scheduling algorithm does the Go runtime use for goroutines?",
			GoldAnswer:       "work-stealing",
			Category:         "single-session",
			RequiredSessions: []string{"s01"},
		},
		{
			ID:               "q02-single-gc",
			SessionID:        "s03",
			Question:         "What write barrier approach does Go use since version 1.8?",
			GoldAnswer:       "hybrid write barrier combining Dijkstra and Yuasa",
			Category:         "single-session",
			RequiredSessions: []string{"s03"},
		},
		{
			ID:               "q03-single-select",
			SessionID:        "s04",
			Question:         "How does the select statement choose when multiple channel operations are ready?",
			GoldAnswer:       "chosen at random",
			Category:         "single-session",
			RequiredSessions: []string{"s04"},
		},
		{
			ID:               "q04-single-generics",
			SessionID:        "s08",
			Question:         "What Go version introduced type parameters?",
			GoldAnswer:       "Go 1.18",
			Category:         "single-session",
			RequiredSessions: []string{"s08"},
		},

		// Multi-session queries (require info from multiple sessions)
		{
			ID:               "q05-multi-concurrency",
			SessionID:        "s01",
			Question:         "Describe the concurrency primitives: goroutine scheduling and channel select behavior.",
			GoldAnswer:       "work-stealing",
			Category:         "multi-session",
			RequiredSessions: []string{"s01", "s04"},
		},
		{
			ID:               "q06-multi-error",
			SessionID:        "s05",
			Question:         "How are errors propagated in Go, including wrapping with fmt.Errorf and context cancellation errors?",
			GoldAnswer:       "percent-w verb",
			Category:         "multi-session",
			RequiredSessions: []string{"s05", "s10"},
		},
		{
			ID:               "q07-multi-testing",
			SessionID:        "s09",
			Question:         "How do Go generics and table-driven tests complement each other?",
			GoldAnswer:       "table-driven tests",
			Category:         "multi-session",
			RequiredSessions: []string{"s08", "s09"},
		},

		// Temporal queries (require understanding when something was said)
		{
			ID:               "q08-temporal-gc",
			SessionID:        "s03",
			Question:         "What GC tuning mechanism was added in Go 1.19?",
			GoldAnswer:       "debug.SetMemoryLimit",
			Category:         "temporal",
			RequiredSessions: []string{"s03"},
		},
		{
			ID:               "q09-temporal-modules",
			SessionID:        "s07",
			Question:         "When did Go modules become the default build mode?",
			GoldAnswer:       "Go 1.16",
			Category:         "temporal",
			RequiredSessions: []string{"s07"},
		},
		{
			ID:               "q10-temporal-routing",
			SessionID:        "s06",
			Question:         "What routing improvement came in Go 1.22?",
			GoldAnswer:       "method-based patterns",
			Category:         "temporal",
			RequiredSessions: []string{"s06"},
		},
	}

	return &Dataset{
		Sessions: sessions,
		Queries:  queries,
	}
}
