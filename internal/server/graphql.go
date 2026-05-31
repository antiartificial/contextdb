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

	feedbackEventType := graphql.NewObject(graphql.ObjectConfig{
		Name: "FeedbackEvent",
		Fields: graphql.Fields{
			"eventId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.EventID.String(), nil
			}},
			"namespace": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.Namespace, nil
			}},
			"nodeId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.NodeID.String(), nil
			}},
			"nodeVersion": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return int(event.NodeVersion), nil
			}},
			"action": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.Action, nil
			}},
			"confidence": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.Confidence, nil
			}},
			"utility": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.Utility, nil
			}},
			"sourceId": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.SourceID, nil
			}},
			"sourceCredibility": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.SourceCredibility, nil
			}},
			"reason": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.Reason, nil
			}},
			"quality": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.Quality, nil
			}},
			"txTime": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				event, _ := p.Source.(client.FeedbackEvent)
				return event.TxTime, nil
			}},
		},
	})

	sourceTrustPointType := graphql.NewObject(graphql.ObjectConfig{
		Name: "SourceTrustPoint",
		Fields: graphql.Fields{
			"sourceId": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				point, _ := p.Source.(client.SourceTrustPoint)
				return point.SourceID, nil
			}},
			"nodeId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				point, _ := p.Source.(client.SourceTrustPoint)
				return point.NodeID.String(), nil
			}},
			"action": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				point, _ := p.Source.(client.SourceTrustPoint)
				return point.Action, nil
			}},
			"sourceCredibility": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				point, _ := p.Source.(client.SourceTrustPoint)
				return point.SourceCredibility, nil
			}},
			"reason": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				point, _ := p.Source.(client.SourceTrustPoint)
				return point.Reason, nil
			}},
			"txTime": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				point, _ := p.Source.(client.SourceTrustPoint)
				return point.TxTime, nil
			}},
		},
	})

	reviewItemType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewItem",
		Fields: graphql.Fields{
			"id": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.ID, nil
			}},
			"type": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Type, nil
			}},
			"priority": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Priority, nil
			}},
			"reason": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Reason, nil
			}},
			"nodeId": &graphql.Field{Type: graphql.ID, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				if item.NodeID == uuid.Nil {
					return nil, nil
				}
				return item.NodeID.String(), nil
			}},
			"nodeIds": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.ID))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				out := make([]string, len(item.NodeIDs))
				for i, id := range item.NodeIDs {
					out[i] = id.String()
				}
				return out, nil
			}},
			"sourceId": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.SourceID, nil
			}},
			"action": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Action, nil
			}},
			"text": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Text, nil
			}},
			"createdAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.CreatedAt, nil
			}},
			"suggestedAction": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Suggested, nil
			}},
			"confidence": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Confidence, nil
			}},
			"status": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Status, nil
			}},
			"owner": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Owner, nil
			}},
			"decision": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Decision, nil
			}},
			"note": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Note, nil
			}},
			"recheckAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.RecheckAt, nil
			}},
			"reviewedAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.ReviewedAt, nil
			}},
			"escalated": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.Escalated, nil
			}},
			"escalationLevel": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.EscalationLevel, nil
			}},
			"escalationReason": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.EscalationReason, nil
			}},
			"escalationAgeHours": &graphql.Field{Type: graphql.Float, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				item, _ := p.Source.(client.ReviewItem)
				return item.EscalationAgeHours, nil
			}},
		},
	})

	reviewDecisionType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewDecision",
		Fields: graphql.Fields{
			"eventId": &graphql.Field{Type: graphql.ID, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				if decision.EventID == uuid.Nil {
					return nil, nil
				}
				return decision.EventID.String(), nil
			}},
			"namespace": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.Namespace, nil
			}},
			"reviewId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.ReviewID, nil
			}},
			"status": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.Status, nil
			}},
			"owner": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.Owner, nil
			}},
			"decision": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.Decision, nil
			}},
			"note": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.Note, nil
			}},
			"recheckAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.RecheckAt, nil
			}},
			"txTime": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				decision, _ := p.Source.(client.ReviewDecision)
				return decision.TxTime, nil
			}},
		},
	})

	reviewEscalationGroupType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewEscalationGroup",
		Fields: graphql.Fields{
			"owner": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.Owner, nil
			}},
			"sourceId": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.SourceID, nil
			}},
			"type": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.Type, nil
			}},
			"escalationLevel": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.EscalationLevel, nil
			}},
			"count": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.Count, nil
			}},
			"maxPriority": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.MaxPriority, nil
			}},
			"maxAgeHours": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.MaxAgeHours, nil
			}},
			"reviewIds": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.ID))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				group, _ := p.Source.(client.ReviewEscalationGroup)
				return group.ReviewIDs, nil
			}},
		},
	})

	reviewEscalationDigestType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewEscalationDigest",
		Fields: graphql.Fields{
			"generatedAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				digest, _ := p.Source.(client.ReviewEscalationDigest)
				return digest.GeneratedAt, nil
			}},
			"note": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				digest, _ := p.Source.(client.ReviewEscalationDigest)
				return digest.Note, nil
			}},
			"totalEscalated": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				digest, _ := p.Source.(client.ReviewEscalationDigest)
				return digest.TotalEscalated, nil
			}},
			"groups": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewEscalationGroupType))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				digest, _ := p.Source.(client.ReviewEscalationDigest)
				return digest.Groups, nil
			}},
		},
	})

	reviewHandoffWebhookDeliveryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewHandoffWebhookDelivery",
		Fields: graphql.Fields{
			"targetUrl": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.TargetURL, nil
			}},
			"method": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.Method, nil
			}},
			"dryRun": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.DryRun, nil
			}},
			"eventId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.EventID.String(), nil
			}},
			"plannedAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.PlannedAt, nil
			}},
			"totalEscalated": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.TotalEscalated, nil
			}},
			"payloadSha256": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.PayloadSHA256, nil
			}},
			"signature": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.Signature, nil
			}},
			"attempt": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.Attempt, nil
			}},
			"maxAttempts": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.MaxAttempts, nil
			}},
			"executed": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.Executed, nil
			}},
			"statusCode": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.StatusCode, nil
			}},
			"responseBody": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.ResponseBody, nil
			}},
			"error": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.Error, nil
			}},
			"groups": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewEscalationGroupType))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				delivery, _ := p.Source.(client.ReviewHandoffWebhookDelivery)
				return delivery.Groups, nil
			}},
		},
	})

	reviewHandoffDeliveryReceiptType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewHandoffDeliveryReceipt",
		Fields: graphql.Fields{
			"receiptId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.ReceiptID.String(), nil
			}},
			"digestEventId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.DigestEventID.String(), nil
			}},
			"targetUrl": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.TargetURL, nil
			}},
			"deliveredAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.DeliveredAt, nil
			}},
			"owner": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.Owner, nil
			}},
			"escalationLevel": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.EscalationLevel, nil
			}},
			"success": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.Success, nil
			}},
			"statusCode": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.StatusCode, nil
			}},
			"payloadSha256": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.PayloadSHA256, nil
			}},
			"responseSha256": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.ResponseSHA256, nil
			}},
			"error": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				receipt, _ := p.Source.(client.ReviewHandoffDeliveryReceipt)
				return receipt.Error, nil
			}},
		},
	})

	reviewHandoffRetryCandidateType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewHandoffRetryCandidate",
		Fields: graphql.Fields{
			"digestEventId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.DigestEventID.String(), nil
			}},
			"targetUrl": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.TargetURL, nil
			}},
			"lastReceiptId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.LastReceiptID.String(), nil
			}},
			"lastAttemptAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.LastAttemptAt, nil
			}},
			"owner": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.Owner, nil
			}},
			"escalationLevel": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.EscalationLevel, nil
			}},
			"attempts": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.Attempts, nil
			}},
			"lastStatusCode": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.LastStatusCode, nil
			}},
			"payloadSha256": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.PayloadSHA256, nil
			}},
			"lastError": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				candidate, _ := p.Source.(client.ReviewHandoffRetryCandidate)
				return candidate.LastError, nil
			}},
		},
	})

	reviewHandoffRetryRecommendationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewHandoffRetryRecommendation",
		Fields: graphql.Fields{
			"digestEventId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.DigestEventID.String(), nil
			}},
			"targetUrl": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.TargetURL, nil
			}},
			"lastReceiptId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.LastReceiptID.String(), nil
			}},
			"lastAttemptAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.LastAttemptAt, nil
			}},
			"owner": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.Owner, nil
			}},
			"escalationLevel": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.EscalationLevel, nil
			}},
			"attempts": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.Attempts, nil
			}},
			"lastStatusCode": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.LastStatusCode, nil
			}},
			"payloadSha256": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.PayloadSHA256, nil
			}},
			"lastError": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.LastError, nil
			}},
			"recommendedAfter": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.RecommendedAfter, nil
			}},
			"delaySeconds": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.DelaySeconds, nil
			}},
			"ready": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.Ready, nil
			}},
			"reason": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				recommendation, _ := p.Source.(client.ReviewHandoffRetryRecommendation)
				return recommendation.Reason, nil
			}},
		},
	})

	reviewHandoffRetryStatusFamilyCountType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewHandoffRetryStatusFamilyCount",
		Fields: graphql.Fields{
			"family": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				count, _ := p.Source.(client.ReviewHandoffRetryStatusFamilyCount)
				return count.Family, nil
			}},
			"count": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				count, _ := p.Source.(client.ReviewHandoffRetryStatusFamilyCount)
				return count.Count, nil
			}},
		},
	})

	reviewHandoffRetryFatigueSummaryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "ReviewHandoffRetryFatigueSummary",
		Fields: graphql.Fields{
			"targetUrl": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.TargetURL, nil
			}},
			"candidates": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.Candidates, nil
			}},
			"totalAttempts": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.TotalAttempts, nil
			}},
			"ready": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.Ready, nil
			}},
			"waiting": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.Waiting, nil
			}},
			"statusFamilies": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffRetryStatusFamilyCountType))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.StatusFamilies, nil
			}},
			"lastStatusCode": &graphql.Field{Type: graphql.Int, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.LastStatusCode, nil
			}},
			"lastError": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.LastError, nil
			}},
			"lastAttemptAt": &graphql.Field{Type: graphql.DateTime, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				summary, _ := p.Source.(client.ReviewHandoffRetryFatigueSummary)
				return summary.LastAttemptAt, nil
			}},
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

	acquisitionTaskType := graphql.NewObject(graphql.ObjectConfig{
		Name: "AcquisitionTask",
		Fields: graphql.Fields{
			"id": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				return t.ID, nil
			}},
			"type": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				return t.Type, nil
			}},
			"priority": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				return t.Priority, nil
			}},
			"description": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				return t.Description, nil
			}},
			"prompt": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				return t.Prompt, nil
			}},
			"relatedNodeIds": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.ID))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				out := make([]string, len(t.RelatedNodeIDs))
				for i, id := range t.RelatedNodeIDs {
					out[i] = id.String()
				}
				return out, nil
			}},
			"nearestTopics": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.String))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				t, _ := p.Source.(client.AcquisitionTask)
				return t.NearestTopics, nil
			}},
		},
	})

	acquisitionPlanType := graphql.NewObject(graphql.ObjectConfig{
		Name: "AcquisitionPlan",
		Fields: graphql.Fields{
			"namespace": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				plan, _ := p.Source.(*client.AcquisitionPlan)
				if plan == nil {
					return "", nil
				}
				return plan.Namespace, nil
			}},
			"coverageScore": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				plan, _ := p.Source.(*client.AcquisitionPlan)
				if plan == nil {
					return 0, nil
				}
				return plan.CoverageScore, nil
			}},
			"totalNodes": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				plan, _ := p.Source.(*client.AcquisitionPlan)
				if plan == nil {
					return 0, nil
				}
				return plan.TotalNodes, nil
			}},
			"tasks": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(acquisitionTaskType))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				plan, _ := p.Source.(*client.AcquisitionPlan)
				if plan == nil {
					return []client.AcquisitionTask{}, nil
				}
				return plan.Tasks, nil
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

	rankedNodeExplanationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "RankedNodeExplanation",
		Fields: graphql.Fields{
			"nodeId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.NodeID.String(), nil
			}},
			"text": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.Text, nil
			}},
			"score": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.Score, nil
			}},
			"similarityScore": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.SimilarityScore, nil
			}},
			"confidenceScore": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.ConfidenceScore, nil
			}},
			"recencyScore": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.RecencyScore, nil
			}},
			"utilityScore": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.UtilityScore, nil
			}},
			"scoreBreakdown": &graphql.Field{Type: graphql.NewNonNull(scoreBreakdownType), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.ScoreBreakdown, nil
			}},
			"retrievalSource": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				n, _ := p.Source.(client.RankedNodeExplanation)
				return n.RetrievalSource, nil
			}},
		},
	})

	rankEvidenceLinkType := graphql.NewObject(graphql.ObjectConfig{
		Name: "RankEvidenceLink",
		Fields: graphql.Fields{
			"nodeId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				link, _ := p.Source.(client.RankEvidenceLink)
				return link.NodeID.String(), nil
			}},
			"edgeId": &graphql.Field{Type: graphql.NewNonNull(graphql.ID), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				link, _ := p.Source.(client.RankEvidenceLink)
				return link.EdgeID.String(), nil
			}},
			"edgeWeight": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				link, _ := p.Source.(client.RankEvidenceLink)
				return link.EdgeWeight, nil
			}},
			"confidence": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				link, _ := p.Source.(client.RankEvidenceLink)
				return link.Confidence, nil
			}},
			"text": &graphql.Field{Type: graphql.String, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				link, _ := p.Source.(client.RankEvidenceLink)
				return link.Text, nil
			}},
		},
	})

	rankEvidenceType := graphql.NewObject(graphql.ObjectConfig{
		Name: "RankEvidence",
		Fields: graphql.Fields{
			"compoundConfidence": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				evidence, _ := p.Source.(client.RankEvidence)
				return evidence.CompoundConfidence, nil
			}},
			"supportCount": &graphql.Field{Type: graphql.NewNonNull(graphql.Int), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				evidence, _ := p.Source.(client.RankEvidence)
				return evidence.SupportCount, nil
			}},
			"links": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(rankEvidenceLinkType))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				evidence, _ := p.Source.(client.RankEvidence)
				return evidence.Links, nil
			}},
		},
	})

	rankedNodeExplanationType.AddFieldConfig("evidence", &graphql.Field{
		Type: graphql.NewNonNull(rankEvidenceType),
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			n, _ := p.Source.(client.RankedNodeExplanation)
			return n.Evidence, nil
		},
	})

	rankFactorDeltaType := graphql.NewObject(graphql.ObjectConfig{
		Name: "RankFactorDelta",
		Fields: graphql.Fields{
			"factor": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(client.RankFactorDelta)
				return f.Factor, nil
			}},
			"nodeContribution": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(client.RankFactorDelta)
				return f.NodeContribution, nil
			}},
			"otherContribution": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(client.RankFactorDelta)
				return f.OtherContribution, nil
			}},
			"delta": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				f, _ := p.Source.(client.RankFactorDelta)
				return f.Delta, nil
			}},
		},
	})

	rankExplanationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "RankExplanation",
		Fields: graphql.Fields{
			"node": &graphql.Field{Type: graphql.NewNonNull(rankedNodeExplanationType), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil {
					return client.RankedNodeExplanation{}, nil
				}
				return e.Node, nil
			}},
			"other": &graphql.Field{Type: graphql.NewNonNull(rankedNodeExplanationType), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil {
					return client.RankedNodeExplanation{}, nil
				}
				return e.Other, nil
			}},
			"winnerNodeId": &graphql.Field{Type: graphql.ID, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil || e.WinnerNodeID == uuid.Nil {
					return nil, nil
				}
				return e.WinnerNodeID.String(), nil
			}},
			"loserNodeId": &graphql.Field{Type: graphql.ID, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil || e.LoserNodeID == uuid.Nil {
					return nil, nil
				}
				return e.LoserNodeID.String(), nil
			}},
			"margin": &graphql.Field{Type: graphql.NewNonNull(graphql.Float), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil {
					return 0, nil
				}
				return e.Margin, nil
			}},
			"summary": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil {
					return "", nil
				}
				return e.Summary, nil
			}},
			"factors": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(rankFactorDeltaType))), Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				e, _ := p.Source.(*client.RankExplanation)
				if e == nil {
					return []client.RankFactorDelta{}, nil
				}
				return e.Factors, nil
			}},
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
			"explainRank": &graphql.Field{
				Type: graphql.NewNonNull(rankExplanationType),
				Args: graphql.FieldConfigArgument{
					"namespace":   &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":        &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"nodeId":      &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"otherNodeId": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"text":        &graphql.ArgumentConfig{Type: graphql.String},
					"vector":      &graphql.ArgumentConfig{Type: graphql.NewList(graphql.Float)},
					"maxDepth":    &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveExplainRank,
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
			"acquisitionPlan": &graphql.Field{
				Type: graphql.NewNonNull(acquisitionPlanType),
				Args: graphql.FieldConfigArgument{
					"namespace":  &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"topK":       &graphql.ArgumentConfig{Type: graphql.Int},
					"minGapSize": &graphql.ArgumentConfig{Type: graphql.Float},
					"maxGaps":    &graphql.ArgumentConfig{Type: graphql.Int},
					"budget":     &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveAcquisitionPlan,
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
			"feedbackEvents": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(feedbackEventType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveFeedbackEvents,
			},
			"sourceTrustTimeline": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(sourceTrustPointType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"sourceId":  &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveSourceTrustTimeline,
			},
			"reviewQueue": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewItemType))),
				Args: graphql.FieldConfigArgument{
					"namespace":                       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":                            &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":                           &graphql.ArgumentConfig{Type: graphql.DateTime},
					"lowConfidenceThreshold":          &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceTrustThreshold":            &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceTrustDropThreshold":        &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceRefutationThreshold":       &graphql.ArgumentConfig{Type: graphql.Int},
					"escalationAfterHours":            &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceAnomalyEscalationPriority": &graphql.ArgumentConfig{Type: graphql.Float},
					"types":                           &graphql.ArgumentConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
					"sourceId":                        &graphql.ArgumentConfig{Type: graphql.String},
					"status":                          &graphql.ArgumentConfig{Type: graphql.String},
					"owner":                           &graphql.ArgumentConfig{Type: graphql.String},
					"limit":                           &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveReviewQueue,
			},
			"reviewEscalationDigest": &graphql.Field{
				Type: graphql.NewNonNull(reviewEscalationDigestType),
				Args: graphql.FieldConfigArgument{
					"namespace":                       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":                            &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":                           &graphql.ArgumentConfig{Type: graphql.DateTime},
					"lowConfidenceThreshold":          &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceTrustThreshold":            &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceTrustDropThreshold":        &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceRefutationThreshold":       &graphql.ArgumentConfig{Type: graphql.Int},
					"escalationAfterHours":            &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceAnomalyEscalationPriority": &graphql.ArgumentConfig{Type: graphql.Float},
					"types":                           &graphql.ArgumentConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
					"sourceId":                        &graphql.ArgumentConfig{Type: graphql.String},
					"status":                          &graphql.ArgumentConfig{Type: graphql.String},
					"owner":                           &graphql.ArgumentConfig{Type: graphql.String},
					"limit":                           &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveReviewEscalationDigest,
			},
			"reviewEscalationDigests": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewEscalationDigestType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveReviewEscalationDigests,
			},
			"reviewHandoffs": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewEscalationDigestType))),
				Args: graphql.FieldConfigArgument{
					"namespace":       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":            &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":           &graphql.ArgumentConfig{Type: graphql.DateTime},
					"owner":           &graphql.ArgumentConfig{Type: graphql.String},
					"escalationLevel": &graphql.ArgumentConfig{Type: graphql.String},
					"limit":           &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveReviewHandoffs,
			},
			"reviewHandoffWebhookPlan": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffWebhookDeliveryType))),
				Args: graphql.FieldConfigArgument{
					"namespace":       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":            &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":           &graphql.ArgumentConfig{Type: graphql.DateTime},
					"owner":           &graphql.ArgumentConfig{Type: graphql.String},
					"escalationLevel": &graphql.ArgumentConfig{Type: graphql.String},
					"limit":           &graphql.ArgumentConfig{Type: graphql.Int},
					"targetUrl":       &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"secret":          &graphql.ArgumentConfig{Type: graphql.String},
					"maxAttempts":     &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveReviewHandoffWebhookPlan,
			},
			"reviewHandoffDeliveryReceipts": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffDeliveryReceiptType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveReviewHandoffDeliveryReceipts,
			},
			"reviewHandoffRetryCandidates": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffRetryCandidateType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveReviewHandoffRetryCandidates,
			},
			"reviewHandoffRetryRecommendations": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffRetryRecommendationType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveReviewHandoffRetryRecommendations,
			},
			"reviewHandoffRetryFatigue": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffRetryFatigueSummaryType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveReviewHandoffRetryFatigue,
			},
			"reviewDecisions": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewDecisionType))),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":     &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveReviewDecisions,
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
			"recordReviewDecision": &graphql.Field{
				Type: graphql.NewNonNull(reviewDecisionType),
				Args: graphql.FieldConfigArgument{
					"namespace": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":      &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"reviewId":  &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"status":    &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"owner":     &graphql.ArgumentConfig{Type: graphql.String},
					"decision":  &graphql.ArgumentConfig{Type: graphql.String},
					"note":      &graphql.ArgumentConfig{Type: graphql.String},
					"recheckAt": &graphql.ArgumentConfig{Type: graphql.DateTime},
				},
				Resolve: s.resolveRecordReviewDecision,
			},
			"recordReviewEscalationDigest": &graphql.Field{
				Type: graphql.NewNonNull(reviewEscalationDigestType),
				Args: graphql.FieldConfigArgument{
					"namespace":                       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":                            &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"note":                            &graphql.ArgumentConfig{Type: graphql.String},
					"after":                           &graphql.ArgumentConfig{Type: graphql.DateTime},
					"lowConfidenceThreshold":          &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceTrustThreshold":            &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceTrustDropThreshold":        &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceRefutationThreshold":       &graphql.ArgumentConfig{Type: graphql.Int},
					"escalationAfterHours":            &graphql.ArgumentConfig{Type: graphql.Float},
					"sourceAnomalyEscalationPriority": &graphql.ArgumentConfig{Type: graphql.Float},
					"types":                           &graphql.ArgumentConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
					"sourceId":                        &graphql.ArgumentConfig{Type: graphql.String},
					"status":                          &graphql.ArgumentConfig{Type: graphql.String},
					"owner":                           &graphql.ArgumentConfig{Type: graphql.String},
					"limit":                           &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveRecordReviewEscalationDigest,
			},
			"deliverReviewHandoffWebhook": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewHandoffWebhookDeliveryType))),
				Args: graphql.FieldConfigArgument{
					"namespace":       &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":            &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":           &graphql.ArgumentConfig{Type: graphql.DateTime},
					"owner":           &graphql.ArgumentConfig{Type: graphql.String},
					"escalationLevel": &graphql.ArgumentConfig{Type: graphql.String},
					"limit":           &graphql.ArgumentConfig{Type: graphql.Int},
					"targetUrl":       &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"secret":          &graphql.ArgumentConfig{Type: graphql.String},
					"maxAttempts":     &graphql.ArgumentConfig{Type: graphql.Int},
					"execute":         &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Boolean)},
					"timeoutMs":       &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveDeliverReviewHandoffWebhook,
			},
			"retryReviewHandoffWebhook": &graphql.Field{
				Type: graphql.NewNonNull(reviewHandoffWebhookDeliveryType),
				Args: graphql.FieldConfigArgument{
					"namespace":     &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "default"},
					"mode":          &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "general"},
					"after":         &graphql.ArgumentConfig{Type: graphql.DateTime},
					"digestEventId": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"targetUrl":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"secret":        &graphql.ArgumentConfig{Type: graphql.String},
					"maxAttempts":   &graphql.ArgumentConfig{Type: graphql.Int},
					"execute":       &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Boolean)},
					"timeoutMs":     &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: s.resolveRetryReviewHandoffWebhook,
			},
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

