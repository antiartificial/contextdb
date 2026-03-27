package dsl

import (
	"strconv"
)

// ParseCQL parses a CQL (Contextual Query Language) string into a Query AST.
//
//	FIND "text" [IN NAMESPACE ns] [AS OF t] [WHERE ...] [FOLLOW ...]
//	[WEIGHT ...] [LIMIT n] [RERANK] [RETURN ...]
func ParseCQL(input string) (*Query, error) {
	tokens := NewLexer(input).All()
	p := &cqlParser{parser: newParser(tokens)}
	return p.parse()
}

type cqlParser struct {
	*parser
}

func (p *cqlParser) parse() (*Query, error) {
	q := &Query{
		Weights: DefaultWeights(),
	}

	// FIND clause — required
	if err := p.parseFindClause(q); err != nil {
		return nil, err
	}

	// Remaining clauses are optional, order-independent within reason
	for p.cur().Type != TokEOF {
		switch p.cur().Type {
		case TokIn:
			if err := p.parseInClause(q); err != nil {
				return nil, err
			}
		case TokAs:
			if err := p.parseAsOfClause(q); err != nil {
				return nil, err
			}
		case TokKnown:
			if err := p.parseKnownAtClause(q); err != nil {
				return nil, err
			}
		case TokWhere:
			if err := p.parseWhereClause(q); err != nil {
				return nil, err
			}
		case TokFollow:
			if err := p.parseFollowClause(q); err != nil {
				return nil, err
			}
		case TokExclude:
			if err := p.parseExcludeClause(q); err != nil {
				return nil, err
			}
		case TokWeight:
			if err := p.parseWeightClause(q); err != nil {
				return nil, err
			}
		case TokLimit:
			if err := p.parseLimitClause(q); err != nil {
				return nil, err
			}
		case TokRerank:
			if err := p.parseRerankClause(q); err != nil {
				return nil, err
			}
		case TokReturn:
			if err := p.parseReturnClause(q); err != nil {
				return nil, err
			}
		default:
			return nil, errUnexpected(p.cur())
		}
	}

	return q, nil
}

func (p *cqlParser) parseFindClause(q *Query) error {
	if _, err := p.expect(TokFind); err != nil {
		return err
	}
	tok, err := p.expect(TokString)
	if err != nil {
		return err
	}
	q.SearchText = tok.Lit
	return nil
}

