package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/graphql-go/graphql"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/internal/store/postgres"
	"github.com/antiartificial/contextdb/pkg/client"
)

type GraphQLServer struct {
	db     *client.DB
	graph  store.GraphStore
	schema graphql.Schema
}

type graphQLRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

type graphQLSearchResult struct {
	Nodes      []graphQLNode
	TotalCount int
}

type graphQLNode struct {
	Node            core.Node
	Score           float64
	SimilarityScore float64
	ConfidenceScore float64
	RecencyScore    float64
	UtilityScore    float64
	Breakdown       core.ScoreBreakdown
	RetrievalSource string
}

type graphQLEdge struct {
	Edge  core.Edge
	Graph store.GraphStore
}

// NewGraphQLServer builds the GraphQL endpoint over the client and graph store.
func NewGraphQLServer(db *client.DB) (*GraphQLServer, error) {
	graph, _, _, _ := db.Stores()
	s := &GraphQLServer{db: db, graph: graph}
	schema, err := s.buildSchema()
	if err != nil {
		return nil, err
	}
	s.schema = schema
	return s, nil
}

func (s *GraphQLServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req graphQLRequest
	switch r.Method {
	case http.MethodGet:
		req.Query = r.URL.Query().Get("query")
		req.OperationName = r.URL.Query().Get("operationName")
	case http.MethodPost:
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing GraphQL query"))
		return
	}

	result := graphql.Do(graphql.Params{
		Schema:         s.schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        r.Context(),
	})
	status := http.StatusOK
	if len(result.Errors) > 0 && result.Data == nil {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, result)
}

