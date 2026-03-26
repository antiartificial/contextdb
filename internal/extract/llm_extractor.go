package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
)

const extractionPrompt = `You are an entity and relation extraction system. Given the input text, extract:
1. Entities: named things (people, organizations, concepts, events, locations, etc.)
2. Relations: directed relationships between entities

Output valid JSON only, no other text:
{
  "entities": [{"name": "...", "type": "...", "properties": {...}}],
  "relations": [{"subject": "...", "predicate": "...", "object": "...", "weight": 0.9}]
}

Rules:
- Entity names should be canonical (e.g., "Go" not "golang" or "the Go programming language")
- Relation predicates use snake_case (e.g., "relates_to", "is_part_of", "contradicts")
- Weight is confidence in the relation [0, 1], default 0.8
- Extract all meaningful entities and relations, not just the most obvious ones`

// LLMExtractor extracts entities and relations using an LLM provider.
type LLMExtractor struct {
	provider   Provider
	chatModel  string
	embedModel string
}

// NewLLMExtractor returns an extractor powered by the given LLM provider.
func NewLLMExtractor(provider Provider, chatModel, embedModel string) *LLMExtractor {
	return &LLMExtractor{
		provider:   provider,
		chatModel:  chatModel,
		embedModel: embedModel,
	}
}

func (e *LLMExtractor) Extract(ctx context.Context, req ExtractionRequest) (*ExtractionResult, error) {
	// Step 1: Extract entities and relations via chat
	chatResp, err := e.provider.Chat(ctx, ChatRequest{
		Model: e.chatModel,
		Messages: []ChatMessage{
			{Role: "system", Content: extractionPrompt},
			{Role: "user", Content: req.Text},
		},
		Temperature: 0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("chat extraction: %w", err)
	}

	// Parse the JSON response
	var parsed struct {
		Entities  []Entity  `json:"entities"`
		Relations []Relation `json:"relations"`
	}

	content := strings.TrimSpace(chatResp.Content)
	// strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse extraction result: %w (raw: %s)", err, content)
	}

	// Step 2: Generate embeddings for entity names + original text
	var textsToEmbed []string
	textsToEmbed = append(textsToEmbed, req.Text) // index 0 = original text
	for _, ent := range parsed.Entities {
		textsToEmbed = append(textsToEmbed, ent.Name)
	}

	var vectors [][]float32
	if e.embedModel != "" {
		vectors, err = e.provider.Embed(ctx, textsToEmbed, e.embedModel)
		if err != nil {
			return nil, fmt.Errorf("embed entities: %w", err)
		}
	}

	// Step 3: Build nodes from entities
	now := time.Now()
	entityIDMap := make(map[string]uuid.UUID) // entity name → node ID
	var nodes []core.Node

	for i, ent := range parsed.Entities {
		id := uuid.New()
		entityIDMap[ent.Name] = id

		props := ent.Properties
		if props == nil {
			props = make(map[string]any)
		}
		props["text"] = ent.Name
		props["entity_type"] = ent.Type

		labels := append([]string{ent.Type}, req.Labels...)

		var vec []float32
		if vectors != nil && i+1 < len(vectors) {
			vec = vectors[i+1]
		}

		nodes = append(nodes, core.Node{
			ID:         id,
			Namespace:  req.Namespace,
			Labels:     labels,
			Properties: props,
			Vector:     vec,
			Confidence: 0.7, // default for extracted entities
			ValidFrom:  now,
			TxTime:     now,
		})
	}

	// Step 4: Build edges from relations
	var edges []core.Edge
	for _, rel := range parsed.Relations {
		srcID, srcOK := entityIDMap[rel.Subject]
		dstID, dstOK := entityIDMap[rel.Object]
		if !srcOK || !dstOK {
			continue
		}

		weight := rel.Weight
		if weight <= 0 {
			weight = 0.8
		}

		edges = append(edges, core.Edge{
			ID:        uuid.New(),
			Namespace: req.Namespace,
			Src:       srcID,
			Dst:       dstID,
			Type:      rel.Predicate,
			Weight:    weight,
			ValidFrom: now,
			TxTime:    now,
		})
	}

	return &ExtractionResult{
		Nodes:    nodes,
		Edges:    edges,
		Entities: parsed.Entities,
	}, nil
}
