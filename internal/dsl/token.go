package dsl

import "fmt"

// Token represents a lexical token.
type Token struct {
	Type TokenType
	Lit  string
	Pos  Position
}

// Position tracks location in source for error reporting.
type Position struct {
	Line   int
	Column int
	Offset int
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// TokenType classifies tokens.
type TokenType int

const (
	// Special
	TokEOF TokenType = iota
	TokIllegal

	// Literals
	TokIdent   // field names, dotted paths
	TokString  // "quoted"
	TokNumber  // 42, 3.14, -1
	TokInteger // subset: whole numbers only (for top/limit/depth)

	// Punctuation
	TokPipe    // |
	TokComma   // ,
	TokColon   // :
	TokLParen  // (
	TokRParen  // )
	TokDot     // .

	// Comparison operators
	TokEq   // =
	TokNeq  // !=
	TokGt   // >
	TokGte  // >=
	TokLt   // <
	TokLte  // <=

	// Keywords — pipe syntax (lowercase by convention)
	TokSearch
	TokWhere
	TokWeight
	TokTop
	TokExpand
	TokRerank
	TokIn
	TokAs
	TokOf
	TokKnown
	TokAt
	TokReturn
	TokAnd
	TokOr
	TokNot
	TokBetween
	TokIs
	TokNull
	TokDepth
	TokMode
	TokAgo
	TokNow
	TokLast
	TokYesterday
	TokToday
	TokLike
	TokWith
	TokNamespace

	// CQL keywords
	TokFind
	TokFollow
	TokExclude
	TokLimit
	TokSources
)

// keywords maps lowercase strings to their token types.
var keywords = map[string]TokenType{
	"search":    TokSearch,
	"where":     TokWhere,
	"weight":    TokWeight,
	"top":       TokTop,
	"expand":    TokExpand,
	"rerank":    TokRerank,
	"in":        TokIn,
	"as":        TokAs,
	"of":        TokOf,
	"known":     TokKnown,
	"at":        TokAt,
	"return":    TokReturn,
	"and":       TokAnd,
	"or":        TokOr,
	"not":       TokNot,
	"between":   TokBetween,
	"is":        TokIs,
	"null":      TokNull,
	"depth":     TokDepth,
	"mode":      TokMode,
	"ago":       TokAgo,
	"now":       TokNow,
	"last":      TokLast,
	"yesterday": TokYesterday,
	"today":     TokToday,
	"like":      TokLike,
	"as_of":     TokAs, // underscore variant for pipe syntax
	"known_at":  TokKnown, // underscore variant for pipe syntax
	"with":      TokWith,
	"namespace": TokNamespace,
	"find":      TokFind,
	"follow":    TokFollow,
	"exclude":   TokExclude,
	"limit":     TokLimit,
	"sources":   TokSources,
}

func (t TokenType) String() string {
	switch t {
	case TokEOF:
		return "EOF"
	case TokIllegal:
		return "ILLEGAL"
	case TokIdent:
		return "IDENT"
	case TokString:
		return "STRING"
	case TokNumber:
		return "NUMBER"
	case TokInteger:
		return "INTEGER"
	case TokPipe:
		return "|"
	case TokComma:
		return ","
	case TokColon:
		return ":"
	case TokLParen:
		return "("
	case TokRParen:
		return ")"
	case TokDot:
		return "."
	case TokEq:
		return "="
	case TokNeq:
		return "!="
	case TokGt:
		return ">"
	case TokGte:
		return ">="
	case TokLt:
		return "<"
	case TokLte:
		return "<="
	default:
		// keywords
		for k, v := range keywords {
			if v == t {
				return k
			}
		}
		return fmt.Sprintf("TokenType(%d)", int(t))
	}
}
