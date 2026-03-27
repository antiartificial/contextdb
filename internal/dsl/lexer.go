package dsl

import (
	"strings"
	"unicode"
)

// Lexer tokenises a DSL input string.
type Lexer struct {
	input []rune
	pos   int
	line  int
	col   int
}

// NewLexer creates a lexer for the given input.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: []rune(input),
		line:  1,
		col:   1,
	}
}

func (l *Lexer) curPos() Position {
	return Position{Line: l.line, Column: l.col, Offset: l.pos}
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	ch := l.input[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(l.input[l.pos]) {
		l.advance()
	}
}

// All tokenises the entire input and returns the token slice.
func (l *Lexer) All() []Token {
	var tokens []Token
	for {
		tok := l.Next()
		tokens = append(tokens, tok)
		if tok.Type == TokEOF {
			break
		}
	}
	return tokens
}

// Next returns the next token.
func (l *Lexer) Next() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokEOF, Pos: l.curPos()}
	}

	pos := l.curPos()
	ch := l.peek()

	switch {
	case ch == '"':
		return l.lexString(pos)
	case ch == '-' && l.pos+1 < len(l.input) && isDigit(l.input[l.pos+1]):
		return l.lexNumber(pos)
	case isDigit(ch):
		return l.lexNumber(pos)
	case isIdentStart(ch):
		return l.lexIdentOrKeyword(pos)
	case ch == '|':
		l.advance()
		return Token{Type: TokPipe, Lit: "|", Pos: pos}
	case ch == ',':
		l.advance()
		return Token{Type: TokComma, Lit: ",", Pos: pos}
	case ch == ':':
		l.advance()
		return Token{Type: TokColon, Lit: ":", Pos: pos}
	case ch == '(':
		l.advance()
		return Token{Type: TokLParen, Lit: "(", Pos: pos}
	case ch == ')':
		l.advance()
		return Token{Type: TokRParen, Lit: ")", Pos: pos}
	case ch == '=':
		l.advance()
		return Token{Type: TokEq, Lit: "=", Pos: pos}
	case ch == '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Type: TokNeq, Lit: "!=", Pos: pos}
		}
		return Token{Type: TokIllegal, Lit: "!", Pos: pos}
	case ch == '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Type: TokGte, Lit: ">=", Pos: pos}
		}
		return Token{Type: TokGt, Lit: ">", Pos: pos}
	case ch == '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{Type: TokLte, Lit: "<=", Pos: pos}
		}
		return Token{Type: TokLt, Lit: "<", Pos: pos}
	default:
		l.advance()
		return Token{Type: TokIllegal, Lit: string(ch), Pos: pos}
	}
}

func (l *Lexer) lexString(pos Position) Token {
	l.advance() // consume opening "
	var buf []rune
	for l.pos < len(l.input) {
		ch := l.advance()
		if ch == '"' {
			return Token{Type: TokString, Lit: string(buf), Pos: pos}
		}
		if ch == '\\' && l.pos < len(l.input) {
			next := l.advance()
			switch next {
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			case 'n':
				buf = append(buf, '\n')
			default:
				buf = append(buf, '\\', next)
			}
			continue
		}
		buf = append(buf, ch)
	}
	// unterminated string
	return Token{Type: TokIllegal, Lit: `"` + string(buf), Pos: pos}
}

func (l *Lexer) lexNumber(pos Position) Token {
	var buf []rune
	if l.peek() == '-' {
		buf = append(buf, '-')
		l.advance()
	}
	isFloat := false
	for l.pos < len(l.input) {
		ch := l.peek()
		if isDigit(ch) {
			buf = append(buf, ch)
			l.advance()
		} else if ch == '.' && !isFloat {
			// look ahead: only treat as decimal if followed by digit
			if l.pos+1 < len(l.input) && isDigit(l.input[l.pos+1]) {
				isFloat = true
				buf = append(buf, ch)
				l.advance()
			} else {
				break
			}
		} else {
			break
		}
	}
	lit := string(buf)
	if isFloat {
		return Token{Type: TokNumber, Lit: lit, Pos: pos}
	}
	return Token{Type: TokInteger, Lit: lit, Pos: pos}
}

func (l *Lexer) lexIdentOrKeyword(pos Position) Token {
	var buf []rune
	for l.pos < len(l.input) {
		ch := l.peek()
		if isIdentCont(ch) {
			buf = append(buf, ch)
			l.advance()
		} else if ch == '.' {
			// dotted path: source.credibility
			buf = append(buf, ch)
			l.advance()
		} else {
			break
		}
	}
	lit := string(buf)
	// Strip trailing dot if present (e.g. path followed by punctuation)
	if strings.HasSuffix(lit, ".") {
		lit = lit[:len(lit)-1]
		l.pos--
		l.col--
	}

	lower := strings.ToLower(lit)
	if tokType, ok := keywords[lower]; ok {
		return Token{Type: tokType, Lit: lit, Pos: pos}
	}
	return Token{Type: TokIdent, Lit: lit, Pos: pos}
}

func isDigit(ch rune) bool     { return ch >= '0' && ch <= '9' }
func isIdentStart(ch rune) bool { return unicode.IsLetter(ch) || ch == '_' }
func isIdentCont(ch rune) bool  { return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' }