func (s *GraphQLServer) buildSchema() (graphql.Schema, error) {
	scoreBreakdownType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ScoreBreakdown",
		Fields: graphql.Fields{
			"similarity": &graphql.Field{Type: graphql.Float, Resolve: resolveScoreBreakdownFloat(func(b core.ScoreBreakdown) float64 { return b.Similarity })},
			"confidence": &graphql.Field{Type: graphql.Float, Resolve: resolveScoreBreakdownFloat(func(b core.ScoreBreakdown) float64 { return b.Confidence })},
			"recency":    &graphql.Field{Type: graphql.Float, Resolve: resolveScoreBreakdownFloat(func(b core.ScoreBreakdown) float64 { return b.Recency })},
			"utility":    &graphql.Field{Type: graphql.Float, Resolve: resolveScoreBreakdownFloat(func(b core.ScoreBreakdown) float64 { return b.Utility })},
		},
	})

	sourceType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Source",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					src, _ := p.Source.(core.Source)
					return src.ID.String(), nil
				},
			},
			"name": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					src, _ := p.Source.(core.Source)
					return src.ExternalID, nil
				},
			},
			"alpha": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				src, _ := p.Source.(core.Source)
				return src.Alpha, nil
			}},
			"beta": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				src, _ := p.Source.(core.Source)
				return src.Beta, nil
			}},
			"effectiveCredibility": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				src, _ := p.Source.(core.Source)
				return src.EffectiveCredibility(), nil
			}},
		},
	})

	featureInfoType := graphql.NewObject(graphql.ObjectConfig{
		Name: "FeatureInfo",
		Fields: graphql.Fields{
			"name": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(buildinfo.Feature)
				return f.Name, nil
			}},
			"status": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(buildinfo.Feature)
				return f.Status, nil
			}},
			"since": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(buildinfo.Feature)
				return f.Since, nil
			}},
			"description": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(buildinfo.Feature)
				return f.Description, nil
			}},
		},
	})

	migrationInfoType := graphql.NewObject(graphql.ObjectConfig{
		Name: "MigrationInfo",
		Fields: graphql.Fields{
			"version": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				m, _ := p.Source.(buildinfo.Migration)
				return m.Version, nil
			}},
			"name": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				m, _ := p.Source.(buildinfo.Migration)
				return m.Name, nil
			}},
		},
	})

	versionInfoType := graphql.NewObject(graphql.ObjectConfig{
		Name: "VersionInfo",
		Fields: graphql.Fields{
			"version": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.Version, nil
			}},
			"apiVersion": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.APIVersion, nil
			}},
			"docsVersion": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.DocsVersion, nil
			}},
			"compatibility": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.Compatibility, nil
			}},
			"latestMigration": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.LatestMigration, nil
			}},
			"recommendedDocs": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.RecommendedDocs, nil
			}},
			"releaseNotesPath": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				info, _ := p.Source.(buildinfo.Info)
				return info.ReleaseNotesPath, nil
			}},
			"features": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(featureInfoType))),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					info, _ := p.Source.(buildinfo.Info)
					return info.Features, nil
				},
			},
			"migrations": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(migrationInfoType))),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					info, _ := p.Source.(buildinfo.Info)
					return info.Migrations, nil
				},
			},
		},
	})

	feedbackResultType := graphql.NewObject(graphql.ObjectConfig{
		Name: "FeedbackResult",
		Fields: graphql.Fields{
			"nodeId":            &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
			"action":            &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"confidence":        &graphql.Field{Type: graphql.NewNonNull(graphql.Float)},
			"utility":           &graphql.Field{Type: graphql.NewNonNull(graphql.Float)},
			"sourceId":          &graphql.Field{Type: graphql.String},
			"sourceCredibility": &graphql.Field{Type: graphql.Float},
			"reason":            &graphql.Field{Type: graphql.String},
		},
	})

	citedClaimType := graphql.NewObject(graphql.ObjectConfig{
		Name: "CitedClaim",
		Fields: graphql.Fields{
			"nodeId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return citedClaimFromSource(p.Source).NodeID.String(), nil
				},
			},
			"sourceId": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).SourceID, nil
			}},
			"text": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) { return citedClaimFromSource(p.Source).Text, nil }},
			"confidence": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).Confidence, nil
			}},
			"epistemicType": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).EpistemicType, nil
			}},
			"validFrom": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).ValidFrom, nil
			}},
			"validUntil": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).ValidUntil, nil
			}},
			"provenanceDepth": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).ProvenanceDepth, nil
			}},
			"relation": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return citedClaimFromSource(p.Source).Relation, nil
			}},
		},
	})

	narrativeReportType := graphql.NewObject(graphql.ObjectConfig{
		Name: "NarrativeReport",
		Fields: graphql.Fields{
			"nodeId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return narrativeReportFromSource(p.Source).NodeID.String(), nil
				},
			},
			"namespace": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).Namespace, nil
			}},
			"generatedAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).GeneratedAt, nil
			}},
			"summary": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).Summary, nil
			}},
			"claim": &graphql.Field{Type: citedClaimType, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).Claim, nil
			}},
			"evidence": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(citedClaimType)), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).Evidence, nil
			}},
			"contradictions": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(citedClaimType)), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).Contradictions, nil
			}},
			"provenance": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(citedClaimType)), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).Provenance, nil
			}},
			"confidenceExplanation": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return narrativeReportFromSource(p.Source).ConfidenceExplanation, nil
			}},
		},
	})

	knowledgeGapType := graphql.NewObject(graphql.ObjectConfig{
		Name: "KnowledgeGap",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return knowledgeGapFromSource(p.Source).ID.String(), nil
				},
			},
			"nearestTopics": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(graphql.String)), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return knowledgeGapFromSource(p.Source).NearestTopics, nil
			}},
			"centroidVector": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(graphql.Float)), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return float32sToFloat64s(knowledgeGapFromSource(p.Source).CentroidVector), nil
			}},
			"densityScore": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return knowledgeGapFromSource(p.Source).DensityScore, nil
			}},
			"confidenceGap": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return knowledgeGapFromSource(p.Source).ConfidenceGap, nil
			}},
			"temporalGapSeconds": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return knowledgeGapFromSource(p.Source).TemporalGap.Seconds(), nil
			}},
		},
	})

	gapReportType := graphql.NewObject(graphql.ObjectConfig{
		Name: "GapReport",
		Fields: graphql.Fields{
			"namespace": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return gapReportFromSource(p.Source).Namespace, nil
			}},
			"gaps": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(knowledgeGapType)), Resolve: func(p graphql.ResolveParams) (interface{}, error) { return gapReportFromSource(p.Source).Gaps, nil }},
			"coverageScore": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return gapReportFromSource(p.Source).CoverageScore, nil
			}},
			"totalNodes": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return gapReportFromSource(p.Source).TotalNodes, nil
			}},
			"gapsDetected": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return gapReportFromSource(p.Source).GapsDetected, nil
			}},
		},
	})

	var nodeType *graphql.Object
	edgeType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Edge",
		Fields: graphql.FieldsThunk(func() graphql.Fields {
			return graphql.Fields{
				"id": &graphql.Field{
					Type: graphql.NewNonNull(graphql.ID),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						edge := graphQLEdgeFromSource(p.Source)
						return edge.Edge.ID.String(), nil
					},
				},
				"type": &graphql.Field{
					Type: graphql.NewNonNull(graphql.String),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						edge := graphQLEdgeFromSource(p.Source)
						return edge.Edge.Type, nil
					},
				},
				"from": &graphql.Field{
					Type: graphql.NewNonNull(nodeType),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						edge := graphQLEdgeFromSource(p.Source)
						return edge.resolveNode(p, edge.Edge.Src)
					},
				},
				"to": &graphql.Field{
					Type: graphql.NewNonNull(nodeType),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						edge := graphQLEdgeFromSource(p.Source)
						return edge.resolveNode(p, edge.Edge.Dst)
					},
				},
				"weight": &graphql.Field{
					Type: graphql.NewNonNull(graphql.Float),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						edge := graphQLEdgeFromSource(p.Source)
						return edge.Edge.Weight, nil
					},
				},
				"credibility": &graphql.Field{
					Type: graphql.NewNonNull(graphql.Float),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						edge := graphQLEdgeFromSource(p.Source)
						if v, ok := edge.Edge.Properties["credibility"].(float64); ok {
							return v, nil
						}
						return edge.Edge.Weight, nil
					},
				},
			}
		}),
	})

	nodeType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Node",
		Fields: graphql.FieldsThunk(func() graphql.Fields {
			return graphql.Fields{
				"id": &graphql.Field{
					Type: graphql.NewNonNull(graphql.ID),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return n.Node.ID.String(), nil
					},
				},
				"content": &graphql.Field{
					Type: graphql.NewNonNull(graphql.String),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return core.NodeText(n.Node), nil
					},
				},
				"vector": &graphql.Field{
					Type: graphql.NewList(graphql.NewNonNull(graphql.Float)),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return float32sToFloat64s(n.Node.Vector), nil
					},
				},
				"credibility": &graphql.Field{
					Type: graphql.NewNonNull(graphql.Float),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return n.Node.Confidence, nil
					},
				},
				"validFrom": &graphql.Field{
					Type: graphql.DateTime,
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return n.Node.ValidFrom, nil
					},
				},
				"validTo": &graphql.Field{
					Type: graphql.DateTime,
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return n.Node.ValidUntil, nil
					},
				},
				"score":           &graphql.Field{Type: graphql.Float, Resolve: resolveGraphQLNodeFloat(func(n graphQLNode) float64 { return n.Score })},
				"similarityScore": &graphql.Field{Type: graphql.Float, Resolve: resolveGraphQLNodeFloat(func(n graphQLNode) float64 { return n.SimilarityScore })},
				"confidenceScore": &graphql.Field{Type: graphql.Float, Resolve: resolveGraphQLNodeFloat(func(n graphQLNode) float64 { return n.ConfidenceScore })},
				"recencyScore":    &graphql.Field{Type: graphql.Float, Resolve: resolveGraphQLNodeFloat(func(n graphQLNode) float64 { return n.RecencyScore })},
				"utilityScore":    &graphql.Field{Type: graphql.Float, Resolve: resolveGraphQLNodeFloat(func(n graphQLNode) float64 { return n.UtilityScore })},
				"scoreBreakdown": &graphql.Field{
					Type: scoreBreakdownType,
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return n.Breakdown, nil
					},
				},
				"retrievalSource": &graphql.Field{
					Type: graphql.String,
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						return n.RetrievalSource, nil
					},
				},
				"edges": &graphql.Field{
					Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(edgeType))),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						edges, err := s.graph.EdgesFrom(p.Context, n.Node.Namespace, n.Node.ID, nil)
						if err != nil {
							return nil, err
						}
						out := make([]graphQLEdge, len(edges))
						for i, edge := range edges {
							out[i] = graphQLEdge{Edge: edge, Graph: s.graph}
						}
						return out, nil
					},
				},
				"sources": &graphql.Field{
					Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(sourceType))),
					Resolve: func(p graphql.ResolveParams) (interface{}, error) {
						n := graphQLNodeFromSource(p.Source)
						sourceID, _ := n.Node.Properties["source_id"].(string)
						if sourceID == "" {
							return []core.Source{}, nil
						}
						src, err := s.graph.GetSourceByExternalID(p.Context, n.Node.Namespace, sourceID)
						if err != nil || src == nil {
							return []core.Source{}, err
						}
						return []core.Source{*src}, nil
					},
				},
			}
		}),
	})

	var filterInput *graphql.InputObject
	filterInput = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "FilterInput",
		Fields: graphql.InputObjectConfigFieldMapThunk(func() graphql.InputObjectConfigFieldMap {
			return graphql.InputObjectConfigFieldMap{
				"and":                  &graphql.InputObjectFieldConfig{Type: graphql.NewList(filterInput)},
				"or":                   &graphql.InputObjectFieldConfig{Type: graphql.NewList(filterInput)},
				"not":                  &graphql.InputObjectFieldConfig{Type: filterInput},
				"contentContains":      &graphql.InputObjectFieldConfig{Type: graphql.String},
				"similarityMin":        &graphql.InputObjectFieldConfig{Type: graphql.Float},
				"hasEdgeTo":            &graphql.InputObjectFieldConfig{Type: graphql.NewList(graphql.ID)},
				"edgeTypes":            &graphql.InputObjectFieldConfig{Type: graphql.NewList(graphql.String)},
				"edgeWeightMin":        &graphql.InputObjectFieldConfig{Type: graphql.Float},
				"sourceCredibilityMin": &graphql.InputObjectFieldConfig{Type: graphql.Float},
				"sourceCredibilityMax": &graphql.InputObjectFieldConfig{Type: graphql.Float},
				"validAt":              &graphql.InputObjectFieldConfig{Type: graphql.DateTime},
				"validFromBefore":      &graphql.InputObjectFieldConfig{Type: graphql.DateTime},
				"validToAfter":         &graphql.InputObjectFieldConfig{Type: graphql.DateTime},
			}
		}),
	})

	searchResultType := graphql.NewObject(graphql.ObjectConfig{
		Name: "SearchResult",
		Fields: graphql.Fields{
			"nodes":      &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(nodeType)))},
			"totalCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
		},
	})

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"search": &graphql.Field{
				Type: graphql.NewNonNull(searchResultType),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"query":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"filter":    &graphql.ArgumentConfig{Type: filterInput},
					"limit":     &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 10},
				},
				Resolve: s.resolveSearch,
			},
			"narrative": &graphql.Field{
				Type: narrativeReportType,
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"nodeId":    &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: s.resolveNarrative,
			},
			"knowledgeGaps": &graphql.Field{
				Type: graphql.NewNonNull(gapReportType),
				Args: graphql.FieldConfigArgument{
					"namespace":  &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"topK":       &graphql.ArgumentConfig{Type: graphql.Int},
					"minGapSize": &graphql.ArgumentConfig{Type: graphql.Float},
					"maxGaps":    &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveKnowledgeGaps,
			},
			"version": &graphql.Field{
				Type:    graphql.NewNonNull(versionInfoType),
				Resolve: s.resolveVersionInfo,
			},
			"features": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(featureInfoType))),
				Resolve: s.resolveFeatures,
			},
			"migrations": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(migrationInfoType))),
				Resolve: s.resolveMigrations,
			},
		},
	})

	feedbackArgs := graphql.FieldConfigArgument{
		"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
		"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
		"nodeId":    &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
		"reason":    &graphql.ArgumentConfig{Type: graphql.String},
		"quality":   &graphql.ArgumentConfig{Type: graphql.Int},
	}
	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"validateClaim": &graphql.Field{
				Type:    graphql.NewNonNull(feedbackResultType),
				Args:    feedbackArgs,
				Resolve: s.resolveFeedbackMutation("validate"),
			},
			"refuteClaim": &graphql.Field{
				Type:    graphql.NewNonNull(feedbackResultType),
				Args:    feedbackArgs,
				Resolve: s.resolveFeedbackMutation("refute"),
			},
			"markUseful": &graphql.Field{
				Type:    graphql.NewNonNull(feedbackResultType),
				Args:    feedbackArgs,
				Resolve: s.resolveFeedbackMutation("useful"),
			},
			"markStale": &graphql.Field{
				Type:    graphql.NewNonNull(feedbackResultType),
				Args:    feedbackArgs,
				Resolve: s.resolveFeedbackMutation("stale"),
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{Query: queryType, Mutation: mutationType})
}