func (s *GraphQLServer) resolveExplainRank(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	nodeID, err := uuid.Parse(fmt.Sprint(p.Args["nodeId"]))
	if err != nil {
		return nil, fmt.Errorf("invalid nodeId: %w", err)
	}
	otherNodeID, err := uuid.Parse(fmt.Sprint(p.Args["otherNodeId"]))
	if err != nil {
		return nil, fmt.Errorf("invalid otherNodeId: %w", err)
	}
	text, _ := p.Args["text"].(string)
	maxDepth, _ := p.Args["maxDepth"].(int)

	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ExplainRank(p.Context, client.ExplainRankRequest{
		NodeID:      nodeID,
		OtherNodeID: otherNodeID,
		Text:        text,
		Vector:      float32SliceArg(p.Args["vector"]),
		MaxDepth:    maxDepth,
	})
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

func (s *GraphQLServer) resolveAcquisitionPlan(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	topK, _ := p.Args["topK"].(int)
	minGapSize, _ := p.Args["minGapSize"].(float64)
	maxGaps, _ := p.Args["maxGaps"].(int)
	budget, _ := p.Args["budget"].(int)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.AcquisitionPlan(p.Context, client.AcquisitionPlanRequest{
		TopK:       topK,
		MinGapSize: minGapSize,
		MaxGaps:    maxGaps,
		Budget:     budget,
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

func (s *GraphQLServer) resolveFeedbackEvents(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.FeedbackEvents(p.Context, after)
}

func (s *GraphQLServer) resolveSourceTrustTimeline(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	sourceID, _ := p.Args["sourceId"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.SourceTrustTimeline(p.Context, sourceID, after)
}

func (s *GraphQLServer) resolveReviewQueue(p graphql.ResolveParams) (interface{}, error) {
	ns, mode, req := reviewQueueRequestFromGraphQL(p)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewQueue(p.Context, req)
}

func (s *GraphQLServer) resolveReviewEscalationDigest(p graphql.ResolveParams) (interface{}, error) {
	ns, mode, req := reviewQueueRequestFromGraphQL(p)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewEscalationDigest(p.Context, req)
}

func reviewQueueRequestFromGraphQL(p graphql.ResolveParams) (string, string, client.ReviewQueueRequest) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	threshold, _ := p.Args["lowConfidenceThreshold"].(float64)
	sourceTrustThreshold, _ := p.Args["sourceTrustThreshold"].(float64)
	sourceTrustDropThreshold, _ := p.Args["sourceTrustDropThreshold"].(float64)
	sourceRefutationThreshold, _ := p.Args["sourceRefutationThreshold"].(int)
	escalationAfterHours, _ := p.Args["escalationAfterHours"].(float64)
	sourceAnomalyEscalationPriority, _ := p.Args["sourceAnomalyEscalationPriority"].(float64)
	types, _ := stringList(p.Args["types"])
	sourceID, _ := p.Args["sourceId"].(string)
	status, _ := p.Args["status"].(string)
	owner, _ := p.Args["owner"].(string)
	limit, _ := p.Args["limit"].(int)
	return ns, mode, client.ReviewQueueRequest{
		After:                           after,
		LowConfidenceThreshold:          threshold,
		SourceTrustThreshold:            sourceTrustThreshold,
		SourceTrustDropThreshold:        sourceTrustDropThreshold,
		SourceRefutationThreshold:       sourceRefutationThreshold,
		EscalationAfter:                 time.Duration(escalationAfterHours * float64(time.Hour)),
		SourceAnomalyEscalationPriority: sourceAnomalyEscalationPriority,
		Types:                           types,
		SourceID:                        sourceID,
		Status:                          status,
		Owner:                           owner,
		Limit:                           limit,
	}
}

func (s *GraphQLServer) resolveReviewDecisions(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewDecisions(p.Context, after)
}

func (s *GraphQLServer) resolveReviewEscalationDigests(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewEscalationDigests(p.Context, after)
}

func (s *GraphQLServer) resolveReviewHandoffs(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	owner, _ := p.Args["owner"].(string)
	level, _ := p.Args["escalationLevel"].(string)
	limit, _ := p.Args["limit"].(int)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffs(p.Context, client.ReviewHandoffRequest{
		After:           after,
		Owner:           owner,
		EscalationLevel: level,
		Limit:           limit,
	})
}

func (s *GraphQLServer) resolveReviewHandoffWebhookPlan(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	owner, _ := p.Args["owner"].(string)
	level, _ := p.Args["escalationLevel"].(string)
	limit, _ := p.Args["limit"].(int)
	targetURL, _ := p.Args["targetUrl"].(string)
	secret, _ := p.Args["secret"].(string)
	maxAttempts, _ := p.Args["maxAttempts"].(int)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffWebhookPlan(p.Context, client.ReviewHandoffWebhookRequest{
		ReviewHandoffRequest: client.ReviewHandoffRequest{
			After:           after,
			Owner:           owner,
			EscalationLevel: level,
			Limit:           limit,
		},
		TargetURL:   targetURL,
		Secret:      secret,
		MaxAttempts: maxAttempts,
	})
}

func (s *GraphQLServer) resolveReviewHandoffDeliveryReceipts(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffDeliveryReceipts(p.Context, after)
}

func (s *GraphQLServer) resolveReviewHandoffRetryCandidates(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffRetryCandidates(p.Context, after)
}

func (s *GraphQLServer) resolveReviewHandoffRetryRecommendations(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffRetryRecommendations(p.Context, after, time.Time{})
}

func (s *GraphQLServer) resolveReviewHandoffRetryFatigue(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffRetryFatigue(p.Context, after, time.Time{})
}

func (s *GraphQLServer) resolveDeliverReviewHandoffWebhook(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	owner, _ := p.Args["owner"].(string)
	level, _ := p.Args["escalationLevel"].(string)
	limit, _ := p.Args["limit"].(int)
	targetURL, _ := p.Args["targetUrl"].(string)
	secret, _ := p.Args["secret"].(string)
	maxAttempts, _ := p.Args["maxAttempts"].(int)
	execute, _ := p.Args["execute"].(bool)
	timeoutMS, _ := p.Args["timeoutMs"].(int)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffWebhookDeliver(p.Context, client.ReviewHandoffWebhookRequest{
		ReviewHandoffRequest: client.ReviewHandoffRequest{
			After:           after,
			Owner:           owner,
			EscalationLevel: level,
			Limit:           limit,
		},
		TargetURL:   targetURL,
		Secret:      secret,
		MaxAttempts: maxAttempts,
		Execute:     execute,
		Timeout:     time.Duration(timeoutMS) * time.Millisecond,
	})
}

func (s *GraphQLServer) resolveRetryReviewHandoffWebhook(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	after, _ := p.Args["after"].(time.Time)
	rawDigestID, _ := p.Args["digestEventId"].(string)
	digestID, err := uuid.Parse(rawDigestID)
	if err != nil {
		return nil, fmt.Errorf("invalid digestEventId: %w", err)
	}
	targetURL, _ := p.Args["targetUrl"].(string)
	secret, _ := p.Args["secret"].(string)
	maxAttempts, _ := p.Args["maxAttempts"].(int)
	execute, _ := p.Args["execute"].(bool)
	timeoutMS, _ := p.Args["timeoutMs"].(int)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.ReviewHandoffWebhookRetry(p.Context, client.ReviewHandoffRetryRequest{
		After:         after,
		DigestEventID: digestID,
		TargetURL:     targetURL,
		Secret:        secret,
		MaxAttempts:   maxAttempts,
		Execute:       execute,
		Timeout:       time.Duration(timeoutMS) * time.Millisecond,
	})
}

func (s *GraphQLServer) resolveRecordReviewDecision(p graphql.ResolveParams) (interface{}, error) {
	ns, _ := p.Args["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	mode, _ := p.Args["mode"].(string)
	reviewID, _ := p.Args["reviewId"].(string)
	status, _ := p.Args["status"].(string)
	owner, _ := p.Args["owner"].(string)
	decision, _ := p.Args["decision"].(string)
	note, _ := p.Args["note"].(string)
	recheckAt, _ := p.Args["recheckAt"].(time.Time)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.RecordReviewDecision(p.Context, client.ReviewDecisionRequest{
		ReviewID:  reviewID,
		Status:    status,
		Owner:     owner,
		Decision:  decision,
		Note:      note,
		RecheckAt: recheckAt,
	})
}

func (s *GraphQLServer) resolveRecordReviewEscalationDigest(p graphql.ResolveParams) (interface{}, error) {
	ns, mode, req := reviewQueueRequestFromGraphQL(p)
	note, _ := p.Args["note"].(string)
	h := s.db.Namespace(ns, resolveModeForGraphQL(mode))
	return h.RecordReviewEscalationDigest(p.Context, req, note)
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

func float32SliceArg(raw any) []float32 {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]float32, 0, len(items))
	for _, item := range items {
		if v, ok := floatArg(item); ok {
			out = append(out, float32(v))
		}
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