func (p *cqlParser) parseInClause(q *Query) error {
	p.advance() // consume IN

	// optional NAMESPACE keyword
	if p.cur().Type == TokNamespace {
		p.advance()
	}

	nsTok := p.cur()
	if nsTok.Type != TokIdent {
		return errAt(nsTok.Pos, "expected namespace identifier")
	}
	q.Namespace = nsTok.Lit
	p.advance()

	// optional MODE
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

func (p *cqlParser) parseAsOfClause(q *Query) error {
	p.advance() // consume AS
	if _, err := p.expect(TokOf); err != nil {
		return err
	}
	t, err := p.parseDatetime()
	if err != nil {
		return err
	}
	q.ValidAt = &t
	return nil
}

func (p *cqlParser) parseKnownAtClause(q *Query) error {
	p.advance() // consume KNOWN
	if _, err := p.expect(TokAt); err != nil {
		return err
	}
	t, err := p.parseDatetime()
	if err != nil {
		return err
	}
	q.KnownAt = &t
	return nil
}

func (p *cqlParser) parseWhereClause(q *Query) error {
	p.advance() // consume WHERE
	return p.parseBoolExpr(q)
}

func (p *cqlParser) parseBoolExpr(q *Query) error {
	if err := p.parseBoolTerm(q); err != nil {
		return err
	}

	for p.cur().Type == TokAnd || p.cur().Type == TokOr {
		// For now, we flatten AND/OR into the predicate list.
		// OR support would need a tree structure — keep it flat with AND semantics.
		p.advance()
		if err := p.parseBoolTerm(q); err != nil {
			return err
		}
	}
	return nil
}

func (p *cqlParser) parseBoolTerm(q *Query) error {
	// NOT
	if p.cur().Type == TokNot {
		p.advance()
		return p.parseBoolTerm(q)
	}

	// Parenthesized expression
	if p.cur().Type == TokLParen {
		p.advance()
		if err := p.parseBoolExpr(q); err != nil {
			return err
		}
		if _, err := p.expect(TokRParen); err != nil {
			return err
		}
		return nil
	}

	// Comparison
	pred, err := p.parseComparison()
	if err != nil {
		return err
	}
	q.Predicates = append(q.Predicates, pred)
	return nil
}

func (p *cqlParser) parseComparison() (Predicate, error) {
	fieldTok := p.cur()
	if fieldTok.Type != TokIdent {
		return Predicate{}, errAt(fieldTok.Pos, "expected field path")
	}
	field := fieldTok.Lit
	p.advance()

	// BETWEEN
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

	// IN (list)
	if p.cur().Type == TokIn {
		p.advance()
		if _, err := p.expect(TokLParen); err != nil {
			return Predicate{}, err
		}
		var items []string
		for {
			valTok, err := p.expect(TokString)
			if err != nil {
				return Predicate{}, err
			}
			items = append(items, valTok.Lit)
			if p.cur().Type != TokComma {
				break
			}
			p.advance()
		}
		if _, err := p.expect(TokRParen); err != nil {
			return Predicate{}, err
		}
		return Predicate{
			Field: field,
			Op:    OpIn,
			Value: Value{Type: ValStringList, Strings: items},
		}, nil
	}

	// IS NULL / IS NOT NULL
	if p.cur().Type == TokIs {
		p.advance()
		if p.cur().Type == TokNot {
			p.advance()
			if _, err := p.expect(TokNull); err != nil {
				return Predicate{}, err
			}
			return Predicate{Field: field, Op: OpIsNotNull}, nil
		}
		if _, err := p.expect(TokNull); err != nil {
			return Predicate{}, err
		}
		return Predicate{Field: field, Op: OpIsNull}, nil
	}

	// Standard comparison
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

func (p *cqlParser) parseFollowClause(q *Query) error {
	p.advance() // consume FOLLOW

	spec, err := p.parseEdgeSpec()
	if err != nil {
		return err
	}
	if q.Graph == nil {
		q.Graph = &GraphOpts{}
	}
	q.Graph.Edges = append(q.Graph.Edges, spec)

	for p.cur().Type == TokComma {
		p.advance()
		spec, err = p.parseEdgeSpec()
		if err != nil {
			return err
		}
		q.Graph.Edges = append(q.Graph.Edges, spec)
	}
	return nil
}

func (p *cqlParser) parseEdgeSpec() (EdgeSpec, error) {
	tok := p.cur()
	if tok.Type != TokIdent {
		return EdgeSpec{}, errAt(tok.Pos, "expected edge type")
	}
	spec := EdgeSpec{Type: tok.Lit}
	p.advance()

	// optional DEPTH N
	if p.cur().Type == TokDepth {
		p.advance()
		depthTok, err := p.expect(TokInteger)
		if err != nil {
			return EdgeSpec{}, err
		}
		n, _ := strconv.Atoi(depthTok.Lit)
		spec.MaxDepth = n
	}

	// optional WHERE on edge — skip for now per spec
	return spec, nil
}

func (p *cqlParser) parseWeightClause(q *Query) error {
	p.advance() // consume WEIGHT

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

func (p *cqlParser) parseWeightKV(q *Query) error {
	dimTok := p.cur()
	if dimTok.Type != TokIdent {
		return errAt(dimTok.Pos, "expected weight dimension")
	}
	dim := dimTok.Lit
	p.advance()

	if _, err := p.expect(TokEq); err != nil {
		return err
	}

	var weight float64
	valTok := p.cur()

	if valTok.Type == TokIdent {
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

func (p *cqlParser) parseLimitClause(q *Query) error {
	p.advance() // consume LIMIT
	tok, err := p.expect(TokInteger)
	if err != nil {
		return err
	}
	n, _ := strconv.Atoi(tok.Lit)
	q.Limit = n
	return nil
}

func (p *cqlParser) parseRerankClause(q *Query) error {
	p.advance() // consume RERANK
	q.Rerank = true

	// optional WITH "model-hint"
	if p.cur().Type == TokWith {
		p.advance()
		hintTok, err := p.expect(TokString)
		if err != nil {
			return err
		}
		q.RerankHint = hintTok.Lit
	}
	return nil
}

func (p *cqlParser) parseExcludeClause(q *Query) error {
	p.advance() // consume EXCLUDE

	// Expect SOURCES keyword
	if p.cur().Type != TokSources {
		return errAt(p.cur().Pos, "expected SOURCES after EXCLUDE")
	}
	p.advance() // consume SOURCES

	// Parse comma-separated quoted strings
	for {
		tok, err := p.expect(TokString)
		if err != nil {
			return err
		}
		q.ExcludeSourceIDs = append(q.ExcludeSourceIDs, tok.Lit)
		if p.cur().Type != TokComma {
			break
		}
		p.advance()
	}
	return nil
}

func (p *cqlParser) parseReturnClause(q *Query) error {
	p.advance() // consume RETURN

	for {
		tok := p.cur()
		if tok.Type == TokIdent {
			q.Return = append(q.Return, tok.Lit)
			p.advance()
		} else {
			return errAt(tok.Pos, "expected field name or *")
		}

		if p.cur().Type != TokComma {
			break
		}
		p.advance()
	}
	return nil
}