func (s *GraphQLServer) resolveSearch(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	query, _ := p.Args["query"].(string)
	limit, _ := p.Args["limit"].(int)
	if limit <= 0 {
		limit = 10
	}
	filter, _ := p.Args["filter"].(map[string]any)

	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	results, err := h.Retrieve(p.Context, client.RetrieveRequest{Text: query, TopK: limit})
	if err != nil {
		return nil, err
	}
	nodes := make([]graphQLNode, 0, len(results))
	for _, r := range results {
		gn := graphQLNode{
			Node:            r.Node,
			Score:           r.Score,
			SimilarityScore: r.SimilarityScore,
			ConfidenceScore: r.ConfidenceScore,
			RecencyScore:    r.RecencyScore,
			UtilityScore:    r.UtilityScore,
			Breakdown:       r.Breakdown,
			RetrievalSource: r.RetrievalSource,
		}
		ok, err := s.matchesGraphQLFilter(p, gn, query, filter)
		if err != nil {
			return nil, err
		}
		if ok {
			nodes = append(nodes, gn)
		}
	}

	if len(nodes) == 0 {
		fallback, err := s.scanGraphQLSearch(p, ns, query, filter, limit)
		if err != nil {
			return nil, err
		}
		nodes = fallback
	}

	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return graphQLSearchResult{Nodes: nodes, TotalCount: len(nodes)}, nil
}

