package extract

import "context"

// StaticExtractor returns a fixed result for every extraction request.
// Useful for testing.
type StaticExtractor struct {
	Result *ExtractionResult
	Err    error
}

func (e *StaticExtractor) Extract(_ context.Context, _ ExtractionRequest) (*ExtractionResult, error) {
	return e.Result, e.Err
}
