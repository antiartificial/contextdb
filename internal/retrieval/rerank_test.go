package retrieval

import (
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
)

func TestParseRanking(t *testing.T) {
	is := is.New(t)

	// Normal case
	r := parseRanking("3,1,2", 3)
	is.Equal(r, []int{2, 0, 1})

	// With extra whitespace
	r = parseRanking(" 2 , 1 , 3 ", 3)
	is.Equal(r, []int{1, 0, 2})

	// Incomplete ranking — missing indices appended
	r = parseRanking("2", 3)
	is.Equal(len(r), 3)
	is.Equal(r[0], 1) // doc 2 → index 1

	// Out-of-range indices ignored
	r = parseRanking("1,5,2", 3)
	is.Equal(len(r), 3)
	is.Equal(r[0], 0) // doc 1
	is.Equal(r[1], 1) // doc 2

	// Empty input
	r = parseRanking("", 3)
	is.Equal(len(r), 3)

	// Duplicate indices
	r = parseRanking("1,1,2,3", 3)
	is.Equal(len(r), 3)
}

func TestFallbackRerank(t *testing.T) {
	is := is.New(t)

	candidates := make([]core.Node, 5)
	results := fallbackRerank(candidates, 3)
	is.Equal(len(results), 3)
	is.True(results[0].Score > results[1].Score)
	is.True(results[1].Score > results[2].Score)
}