func (s *GraphQLServer) resolveNarrative(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	nodeIDRaw, _ := p.Args["nodeId"].(string)
	nodeID, err := uuid.Parse(nodeIDRaw)
	if err != nil {
		return nil, err
	}
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.Explain(p.Context, nodeID)
}

func (s *GraphQLServer) resolveKnowledgeGaps(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	topK, _ := p.Args["topK"].(int)
	minGapSize, _ := p.Args["minGapSize"].(float64)
	maxGaps, _ := p.Args["maxGaps"].(int)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.KnowledgeGaps(p.Context, client.GapRequest{
		TopK:       topK,
		MinGapSize: minGapSize,
		MaxGaps:    maxGaps,
	})
}

func (s *GraphQLServer) resolveVersionInfo(p graphql.ResolveParams) (interface{}, error) {
	return buildinfo.Current(postgres.AvailableMigrations()), nil
}

func (s *GraphQLServer) resolveFeatures(p graphql.ResolveParams) (interface{}, error) {
	return buildinfo.Features(), nil
}

func (s *GraphQLServer) resolveMigrations(p graphql.ResolveParams) (interface{}, error) {
	return postgres.AvailableMigrations(), nil
}

func (s *GraphQLServer) resolveFeedbackMutation(action string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ns, _ := p.Args["namespace"].(string)
		if ns == "" {
			ns = "default"
		}
		mode, _ := p.Args["mode"].(string)
		nodeIDRaw, _ := p.Args["nodeId"].(string)
		nodeID, err := uuid.Parse(nodeIDRaw)
		if err != nil {
			return nil, err
		}
		reason, _ := p.Args["reason"].(string)
		quality, _ := p.Args["quality"].(int)

		h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
		var result client.FeedbackResult
		switch action {
		case "validate":
			result, err = h.ValidateClaim(p.Context, nodeID)
		case "refute":
			result, err = h.RefuteClaim(p.Context, nodeID, reason)
		case "useful":
			result, err = h.MarkUseful(p.Context, nodeID, quality)
		case "stale":
			result, err = h.MarkStale(p.Context, nodeID, reason)
		default:
			err = fmt.Errorf("unknown feedback action %q", action)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"nodeId":            result.NodeID.String(),
			"action":            result.Action,
			"confidence":        result.Confidence,
			"utility":           result.Utility,
			"sourceId":          result.SourceID,
			"sourceCredibility": result.SourceCredibility,
			"reason":            result.Reason,
		}, nil
	}
}

