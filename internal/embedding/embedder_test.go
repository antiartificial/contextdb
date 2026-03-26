package embedding

import (
	"context"
	"testing"

	"github.com/matryer/is"
)

// Mock implements Embedder for testing.
type Mock struct {
	dims    int
	calls   int
	vectors map[string][]float32 // text → vector
}

func NewMock(dims int) *Mock {
	return &Mock{dims: dims, vectors: make(map[string][]float32)}
}

func (m *Mock) Set(text string, vec []float32) {
	m.vectors[text] = vec
}

func (m *Mock) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	m.calls++
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := m.vectors[t]; ok {
			out[i] = v
		} else {
			// deterministic default: fill with hash-like values
			v := make([]float32, m.dims)
			for j := range v {
				v[j] = float32(len(t)+j) * 0.01
			}
			out[i] = v
		}
	}
	return out, nil
}

func (m *Mock) Dimensions() int { return m.dims }

func TestMockEmbedder(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	mock := NewMock(3)
	mock.Set("hello", []float32{0.1, 0.2, 0.3})

	vecs, err := mock.Embed(ctx, []string{"hello", "world"})
	is.NoErr(err)
	is.Equal(len(vecs), 2)
	is.Equal(vecs[0], []float32{0.1, 0.2, 0.3})
	is.Equal(len(vecs[1]), 3)
	is.Equal(mock.Dimensions(), 3)
}

func TestCachedEmbedder(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	mock := NewMock(3)
	cached := NewCached(mock, 100)

	// First call: should hit inner
	vecs1, err := cached.Embed(ctx, []string{"hello", "world"})
	is.NoErr(err)
	is.Equal(len(vecs1), 2)
	is.Equal(mock.calls, 1)

	// Second call with same texts: should be cached
	vecs2, err := cached.Embed(ctx, []string{"hello", "world"})
	is.NoErr(err)
	is.Equal(len(vecs2), 2)
	is.Equal(mock.calls, 1) // no new call

	// Third call with mixed texts: only uncached text hits inner
	vecs3, err := cached.Embed(ctx, []string{"hello", "new"})
	is.NoErr(err)
	is.Equal(len(vecs3), 2)
	is.Equal(mock.calls, 2) // one new call for "new"

	// Verify dimensions passthrough
	is.Equal(cached.Dimensions(), 3)
}

func TestCachedEviction(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	mock := NewMock(2)
	cached := NewCached(mock, 2) // tiny cache

	// Fill cache
	_, err := cached.Embed(ctx, []string{"a", "b"})
	is.NoErr(err)
	is.Equal(mock.calls, 1)

	// "a" and "b" cached; adding "c" should evict "a"
	_, err = cached.Embed(ctx, []string{"c"})
	is.NoErr(err)
	is.Equal(mock.calls, 2)

	// "a" was evicted, should need re-embed
	_, err = cached.Embed(ctx, []string{"a"})
	is.NoErr(err)
	is.Equal(mock.calls, 3)

	// "b" and "c" might be evicted now, but "a" should be cached
	_, err = cached.Embed(ctx, []string{"a"})
	is.NoErr(err)
	is.Equal(mock.calls, 3) // still cached
}

func TestEmptyInput(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	mock := NewMock(3)
	cached := NewCached(mock, 100)

	vecs, err := mock.Embed(ctx, nil)
	is.NoErr(err)
	is.True(vecs == nil)

	vecs, err = cached.Embed(ctx, nil)
	is.NoErr(err)
	is.True(vecs == nil)

	vecs, err = mock.Embed(ctx, []string{})
	is.NoErr(err)
	is.Equal(len(vecs), 0)
}
