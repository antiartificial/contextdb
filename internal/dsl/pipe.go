package dsl

import (
	"strconv"
)

// ParsePipe parses a pipe-syntax query string into a Query AST.
//
//	search "text" | where ... | weight ... | top N | expand ... | rerank
func ParsePipe(input string) (*Query, error) {
	tokens := NewLexer(input).All()
	p := &pipeParser{parser: newParser(tokens)}
	return p.parse()
}

type pipeParser struct {
	*parser
}

func (p *pipeParser) parse() (*Query, error) {
	q := &Query{
		Weights: DefaultWeights(),
	}

	// First stage must be "search"
	if err := p.parseSearchStage(q); err != nil {
		return nil, err
	}

	// Remaining stages separated by |
	for p.cur().Type == TokPipe {
		p.advance() // consume |
		if err := p.parseStage(q); err != nil {
			return nil, err
		}
	}

	if p.cur().Type != TokEOF {
		return nil, errUnexpected(p.cur())
	}
	return q, nil
}

func (p *pipeParser) parseSearchStage(q *Query) error {
	if _, err := p.expect(TokSearch); err != nil {
		return err
	}
	tok, err := p.expect(TokString)
	if err != nil {
		return err
	}
	q.SearchText = tok.Lit
	return nil
}

func (p *pipeParser) parseStage(q *Query) error {
	tok := p.cur()
	switch tok.Type {
	case TokWhere:
		return p.parseWhereStage(q)
	case TokWeight:
		return p.parseWeightStage(q)
	case TokTop:
		return p.parseTopStage(q)
	case TokExpand:
		return p.parseExpandStage(q)
	case TokRerank:
		return p.parseRerankStage(q)
	case TokIn:
		return p.parseNamespaceStage(q)
	case TokAs:
		return p.parseAsOfStage(q)
	case TokKnown:
		return p.parseKnownAtStage(q)
	case TokReturn:
		return p.parseReturnStage(q)
	default:
		return errUnexpected(tok)
	}
}

func (p *pipeParser) parseWhereStage(q *Query) error {
	p.advance() // consume "where"

	pred, err := p.parsePredicate()
	if err != nil {
		return err
	}
	q.Predicates = append(q.Predicates, pred)

	for p.cur().Type == TokAnd {
		p.advance()
		pred, err = p.parsePredicate()
		if err != nil {
			return err
		}
		q.Predicates = append(q.Predicates, pred)
	}
	return nil
}

func (p *pipeParser) parsePredicate() (Predicate, error) {
	fieldTok := p.cur()
	if fieldTok.Type != TokIdent && !p.isFieldKeyword(fieldTok.Type) {
		return Predicate{}, errAt(fieldTok.Pos, "expected field name")
	}
	field := fieldTok.Lit
	p.advance()

	// BETWEEN special case
	if p.cur().Type == TokBetween {
		p.advance()
		low, err := p.parseValue()
		if err != nil {
			return Predicate{}, err
		}
		if _, err := p.expect(TokAnd); err != nil {
			return Predicate{}, err
		}
		high, err := p.parseValue()
		if err != nil {
			return Predicate{}, err
		}
		return Predicate{Field: field, Op: OpBetween, Value: low, UpperBound: high}, nil
	}

	op, err := p.parseComparator()
	if err != nil {
		return Predicate{}, err
	}
	val, err := p.parseValue()
	if err != nil {
		return Predicate{}, err
	}
	return Predicate{Field: field, Op: op, Value: val}, nil
}

// isFieldKeyword returns true for tokens that are valid as field names
// even though they're also keywords (e.g. "label", "age").
func (p *pipeParser) isFieldKeyword(t TokenType) bool {
	// field names that happen to also be keywords
	return false
}

func (p *pipeParser) parseWeightStage(q *Query) error {
	p.advance() // consume "weight"

	if err := p.parseWeightKV(q); err != nil {
		return err
	}
	for p.cur().Type == TokComma {
		p.advance()
		if err := p.parseWeightKV(q); err != nil {
			return err
		}
	}
	return nil
}