func (s *GraphQLServer) scanGraphQLSearch(p graphql.ResolveParams, ns, query string, filter map[string]any, limit int) ([]graphQLNode, error) {
	asOf := time.Now()
	if t, ok := graphQLTime(filter["validAt"]); ok {
		asOf = t
	}
	all, err := s.graph.ValidAt(p.Context, ns, asOf, nil)
	if err != nil {
		return nil, err
	}
	out := make([]graphQLNode, 0, len(all))
	needle := strings.ToLower(strings.TrimSpace(query))
	for _, node := range all {
		text := strings.ToLower(core.NodeText(node))
		if needle != "" && !strings.Contains(text, needle) {
			if content, _ := filter["contentContains"].(string); strings.TrimSpace(content) == "" {
				continue
			}
		}
		gn := graphQLNode{
			Node:            node,
			Score:           1,
			SimilarityScore: 1,
			ConfidenceScore: node.Confidence,
			RecencyScore:    1,
			UtilityScore:    1,
			Breakdown: core.ScoreBreakdown{
				Similarity: 1,
			},
			RetrievalSource: "scan",
		}
		ok, err := s.matchesGraphQLFilter(p, gn, query, filter)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, gn)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *GraphQLServer) matchesGraphQLFilter(p graphql.ResolveParams, node graphQLNode, query string, filter map[string]any) (bool, error) {
	if len(filter) == 0 {
		return true, nil
	}
	if filters, ok := filterList(filter["and"]); ok {
		for _, f := range filters {
			ok, err := s.matchesGraphQLFilter(p, node, query, f)
			if err != nil || !ok {
				return ok, err
			}
		}
	}
	if filters, ok := filterList(filter["or"]); ok && len(filters) > 0 {
		anyMatch := false
		for _, f := range filters {
			ok, err := s.matchesGraphQLFilter(p, node, query, f)
			if err != nil {
				return false, err
			}
			anyMatch = anyMatch || ok
		}
		if !anyMatch {
			return false, nil
		}
	}
	if raw, ok := filter["not"].(map[string]any); ok {
		matches, err := s.matchesGraphQLFilter(p, node, query, raw)
		if err != nil || matches {
			return false, err
		}
	}
	if content, _ := filter["contentContains"].(string); content != "" {
		if !strings.Contains(strings.ToLower(core.NodeText(node.Node)), strings.ToLower(content)) {
			return false, nil
		}
	}
	if min, ok := floatArg(filter["similarityMin"]); ok && node.SimilarityScore < min {
		return false, nil
	}
	if t, ok := graphQLTime(filter["validAt"]); ok && !node.Node.IsValidAt(t) {
		return false, nil
	}
	if t, ok := graphQLTime(filter["validFromBefore"]); ok && !node.Node.ValidFrom.Before(t) {
		return false, nil
	}
	if t, ok := graphQLTime(filter["validToAfter"]); ok {
		if node.Node.ValidUntil == nil || !node.Node.ValidUntil.After(t) {
			return false, nil
		}
	}
	if ok, err := s.matchesGraphQLEdgeFilters(p, node.Node, filter); err != nil || !ok {
		return ok, err
	}
	if ok, err := s.matchesGraphQLSourceFilters(p, node.Node, filter); err != nil || !ok {
		return ok, err
	}
	return true, nil
}

