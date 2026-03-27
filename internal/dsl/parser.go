package dsl

import (
	"strconv"
	"time"
)

// parser is the shared base for pipe and CQL parsers.
type parser struct {
	tokens []Token
	pos    int
	now    func() time.Time // injectable clock for testing
}

func newParser(tokens []Token) *parser {
	return &parser{tokens: tokens, now: time.Now}
}

func (p *parser) cur() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() Token {
	tok := p.cur()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *parser) expect(typ TokenType) (Token, error) {
	tok := p.cur()
	if tok.Type != typ {
		return tok, &ParseError{
			Pos:     tok.Pos,
			Message: "expected " + typ.String() + ", got " + tok.Type.String() + " " + strconv.Quote(tok.Lit),
		}
	}
	p.advance()
	return tok, nil
}

// parseValue parses a scalar value (string, number, relative time, now, date).
func (p *parser) parseValue() (Value, error) {
	tok := p.cur()

	switch tok.Type {
	case TokString:
		p.advance()
		return Value{Type: ValString, Str: tok.Lit}, nil

	case TokNow:
		p.advance()
		return Value{Type: ValNow, Time: p.now()}, nil

	case TokYesterday:
		p.advance()
		t := p.now().Add(-24 * time.Hour)
		return Value{Type: ValTime, Time: t}, nil

	case TokToday:
		p.advance()
		return Value{Type: ValTime, Time: p.now()}, nil

	case TokNumber, TokInteger:
		// Could be plain number, or start of relative time (e.g. "7d ago")
		numTok := p.advance()
		num, err := strconv.ParseFloat(numTok.Lit, 64)
		if err != nil {
			return Value{}, errAt(numTok.Pos, "invalid number: "+numTok.Lit)
		}

		// Check for time unit suffix (relative time)
		if p.cur().Type == TokIdent || p.isTimeUnit() {
			dur, err := p.parseTimeUnitAndAgo(int(num))
			if err != nil {
				return Value{}, err
			}
			t := p.now().Add(-dur)
			return Value{Type: ValTime, Time: t}, nil
		}

		return Value{Type: ValNumber, Num: num}, nil

	case TokLast:
		// "last week", "last month", etc.
		p.advance()
		dur, err := p.parseTimeUnitOnly()
		if err != nil {
			return Value{}, err
		}
		t := p.now().Add(-dur)
		return Value{Type: ValTime, Time: t}, nil

	default:
		// Try as identifier that might be a date (2024-06-01) — but dates
		// are lexed as integers and dashes. Let's handle ISO date.
		if tok.Type == TokIdent {
			// Could be an ISO date-like string without quotes
			t, err := time.Parse("2006-01-02", tok.Lit)
			if err == nil {
				p.advance()
				// Check for time component: T...
				return Value{Type: ValTime, Time: t}, nil
			}
		}
		return Value{}, errUnexpected(tok)
	}
}

// isTimeUnit checks if the current token looks like a time unit.
func (p *parser) isTimeUnit() bool {
	tok := p.cur()
	if tok.Type != TokIdent {
		return false
	}
	return isTimeUnitStr(tok.Lit)
}

func isTimeUnitStr(s string) bool {
	switch s {
	case "s", "m", "h", "d", "w", "mo", "y",
		"second", "seconds", "minute", "minutes",
		"hour", "hours", "day", "days",
		"week", "weeks", "month", "months",
		"year", "years":
		return true
	}
	return false
}

func (p *parser) parseTimeUnitAndAgo(n int) (time.Duration, error) {
	dur, err := p.parseTimeUnitOnly()
	if err != nil {
		return 0, err
	}
	dur = dur * time.Duration(n)

	// Consume optional "ago"
	if p.cur().Type == TokAgo {
		p.advance()
	}
	return dur, nil
}

func (p *parser) parseTimeUnitOnly() (time.Duration, error) {
	tok := p.cur()
	if tok.Type != TokIdent {
		return 0, errAt(tok.Pos, "expected time unit")
	}
	p.advance()
	return parseTimeUnit(tok.Lit, tok.Pos)
}

func parseTimeUnit(s string, pos Position) (time.Duration, error) {
	switch s {
	case "s", "second", "seconds":
		return time.Second, nil
	case "m", "minute", "minutes":
		return time.Minute, nil
	case "h", "hour", "hours":
		return time.Hour, nil
	case "d", "day", "days":
		return 24 * time.Hour, nil
	case "w", "week", "weeks":
		return 7 * 24 * time.Hour, nil
	case "mo", "month", "months":
		return 30 * 24 * time.Hour, nil // approximate
	case "y", "year", "years":
		return 365 * 24 * time.Hour, nil // approximate
	default:
		return 0, errAt(pos, "unknown time unit: "+s)
	}
}

// parseDatetime parses an ISO date, relative time, or "now".
func (p *parser) parseDatetime() (time.Time, error) {
	tok := p.cur()

	if tok.Type == TokNow {
		p.advance()
		return p.now(), nil
	}

	if tok.Type == TokYesterday {
		p.advance()
		return p.now().Add(-24 * time.Hour), nil
	}

	if tok.Type == TokToday {
		p.advance()
		return p.now(), nil
	}

	// relative time: "7d ago"
	if tok.Type == TokInteger || tok.Type == TokNumber {
		n, _ := strconv.Atoi(tok.Lit)
		p.advance()
		dur, err := p.parseTimeUnitAndAgo(n)
		if err != nil {
			return time.Time{}, err
		}
		return p.now().Add(-dur), nil
	}

	if tok.Type == TokLast {
		p.advance()
		dur, err := p.parseTimeUnitOnly()
		if err != nil {
			return time.Time{}, err
		}
		return p.now().Add(-dur), nil
	}

	// ISO date as identifier or string
	var dateStr string
	if tok.Type == TokString {
		dateStr = tok.Lit
		p.advance()
	} else if tok.Type == TokIdent {
		dateStr = tok.Lit
		p.advance()
	} else {
		return time.Time{}, errAt(tok.Pos, "expected datetime value")
	}

	// Try full ISO datetime, then date-only
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, dateStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errAt(tok.Pos, "invalid date: "+dateStr)
}

// parseComparator parses a comparison operator token.
func (p *parser) parseComparator() (CompareOp, error) {
	tok := p.cur()
	switch tok.Type {
	case TokGt:
		p.advance()
		return OpGt, nil
	case TokGte:
		p.advance()
		return OpGte, nil
	case TokLt:
		p.advance()
		return OpLt, nil
	case TokLte:
		p.advance()
		return OpLte, nil
	case TokEq:
		p.advance()
		return OpEq, nil
	case TokNeq:
		p.advance()
		return OpNeq, nil
	case TokLike:
		p.advance()
		return OpLike, nil
	case TokIs:
		p.advance()
		return OpEq, nil // "is" treated as "=" for pipe syntax
	default:
		return 0, errAt(tok.Pos, "expected comparison operator")
	}
}