func (p *pipeParser) parseWeightKV(q *Query) error {
	dimTok := p.cur()
	if dimTok.Type != TokIdent {
		return errAt(dimTok.Pos, "expected weight dimension")
	}
	dim := dimTok.Lit
	p.advance()

	if _, err := p.expect(TokColon); err != nil {
		return err
	}

	var weight float64
	valTok := p.cur()

	if valTok.Type == TokIdent {
		// preset name
		w, ok := ResolvePreset(valTok.Lit)
		if !ok {
			return errAt(valTok.Pos, "unknown weight preset: "+valTok.Lit)
		}
		weight = w
		p.advance()
	} else if valTok.Type == TokNumber || valTok.Type == TokInteger {
		w, err := strconv.ParseFloat(valTok.Lit, 64)
		if err != nil {
			return errAt(valTok.Pos, "invalid weight value")
		}
		weight = w
		p.advance()
	} else {
		return errAt(valTok.Pos, "expected weight value or preset")
	}

	switch dim {
	case "similarity":
		q.Weights.Similarity = weight
	case "confidence":
		q.Weights.Confidence = weight
	case "recency":
		q.Weights.Recency = weight
	case "utility":
		q.Weights.Utility = weight
	default:
		return errAt(dimTok.Pos, "unknown weight dimension: "+dim)
	}
	return nil
}

func (p *pipeParser) parseTopStage(q *Query) error {
	p.advance() // consume "top"
	tok, err := p.expect(TokInteger)
	if err != nil {
		return err
	}
	n, _ := strconv.Atoi(tok.Lit)
	q.Limit = n
	return nil
}

func (p *pipeParser) parseExpandStage(q *Query) error {
	p.advance() // consume "expand"

	edgeTok := p.cur()
	if edgeTok.Type != TokIdent {
		return errAt(edgeTok.Pos, "expected edge type")
	}
	p.advance()

	spec := EdgeSpec{Type: edgeTok.Lit}

	// optional "depth N"
	if p.cur().Type == TokDepth {
		p.advance()
		depthTok, err := p.expect(TokInteger)
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(depthTok.Lit)
		spec.MaxDepth = n
	}

	if q.Graph == nil {
		q.Graph = &GraphOpts{}
	}
	q.Graph.Edges = append(q.Graph.Edges, spec)
	return nil
}

func (p *pipeParser) parseRerankStage(q *Query) error {
	p.advance() // consume "rerank"
	q.Rerank = true
	return nil
}

func (p *pipeParser) parseNamespaceStage(q *Query) error {
	p.advance() // consume "in"

	nsTok := p.cur()
	if nsTok.Type != TokIdent {
		return errAt(nsTok.Pos, "expected namespace identifier")
	}
	q.Namespace = nsTok.Lit
	p.advance()

	// optional "mode <name>"
	if p.cur().Type == TokMode {
		p.advance()
		modeTok := p.cur()
		if modeTok.Type != TokIdent {
			return errAt(modeTok.Pos, "expected mode name")
		}
		q.Mode = modeTok.Lit
		p.advance()
	}
	return nil
}

func (p *pipeParser) parseAsOfStage(q *Query) error {
	p.advance() // consume "as"
	// expect "_of" as a separate token or joined
	if p.cur().Type == TokOf {
		p.advance()
	}
	t, err := p.parseDatetime()
	if err != nil {
		return err
	}
	q.ValidAt = &t
	return nil
}

func (p *pipeParser) parseKnownAtStage(q *Query) error {
	p.advance() // consume "known"
	if p.cur().Type == TokAt {
		p.advance()
	}
	t, err := p.parseDatetime()
	if err != nil {
		return err
	}
	q.KnownAt = &t
	return nil
}

func (p *pipeParser) parseReturnStage(q *Query) error {
	p.advance() // consume "return"

	for {
		tok := p.cur()
		if tok.Type != TokIdent {
			return errAt(tok.Pos, "expected field name")
		}
		q.Return = append(q.Return, tok.Lit)
		p.advance()

		if p.cur().Type != TokComma {
			break
		}
		p.advance() // consume comma
	}
	return nil
}