func (s *GraphQLServer) matchesGraphQLEdgeFilters(p graphql.ResolveParams, node core.Node, filter map[string]any) (bool, error) {
	ids, hasIDs := stringList(filter["hasEdgeTo"])
	types, hasTypes := stringList(filter["edgeTypes"])
	minWeight, hasMinWeight := floatArg(filter["edgeWeightMin"])
	if !hasIDs && !hasTypes && !hasMinWeight {
		return true, nil
	}
	edges, err := s.graph.EdgesFrom(p.Context, node.Namespace, node.ID, types)
	if err != nil {
		return false, err
	}
	for _, edge := range edges {
		if hasMinWeight && edge.Weight < minWeight {
			continue
		}
		if hasIDs {
			for _, id := range ids {
				if edge.Dst.String() == id {
					return true, nil
				}
			}
			continue
		}
		return true, nil
	}
	return false, nil
}

func (s *GraphQLServer) matchesGraphQLSourceFilters(p graphql.ResolveParams, node core.Node, filter map[string]any) (bool, error) {
	min, hasMin := floatArg(filter["sourceCredibilityMin"])
	max, hasMax := floatArg(filter["sourceCredibilityMax"])
	if !hasMin && !hasMax {
		return true, nil
	}
	sourceID, _ := node.Properties["source_id"].(string)
	if sourceID == "" {
		return false, nil
	}
	src, err := s.graph.GetSourceByExternalID(p.Context, node.Namespace, sourceID)
	if err != nil || src == nil {
		return false, err
	}
	cred := src.EffectiveCredibility()
	if hasMin && cred < min {
		return false, nil
	}
	if hasMax && cred > max {
		return false, nil
	}
	return true, nil
}

func graphQLEdgeFromSource(source interface{}) graphQLEdge {
	if edge, ok := source.(graphQLEdge); ok {
		return edge
	}
	if edge, ok := source.(*graphQLEdge); ok && edge != nil {
		return *edge
	}
	return graphQLEdge{}
}

func graphQLNodeFromSource(source interface{}) graphQLNode {
	if node, ok := source.(graphQLNode); ok {
		return node
	}
	if node, ok := source.(*graphQLNode); ok && node != nil {
		return *node
	}
	if node, ok := source.(core.Node); ok {
		return graphQLNode{Node: node}
	}
	return graphQLNode{}
}

func (e graphQLEdge) resolveNode(p graphql.ResolveParams, id uuid.UUID) (interface{}, error) {
	if e.Graph == nil {
		return nil, fmt.Errorf("graph resolver unavailable")
	}
	node, err := e.Graph.GetNode(p.Context, e.Edge.Namespace, id)
	if err != nil || node == nil {
		return nil, err
	}
	return graphQLNode{Node: *node}, nil
}

func resolveGraphQLNodeFloat(fn func(graphQLNode) float64) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		return fn(graphQLNodeFromSource(p.Source)), nil
	}
}

func resolveScoreBreakdownFloat(fn func(core.ScoreBreakdown) float64) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		breakdown, _ := p.Source.(core.ScoreBreakdown)
		return fn(breakdown), nil
	}
}

func citedClaimFromSource(source interface{}) retrieval.CitedClaim {
	if claim, ok := source.(retrieval.CitedClaim); ok {
		return claim
	}
	if claim, ok := source.(*retrieval.CitedClaim); ok && claim != nil {
		return *claim
	}
	return retrieval.CitedClaim{}
}

func narrativeReportFromSource(source interface{}) *retrieval.NarrativeReport {
	if report, ok := source.(*retrieval.NarrativeReport); ok && report != nil {
		return report
	}
	if report, ok := source.(retrieval.NarrativeReport); ok {
		return &report
	}
	return &retrieval.NarrativeReport{}
}

func knowledgeGapFromSource(source interface{}) retrieval.KnowledgeGap {
	if gap, ok := source.(retrieval.KnowledgeGap); ok {
		return gap
	}
	if gap, ok := source.(*retrieval.KnowledgeGap); ok && gap != nil {
		return *gap
	}
	return retrieval.KnowledgeGap{}
}

func gapReportFromSource(source interface{}) *retrieval.GapReport {
	if report, ok := source.(*retrieval.GapReport); ok && report != nil {
		return report
	}
	if report, ok := source.(retrieval.GapReport); ok {
		return &report
	}
	return &retrieval.GapReport{}
}

func resolveModeForGraphQL(mode string) namespace.Mode {
	return resolveMode(mode)
}

func float32sToFloat64s(v []float32) []float64 {
	if len(v) == 0 {
		return nil
	}
	out := make([]float64, len(v))
	for i, f := range v {
		out[i] = float64(f)
	}
	return out
}

func filterList(raw any) ([]map[string]any, bool) {
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, true
}

func stringList(raw any) ([]string, bool) {
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			out = append(out, v)
		case fmt.Stringer:
			out = append(out, v.String())
		}
	}
	return out, len(out) > 0
}

func floatArg(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func graphQLTime(raw any) (time.Time, bool) {
	switch v := raw.(type) {
	case time.Time:
		return v, true
	case *time.Time:
		if v != nil {
			return *v, true
		}
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
