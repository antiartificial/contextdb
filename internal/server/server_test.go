package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
)

func startGRPCTestServer(t *testing.T, db *client.DB) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(server.FormatGRPCCodec())
	server.NewGRPCService(db).Register(srv)

	go func() {
		if err := srv.Serve(lis); err != nil {
			t.Logf("gRPC test server stopped: %v", err)
		}
	}()
	t.Cleanup(func() { srv.GracefulStop() })

	return lis.Addr().String()
}

func TestRESTServer_Ping(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/v1/ping", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var resp map[string]string
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	is.Equal(resp["status"], "ok")
}

func TestRESTServer_Introspection(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	for _, path := range []string{"/v1/version", "/v1/features", "/v1/migrations"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		is.Equal(w.Code, http.StatusOK)
		var resp map[string]any
		is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
		is.Equal(resp["version"], buildinfo.Version)
	}

	req := httptest.NewRequest("GET", "/v1/version", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var versionResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &versionResp))
	is.Equal(versionResp["api_version"], "v1")
	is.Equal(versionResp["latest_migration"], float64(2))
	features := versionResp["features"].([]any)
	is.True(len(features) > 0)
	migrations := versionResp["migrations"].([]any)
	is.Equal(len(migrations), 2)
}

func TestRESTServer_WriteAndRetrieve(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	// Write
	writeBody, _ := json.Marshal(map[string]any{
		"mode":       "belief_system",
		"content":    "Go uses goroutines for concurrency",
		"source_id":  "moderator:alice",
		"labels":     []string{"Claim"},
		"vector":     []float32{0.9, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
		"confidence": 0.95,
	})
	req := httptest.NewRequest("POST", "/v1/namespaces/channel:general/write", bytes.NewReader(writeBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var writeResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &writeResp))
	is.Equal(writeResp["admitted"], true)
	nodeID := writeResp["node_id"].(string)

	writeOtherBody, _ := json.Marshal(map[string]any{
		"mode":       "belief_system",
		"content":    "Go concurrency uses only operating system threads",
		"source_id":  "chat:uncertain",
		"labels":     []string{"Claim"},
		"vector":     []float32{0.1, 0.9, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
		"confidence": 0.2,
	})
	reqOther := httptest.NewRequest("POST", "/v1/namespaces/channel:general/write", bytes.NewReader(writeOtherBody))
	reqOther.Header.Set("Content-Type", "application/json")
	wOther := httptest.NewRecorder()
	handler.ServeHTTP(wOther, reqOther)
	is.Equal(wOther.Code, http.StatusOK)
	var writeOtherResp map[string]any
	is.NoErr(json.Unmarshal(wOther.Body.Bytes(), &writeOtherResp))
	otherNodeID := writeOtherResp["node_id"].(string)
	graph, _, _, _ := db.Stores()
	supportUUID := uuid.New()
	nodeUUID, err := uuid.Parse(nodeID)
	is.NoErr(err)
	is.NoErr(graph.UpsertNode(context.Background(), core.Node{
		ID:         supportUUID,
		Namespace:  "channel:general",
		Properties: map[string]any{"text": "Runbook confirms goroutine concurrency", "source_id": "docs"},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(time.Hour),
		TxTime:     time.Now(),
	}))
	is.NoErr(graph.UpsertEdge(context.Background(), core.Edge{
		ID:        uuid.New(),
		Namespace: "channel:general",
		Src:       supportUUID,
		Dst:       nodeUUID,
		Type:      core.EdgeSupports,
		Weight:    0.8,
	}))

	// Retrieve
	retrieveBody, _ := json.Marshal(map[string]any{
		"vector": []float32{0.9, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
		"top_k":  5,
	})
	req2 := httptest.NewRequest("POST", "/v1/namespaces/channel:general/retrieve", bytes.NewReader(retrieveBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	is.Equal(w2.Code, http.StatusOK)
	var retrieveResp map[string]any
	is.NoErr(json.Unmarshal(w2.Body.Bytes(), &retrieveResp))
	results := retrieveResp["results"].([]any)
	is.True(len(results) > 0)
	first := results[0].(map[string]any)
	breakdown := first["score_breakdown"].(map[string]any)
	is.True(breakdown["similarity"].(float64) > 0)

	explainRankBody, _ := json.Marshal(map[string]any{
		"mode":          "belief_system",
		"node_id":       nodeID,
		"other_node_id": otherNodeID,
		"vector":        []float32{0.9, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	})
	reqRank := httptest.NewRequest("POST", "/v1/namespaces/channel:general/rank/explain", bytes.NewReader(explainRankBody))
	reqRank.Header.Set("Content-Type", "application/json")
	wRank := httptest.NewRecorder()
	handler.ServeHTTP(wRank, reqRank)
	is.Equal(wRank.Code, http.StatusOK)
	var rankResp map[string]any
	is.NoErr(json.Unmarshal(wRank.Body.Bytes(), &rankResp))
	is.Equal(rankResp["winner_node_id"], nodeID)
	is.True(rankResp["summary"].(string) != "")
	is.True(len(rankResp["factors"].([]any)) == 4)
	nodeRank := rankResp["node"].(map[string]any)
	evidence := nodeRank["evidence"].(map[string]any)
	is.Equal(evidence["support_count"], float64(1))

	feedbackBody, _ := json.Marshal(map[string]any{"reason": "verified externally"})
	req3 := httptest.NewRequest("POST", "/v1/namespaces/channel:general/nodes/"+nodeID+"/validate", bytes.NewReader(feedbackBody))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	is.Equal(w3.Code, http.StatusOK)
	var feedbackResp map[string]any
	is.NoErr(json.Unmarshal(w3.Body.Bytes(), &feedbackResp))
	is.Equal(feedbackResp["action"], "validated")
	is.True(feedbackResp["source_credibility"].(float64) > 0.5)

	reqEvents := httptest.NewRequest("GET", "/v1/namespaces/channel:general/feedback/events", nil)
	wEvents := httptest.NewRecorder()
	handler.ServeHTTP(wEvents, reqEvents)

	is.Equal(wEvents.Code, http.StatusOK)
	var eventsResp map[string]any
	is.NoErr(json.Unmarshal(wEvents.Body.Bytes(), &eventsResp))
	events := eventsResp["events"].([]any)
	is.Equal(len(events), 1)
	event := events[0].(map[string]any)
	is.Equal(event["node_id"], nodeID)
	is.Equal(event["action"], "validated")
	is.Equal(event["source_id"], "moderator:alice")

	reqTrust := httptest.NewRequest("GET", "/v1/namespaces/channel:general/sources/moderator:alice/trust", nil)
	wTrust := httptest.NewRecorder()
	handler.ServeHTTP(wTrust, reqTrust)

	is.Equal(wTrust.Code, http.StatusOK)
	var trustResp map[string]any
	is.NoErr(json.Unmarshal(wTrust.Body.Bytes(), &trustResp))
	is.Equal(trustResp["source_id"], "moderator:alice")
	points := trustResp["points"].([]any)
	is.Equal(len(points), 1)
	point := points[0].(map[string]any)
	is.Equal(point["action"], "validated")
	is.True(point["source_credibility"].(float64) > 0.5)

	reqQueue := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/queue?low_confidence_threshold=0.99", nil)
	wQueue := httptest.NewRecorder()
	handler.ServeHTTP(wQueue, reqQueue)

	is.Equal(wQueue.Code, http.StatusOK)
	var queueResp map[string]any
	is.NoErr(json.Unmarshal(wQueue.Body.Bytes(), &queueResp))
	items := queueResp["items"].([]any)
	is.True(len(items) > 0)
	queueItem := items[0].(map[string]any)
	is.True(queueItem["type"] != "")
	reviewID := queueItem["id"].(string)

	decisionBody, _ := json.Marshal(map[string]any{
		"review_id": reviewID,
		"status":    "assigned",
		"owner":     "alice",
		"decision":  "needs_evidence",
		"note":      "check logs",
	})
	reqDecision := httptest.NewRequest("POST", "/v1/namespaces/channel:general/review/decisions", bytes.NewReader(decisionBody))
	reqDecision.Header.Set("Content-Type", "application/json")
	wDecision := httptest.NewRecorder()
	handler.ServeHTTP(wDecision, reqDecision)
	is.Equal(wDecision.Code, http.StatusOK)
	var decisionResp map[string]any
	is.NoErr(json.Unmarshal(wDecision.Body.Bytes(), &decisionResp))
	is.Equal(decisionResp["review_id"], reviewID)
	is.Equal(decisionResp["status"], "assigned")

	reqDecisions := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/decisions", nil)
	wDecisions := httptest.NewRecorder()
	handler.ServeHTTP(wDecisions, reqDecisions)
	is.Equal(wDecisions.Code, http.StatusOK)
	var decisionsResp map[string]any
	is.NoErr(json.Unmarshal(wDecisions.Body.Bytes(), &decisionsResp))
	decisions := decisionsResp["decisions"].([]any)
	is.Equal(len(decisions), 1)
	is.Equal(decisions[0].(map[string]any)["owner"], "alice")

	reqFilteredQueue := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/queue?low_confidence_threshold=0.99&type="+queueItem["type"].(string)+"&status=assigned&owner=alice", nil)
	wFilteredQueue := httptest.NewRecorder()
	handler.ServeHTTP(wFilteredQueue, reqFilteredQueue)
	is.Equal(wFilteredQueue.Code, http.StatusOK)
	var filteredQueueResp map[string]any
	is.NoErr(json.Unmarshal(wFilteredQueue.Body.Bytes(), &filteredQueueResp))
	filteredItems := filteredQueueResp["items"].([]any)
	is.Equal(len(filteredItems), 1)
	is.Equal(filteredItems[0].(map[string]any)["id"], reviewID)

	time.Sleep(5 * time.Millisecond)
	reqEscalations := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/escalations?low_confidence_threshold=0.99&status=assigned&owner=alice&escalation_after_hours=0.000000001", nil)
	wEscalations := httptest.NewRecorder()
	handler.ServeHTTP(wEscalations, reqEscalations)
	is.Equal(wEscalations.Code, http.StatusOK)
	var escalationsResp map[string]any
	is.NoErr(json.Unmarshal(wEscalations.Body.Bytes(), &escalationsResp))
	digest := escalationsResp["digest"].(map[string]any)
	is.Equal(digest["total_escalated"], float64(1))
	groups := digest["groups"].([]any)
	is.Equal(len(groups), 1)
	group := groups[0].(map[string]any)
	is.Equal(group["owner"], "alice")
	is.Equal(group["count"], float64(1))
	is.Equal(group["escalation_level"], "review_overdue")

	digestBody, _ := json.Marshal(map[string]any{
		"low_confidence_threshold": 0.99,
		"status":                   "assigned",
		"owner":                    "alice",
		"escalation_after_hours":   0.000000001,
		"note":                     "weekly handoff",
	})
	reqRecordDigest := httptest.NewRequest("POST", "/v1/namespaces/channel:general/review/escalation-digests", bytes.NewReader(digestBody))
	reqRecordDigest.Header.Set("Content-Type", "application/json")
	wRecordDigest := httptest.NewRecorder()
	handler.ServeHTTP(wRecordDigest, reqRecordDigest)
	is.Equal(wRecordDigest.Code, http.StatusOK)
	var recordDigestResp map[string]any
	is.NoErr(json.Unmarshal(wRecordDigest.Body.Bytes(), &recordDigestResp))
	is.Equal(recordDigestResp["note"], "weekly handoff")
	is.Equal(recordDigestResp["total_escalated"], float64(1))

	reqSavedDigests := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/escalation-digests", nil)
	wSavedDigests := httptest.NewRecorder()
	handler.ServeHTTP(wSavedDigests, reqSavedDigests)
	is.Equal(wSavedDigests.Code, http.StatusOK)
	var savedDigestsResp map[string]any
	is.NoErr(json.Unmarshal(wSavedDigests.Body.Bytes(), &savedDigestsResp))
	savedDigests := savedDigestsResp["digests"].([]any)
	is.Equal(len(savedDigests), 1)
	is.Equal(savedDigests[0].(map[string]any)["note"], "weekly handoff")

	reqHandoffs := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoffs?owner=alice&escalation_level=review_overdue", nil)
	wHandoffs := httptest.NewRecorder()
	handler.ServeHTTP(wHandoffs, reqHandoffs)
	is.Equal(wHandoffs.Code, http.StatusOK)
	var handoffsResp map[string]any
	is.NoErr(json.Unmarshal(wHandoffs.Body.Bytes(), &handoffsResp))
	handoffs := handoffsResp["handoffs"].([]any)
	is.Equal(len(handoffs), 1)
	is.Equal(handoffs[0].(map[string]any)["note"], "weekly handoff")

	webhookBody, _ := json.Marshal(map[string]any{
		"owner":            "alice",
		"escalation_level": "review_overdue",
		"target_url":       "https://ops.example.test/contextdb/handoffs",
		"secret":           "test-secret",
	})
	reqWebhookPlan := httptest.NewRequest("POST", "/v1/namespaces/channel:general/review/handoff-webhooks/plan", bytes.NewReader(webhookBody))
	reqWebhookPlan.Header.Set("Content-Type", "application/json")
	wWebhookPlan := httptest.NewRecorder()
	handler.ServeHTTP(wWebhookPlan, reqWebhookPlan)
	is.Equal(wWebhookPlan.Code, http.StatusOK)
	var webhookPlanResp map[string]any
	is.NoErr(json.Unmarshal(wWebhookPlan.Body.Bytes(), &webhookPlanResp))
	deliveries := webhookPlanResp["deliveries"].([]any)
	is.Equal(len(deliveries), 1)
	delivery := deliveries[0].(map[string]any)
	is.Equal(delivery["target_url"], "https://ops.example.test/contextdb/handoffs")
	is.Equal(delivery["dry_run"], true)
	is.Equal(delivery["method"], "POST")
	is.Equal(delivery["total_escalated"], float64(1))
	is.True(strings.HasPrefix(delivery["signature"].(string), "sha256="))

	receivedWebhook := 0
	webhookTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedWebhook++
		is.Equal(r.Header.Get("X-ContextDB-Delivery-Mode"), "execute")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer webhookTarget.Close()
	deliverBody, _ := json.Marshal(map[string]any{
		"owner":            "alice",
		"escalation_level": "review_overdue",
		"target_url":       webhookTarget.URL,
		"secret":           "test-secret",
		"execute":          true,
		"timeout_ms":       1000,
	})
	reqWebhookDeliver := httptest.NewRequest("POST", "/v1/namespaces/channel:general/review/handoff-webhooks/deliver", bytes.NewReader(deliverBody))
	reqWebhookDeliver.Header.Set("Content-Type", "application/json")
	wWebhookDeliver := httptest.NewRecorder()
	handler.ServeHTTP(wWebhookDeliver, reqWebhookDeliver)
	is.Equal(wWebhookDeliver.Code, http.StatusOK)
	var webhookDeliverResp map[string]any
	is.NoErr(json.Unmarshal(wWebhookDeliver.Body.Bytes(), &webhookDeliverResp))
	executedDeliveries := webhookDeliverResp["deliveries"].([]any)
	is.Equal(len(executedDeliveries), 1)
	executedDelivery := executedDeliveries[0].(map[string]any)
	is.Equal(receivedWebhook, 1)
	is.Equal(executedDelivery["dry_run"], false)
	is.Equal(executedDelivery["executed"], true)
	is.Equal(executedDelivery["status_code"], float64(http.StatusAccepted))
	is.Equal(executedDelivery["response_body"], "accepted")
	reqReceipts := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/receipts", nil)
	wReceipts := httptest.NewRecorder()
	handler.ServeHTTP(wReceipts, reqReceipts)
	is.Equal(wReceipts.Code, http.StatusOK)
	var receiptsResp map[string]any
	is.NoErr(json.Unmarshal(wReceipts.Body.Bytes(), &receiptsResp))
	receipts := receiptsResp["receipts"].([]any)
	is.Equal(len(receipts), 1)
	receipt := receipts[0].(map[string]any)
	is.Equal(receipt["target_url"], webhookTarget.URL)
	is.Equal(receipt["success"], true)
	is.Equal(receipt["status_code"], float64(http.StatusAccepted))
	is.True(receipt["payload_sha256"].(string) != "")
	is.True(receipt["response_sha256"].(string) != "")
	failRESTWebhook := true
	failedWebhookTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failRESTWebhook {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("try later"))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("retry accepted"))
	}))
	defer failedWebhookTarget.Close()
	failedDeliverBody, _ := json.Marshal(map[string]any{
		"owner":            "alice",
		"escalation_level": "review_overdue",
		"target_url":       failedWebhookTarget.URL,
		"secret":           "test-secret",
		"execute":          true,
		"timeout_ms":       1000,
	})
	reqFailedWebhookDeliver := httptest.NewRequest("POST", "/v1/namespaces/channel:general/review/handoff-webhooks/deliver", bytes.NewReader(failedDeliverBody))
	reqFailedWebhookDeliver.Header.Set("Content-Type", "application/json")
	wFailedWebhookDeliver := httptest.NewRecorder()
	handler.ServeHTTP(wFailedWebhookDeliver, reqFailedWebhookDeliver)
	is.Equal(wFailedWebhookDeliver.Code, http.StatusOK)
	reqRetryCandidates := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/retry-candidates", nil)
	wRetryCandidates := httptest.NewRecorder()
	handler.ServeHTTP(wRetryCandidates, reqRetryCandidates)
	is.Equal(wRetryCandidates.Code, http.StatusOK)
	var retryCandidatesResp map[string]any
	is.NoErr(json.Unmarshal(wRetryCandidates.Body.Bytes(), &retryCandidatesResp))
	candidates := retryCandidatesResp["candidates"].([]any)
	is.Equal(len(candidates), 1)
	candidate := candidates[0].(map[string]any)
	is.Equal(candidate["target_url"], failedWebhookTarget.URL)
	is.Equal(candidate["attempts"], float64(1))
	is.Equal(candidate["last_status_code"], float64(http.StatusBadGateway))
	is.True(candidate["last_error"].(string) != "")
	reqRetryRecommendations := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/retry-recommendations", nil)
	wRetryRecommendations := httptest.NewRecorder()
	handler.ServeHTTP(wRetryRecommendations, reqRetryRecommendations)
	is.Equal(wRetryRecommendations.Code, http.StatusOK)
	var retryRecommendationsResp map[string]any
	is.NoErr(json.Unmarshal(wRetryRecommendations.Body.Bytes(), &retryRecommendationsResp))
	recommendations := retryRecommendationsResp["recommendations"].([]any)
	is.Equal(len(recommendations), 1)
	recommendation := recommendations[0].(map[string]any)
	is.Equal(recommendation["target_url"], failedWebhookTarget.URL)
	is.Equal(recommendation["attempts"], float64(1))
	is.Equal(recommendation["delay_seconds"], float64(60))
	is.Equal(recommendation["ready"], false)
	is.Equal(recommendation["reason"], "waiting_for_backoff")
	reqRetryFatigue := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/retry-fatigue", nil)
	wRetryFatigue := httptest.NewRecorder()
	handler.ServeHTTP(wRetryFatigue, reqRetryFatigue)
	is.Equal(wRetryFatigue.Code, http.StatusOK)
	var retryFatigueResp map[string]any
	is.NoErr(json.Unmarshal(wRetryFatigue.Body.Bytes(), &retryFatigueResp))
	summaries := retryFatigueResp["summaries"].([]any)
	is.Equal(len(summaries), 1)
	summary := summaries[0].(map[string]any)
	is.Equal(summary["target_url"], failedWebhookTarget.URL)
	is.Equal(summary["candidates"], float64(1))
	is.Equal(summary["total_attempts"], float64(1))
	is.Equal(summary["waiting"], float64(1))
	statusFamilies := summary["status_families"].([]any)
	is.Equal(len(statusFamilies), 1)
	is.Equal(statusFamilies[0].(map[string]any)["family"], "5xx")
	owners := summary["owners"].([]any)
	is.Equal(len(owners), 1)
	is.Equal(owners[0].(map[string]any)["owner"], "alice")
	escalationLevels := summary["escalation_levels"].([]any)
	is.Equal(len(escalationLevels), 1)
	is.Equal(escalationLevels[0].(map[string]any)["escalation_level"], "review_overdue")
	presets := retryFatigueResp["presets"].([]any)
	is.True(len(presets) >= 3)
	is.Equal(presets[0].(map[string]any)["name"], "review-overdue")
	is.Equal(presets[0].(map[string]any)["example_rest_query"], "preset=review-overdue")
	is.Equal(presets[0].(map[string]any)["example_graphql"], `preset: "review-overdue"`)
	reqRetryFatiguePreset := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/retry-fatigue?preset=review-overdue", nil)
	wRetryFatiguePreset := httptest.NewRecorder()
	handler.ServeHTTP(wRetryFatiguePreset, reqRetryFatiguePreset)
	is.Equal(wRetryFatiguePreset.Code, http.StatusOK)
	var retryFatiguePresetResp map[string]any
	is.NoErr(json.Unmarshal(wRetryFatiguePreset.Body.Bytes(), &retryFatiguePresetResp))
	is.Equal(len(retryFatiguePresetResp["summaries"].([]any)), 1)
	reqRetryFatigueFiltered := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/retry-fatigue?owner=bob", nil)
	wRetryFatigueFiltered := httptest.NewRecorder()
	handler.ServeHTTP(wRetryFatigueFiltered, reqRetryFatigueFiltered)
	is.Equal(wRetryFatigueFiltered.Code, http.StatusOK)
	var retryFatigueFilteredResp map[string]any
	is.NoErr(json.Unmarshal(wRetryFatigueFiltered.Body.Bytes(), &retryFatigueFilteredResp))
	is.Equal(len(retryFatigueFilteredResp["summaries"].([]any)), 0)
	reqRetryFatigueMarkdown := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/handoff-webhooks/retry-fatigue?format=markdown", nil)
	wRetryFatigueMarkdown := httptest.NewRecorder()
	handler.ServeHTTP(wRetryFatigueMarkdown, reqRetryFatigueMarkdown)
	is.Equal(wRetryFatigueMarkdown.Code, http.StatusOK)
	is.True(strings.Contains(wRetryFatigueMarkdown.Header().Get("Content-Type"), "text/markdown"))
	is.True(strings.Contains(wRetryFatigueMarkdown.Body.String(), "# Review Handoff Retry Fatigue"))
	is.True(strings.Contains(wRetryFatigueMarkdown.Body.String(), failedWebhookTarget.URL))
	is.True(strings.Contains(wRetryFatigueMarkdown.Body.String(), "alice=1"))
	is.True(strings.Contains(wRetryFatigueMarkdown.Body.String(), "review_overdue=1"))
	failRESTWebhook = false
	retryBody, _ := json.Marshal(map[string]any{
		"digest_event_id": candidate["digest_event_id"],
		"target_url":      failedWebhookTarget.URL,
		"secret":          "test-secret",
		"execute":         true,
		"timeout_ms":      1000,
	})
	reqRetryWebhook := httptest.NewRequest("POST", "/v1/namespaces/channel:general/review/handoff-webhooks/retry", bytes.NewReader(retryBody))
	reqRetryWebhook.Header.Set("Content-Type", "application/json")
	wRetryWebhook := httptest.NewRecorder()
	handler.ServeHTTP(wRetryWebhook, reqRetryWebhook)
	is.Equal(wRetryWebhook.Code, http.StatusOK)
	var retryResp map[string]any
	is.NoErr(json.Unmarshal(wRetryWebhook.Body.Bytes(), &retryResp))
	retryDelivery := retryResp["delivery"].(map[string]any)
	is.Equal(retryDelivery["executed"], true)
	is.Equal(retryDelivery["status_code"], float64(http.StatusAccepted))
	is.Equal(retryDelivery["response_body"], "retry accepted")

	refuteBody, _ := json.Marshal(map[string]any{"reason": "audit contradicted source"})
	reqRefute := httptest.NewRequest("POST", "/v1/namespaces/channel:general/nodes/"+nodeID+"/refute", bytes.NewReader(refuteBody))
	reqRefute.Header.Set("Content-Type", "application/json")
	wRefute := httptest.NewRecorder()
	handler.ServeHTTP(wRefute, reqRefute)
	is.Equal(wRefute.Code, http.StatusOK)

	reqAnomalyQueue := httptest.NewRequest("GET", "/v1/namespaces/channel:general/review/queue?source_trust_drop_threshold=0.1&type=source_trust_anomaly&source_id=moderator:alice", nil)
	wAnomalyQueue := httptest.NewRecorder()
	handler.ServeHTTP(wAnomalyQueue, reqAnomalyQueue)
	is.Equal(wAnomalyQueue.Code, http.StatusOK)
	var anomalyQueueResp map[string]any
	is.NoErr(json.Unmarshal(wAnomalyQueue.Body.Bytes(), &anomalyQueueResp))
	anomalyItems := anomalyQueueResp["items"].([]any)
	foundAnomaly := false
	for _, raw := range anomalyItems {
		item := raw.(map[string]any)
		if item["type"] == "source_trust_anomaly" {
			foundAnomaly = true
			is.Equal(item["source_id"], "moderator:alice")
		}
	}
	is.True(foundAnomaly)

	req4 := httptest.NewRequest("GET", "/v1/namespaces/channel:general/nodes/"+nodeID+"/narrative", nil)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, req4)

	is.Equal(w4.Code, http.StatusOK)
	var narrativeResp map[string]any
	is.NoErr(json.Unmarshal(w4.Body.Bytes(), &narrativeResp))
	is.True(narrativeResp["summary"].(string) != "")

	gapBody, _ := json.Marshal(map[string]any{"max_gaps": 2})
	req5 := httptest.NewRequest("POST", "/v1/namespaces/channel:general/gaps", bytes.NewReader(gapBody))
	req5.Header.Set("Content-Type", "application/json")
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, req5)

	is.Equal(w5.Code, http.StatusOK)
	var gapResp map[string]any
	is.NoErr(json.Unmarshal(w5.Body.Bytes(), &gapResp))
	is.Equal(gapResp["total_nodes"], float64(2))

	planBody, _ := json.Marshal(map[string]any{"budget": 3})
	reqPlan := httptest.NewRequest("POST", "/v1/namespaces/channel:general/acquisition/plan", bytes.NewReader(planBody))
	reqPlan.Header.Set("Content-Type", "application/json")
	wPlan := httptest.NewRecorder()
	handler.ServeHTTP(wPlan, reqPlan)
	is.Equal(wPlan.Code, http.StatusOK)
	var planResp map[string]any
	is.NoErr(json.Unmarshal(wPlan.Body.Bytes(), &planResp))
	planTasks := planResp["tasks"].([]any)
	is.True(len(planTasks) > 0)
	is.True(planTasks[0].(map[string]any)["prompt"].(string) != "")
}

func TestRESTServer_InvalidFeedbackNodeIDReturnsBadRequest(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	feedbackBody, _ := json.Marshal(map[string]any{"reason": "bad id"})
	req := httptest.NewRequest("POST", "/v1/namespaces/channel:general/nodes/not-a-uuid/validate", bytes.NewReader(feedbackBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusBadRequest)
}

func TestGRPCService_WriteRetrieveFeedbackContract(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{})
	defer db.Close()

	addr := startGRPCTestServer(t, db)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(server.GRPCCodec{})),
	)
	is.NoErr(err)
	defer conn.Close()

	var writeResp server.GRPCWriteResponse
	err = conn.Invoke(ctx, "/contextdb.v1.ContextDB/Write", &server.GRPCWriteRequest{
		Namespace:     "grpc-contract",
		NamespaceMode: "general",
		Content:       "gRPC contract returns score breakdown",
		SourceID:      "docs",
		Labels:        []string{"Fact"},
		Vector:        []float32{0.9, 0.1, 0.0, 0.0},
		Confidence:    0.7,
	}, &writeResp)
	is.NoErr(err)
	is.True(writeResp.Admitted)
	is.True(writeResp.NodeID != "")

	var retrieveResp server.GRPCRetrieveResponse
	err = conn.Invoke(ctx, "/contextdb.v1.ContextDB/Retrieve", &server.GRPCRetrieveRequest{
		Namespace: "grpc-contract",
		Vector:    []float32{0.9, 0.1, 0.0, 0.0},
		TopK:      3,
	}, &retrieveResp)
	is.NoErr(err)
	is.True(len(retrieveResp.Results) > 0)
	is.Equal(retrieveResp.Results[0].ID, writeResp.NodeID)
	is.True(retrieveResp.Results[0].Score > 0)
	is.True(retrieveResp.Results[0].ScoreBreakdown.Similarity > 0)

	var feedbackResp server.GRPCFeedbackResponse
	err = conn.Invoke(ctx, "/contextdb.v1.ContextDB/ValidateClaim", &server.GRPCFeedbackRequest{
		Namespace:     "grpc-contract",
		NamespaceMode: "general",
		NodeID:        writeResp.NodeID,
		Reason:        "verified through gRPC contract test",
	}, &feedbackResp)
	is.NoErr(err)
	is.Equal(feedbackResp.Action, "validated")
	is.Equal(feedbackResp.NodeID, writeResp.NodeID)
	is.Equal(feedbackResp.SourceID, "docs")
	is.True(feedbackResp.SourceCredibility > 0.5)
}

func TestGraphQLServer_SearchResolvesNodesAndSources(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	writeBody, _ := json.Marshal(map[string]any{
		"content":   "Go uses goroutines for concurrency",
		"source_id": "docs",
		"labels":    []string{"Fact"},
	})
	req := httptest.NewRequest("POST", "/v1/namespaces/graphql-test/write", bytes.NewReader(writeBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	var writeResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &writeResp))
	nodeID := writeResp["node_id"].(string)

	writeOtherBody, _ := json.Marshal(map[string]any{
		"content":    "Go uses only operating system threads",
		"source_id":  "chat",
		"labels":     []string{"Fact"},
		"confidence": 0.2,
	})
	req = httptest.NewRequest("POST", "/v1/namespaces/graphql-test/write", bytes.NewReader(writeOtherBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	var writeOtherResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &writeOtherResp))
	otherNodeID := writeOtherResp["node_id"].(string)
	graph, _, _, _ := db.Stores()
	supportUUID := uuid.New()
	nodeUUID, err := uuid.Parse(nodeID)
	is.NoErr(err)
	is.NoErr(graph.UpsertNode(context.Background(), core.Node{
		ID:         supportUUID,
		Namespace:  "graphql-test",
		Properties: map[string]any{"text": "Docs confirm goroutines are the Go concurrency primitive", "source_id": "docs"},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(time.Hour),
		TxTime:     time.Now(),
	}))
	is.NoErr(graph.UpsertEdge(context.Background(), core.Edge{
		ID:        uuid.New(),
		Namespace: "graphql-test",
		Src:       supportUUID,
		Dst:       nodeUUID,
		Type:      core.EdgeSupports,
		Weight:    0.8,
	}))

	queryBody, _ := json.Marshal(map[string]any{
		"query": `{
			search(namespace: "graphql-test", query: "goroutines", limit: 5) {
				totalCount
				nodes {
					id
					content
					score
					scoreBreakdown { similarity }
					sources { name effectiveCredibility }
				}
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var resp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql errors: %v", errs)
	}
	data := resp["data"].(map[string]any)
	search := data["search"].(map[string]any)
	is.Equal(search["totalCount"], float64(1))
	nodes := search["nodes"].([]any)
	node := nodes[0].(map[string]any)
	is.Equal(node["content"], "Go uses goroutines for concurrency")
	sources := node["sources"].([]any)
	is.Equal(sources[0].(map[string]any)["name"], "docs")

	mutationBody, _ := json.Marshal(map[string]any{
		"query": `mutation($id: ID!) {
			validateClaim(namespace: "graphql-test", nodeId: $id) {
				nodeId
				action
				confidence
				sourceCredibility
			}
		}`,
		"variables": map[string]any{"id": nodeID},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(mutationBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql mutation errors: %v", errs)
	}
	mutationData := resp["data"].(map[string]any)
	feedback := mutationData["validateClaim"].(map[string]any)
	is.Equal(feedback["action"], "validated")
	is.True(feedback["sourceCredibility"].(float64) > 0.5)

	reviewID := "low_confidence:" + nodeID
	reviewMutationBody, _ := json.Marshal(map[string]any{
		"query": `mutation($reviewID: ID!) {
			recordReviewDecision(
				namespace: "graphql-test"
				reviewId: $reviewID
				status: "assigned"
				owner: "alice"
				decision: "needs_evidence"
				note: "check source"
			) {
				reviewId
				status
				owner
				decision
			}
		}`,
		"variables": map[string]any{"reviewID": reviewID},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(reviewMutationBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql review mutation errors: %v", errs)
	}
	reviewMutation := resp["data"].(map[string]any)["recordReviewDecision"].(map[string]any)
	is.Equal(reviewMutation["reviewId"], reviewID)
	is.Equal(reviewMutation["status"], "assigned")

	refuteMutationBody, _ := json.Marshal(map[string]any{
		"query": `mutation($id: ID!) {
			refuteClaim(namespace: "graphql-test", nodeId: $id, reason: "audit contradicted source") {
				action
				sourceCredibility
			}
		}`,
		"variables": map[string]any{"id": nodeID},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(refuteMutationBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql refute mutation errors: %v", errs)
	}
	refuteMutation := resp["data"].(map[string]any)["refuteClaim"].(map[string]any)
	is.Equal(refuteMutation["action"], "refuted")

	recordDigestBody, _ := json.Marshal(map[string]any{
		"query": `mutation {
			recordReviewEscalationDigest(
				namespace: "graphql-test"
				lowConfidenceThreshold: 0.99
				status: "assigned"
				owner: "alice"
				escalationAfterHours: 0.000000001
				note: "graphql handoff"
			) {
				totalEscalated
				groups { owner count escalationLevel }
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(recordDigestBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql record escalation digest errors: %v", errs)
	}
	recordDigest := resp["data"].(map[string]any)["recordReviewEscalationDigest"].(map[string]any)
	is.Equal(recordDigest["totalEscalated"], float64(1))

	graphQLWebhookCalls := 0
	graphQLWebhookTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphQLWebhookCalls++
		is.Equal(r.Header.Get("X-ContextDB-Delivery-Mode"), "execute")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer graphQLWebhookTarget.Close()
	deliverWebhookBody, _ := json.Marshal(map[string]any{
		"query": `mutation($target: String!) {
			deliverReviewHandoffWebhook(
				namespace: "graphql-test"
				owner: "alice"
				escalationLevel: "review_overdue"
				targetUrl: $target
				secret: "test-secret"
				execute: true
				timeoutMs: 1000
			) {
				targetUrl
				dryRun
				executed
				statusCode
				responseBody
				error
			}
		}`,
		"variables": map[string]any{"target": graphQLWebhookTarget.URL},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(deliverWebhookBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql deliver handoff webhook errors: %v", errs)
	}
	graphQLDeliveries := resp["data"].(map[string]any)["deliverReviewHandoffWebhook"].([]any)
	is.Equal(len(graphQLDeliveries), 1)
	graphQLExecutedDelivery := graphQLDeliveries[0].(map[string]any)
	is.Equal(graphQLWebhookCalls, 1)
	is.Equal(graphQLExecutedDelivery["dryRun"], false)
	is.Equal(graphQLExecutedDelivery["executed"], true)
	is.Equal(graphQLExecutedDelivery["statusCode"], float64(http.StatusAccepted))
	is.Equal(graphQLExecutedDelivery["responseBody"], "accepted")

	failGraphQLWebhook := true
	graphQLRetryTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Header.Get("X-ContextDB-Delivery-Mode"), "execute")
		if failGraphQLWebhook {
			http.Error(w, "retry later", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("retry accepted"))
	}))
	defer graphQLRetryTarget.Close()
	failedWebhookBody, _ := json.Marshal(map[string]any{
		"query": `mutation($target: String!) {
			deliverReviewHandoffWebhook(
				namespace: "graphql-test"
				owner: "alice"
				escalationLevel: "review_overdue"
				targetUrl: $target
				secret: "test-secret"
				execute: true
				timeoutMs: 1000
			) {
				statusCode
				error
			}
		}`,
		"variables": map[string]any{"target": graphQLRetryTarget.URL},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(failedWebhookBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql failed handoff webhook errors: %v", errs)
	}
	failedDeliveries := resp["data"].(map[string]any)["deliverReviewHandoffWebhook"].([]any)
	is.Equal(failedDeliveries[0].(map[string]any)["statusCode"], float64(http.StatusBadGateway))

	retryCandidatesBody, _ := json.Marshal(map[string]any{
		"query": `query {
			reviewHandoffRetryCandidates(namespace: "graphql-test") {
				digestEventId
				targetUrl
				attempts
				lastStatusCode
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(retryCandidatesBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql retry candidate errors: %v", errs)
	}
	retryCandidates := resp["data"].(map[string]any)["reviewHandoffRetryCandidates"].([]any)
	is.Equal(len(retryCandidates), 1)
	retryCandidate := retryCandidates[0].(map[string]any)
	is.Equal(retryCandidate["targetUrl"], graphQLRetryTarget.URL)
	is.Equal(retryCandidate["attempts"], float64(1))
	is.Equal(retryCandidate["lastStatusCode"], float64(http.StatusBadGateway))
	retryRecommendationsBody, _ := json.Marshal(map[string]any{
		"query": `query {
			reviewHandoffRetryRecommendations(namespace: "graphql-test") {
				digestEventId
				targetUrl
				attempts
				delaySeconds
				ready
				reason
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(retryRecommendationsBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql retry recommendation errors: %v", errs)
	}
	retryRecommendations := resp["data"].(map[string]any)["reviewHandoffRetryRecommendations"].([]any)
	is.Equal(len(retryRecommendations), 1)
	retryRecommendation := retryRecommendations[0].(map[string]any)
	is.Equal(retryRecommendation["digestEventId"], retryCandidate["digestEventId"])
	is.Equal(retryRecommendation["targetUrl"], graphQLRetryTarget.URL)
	is.Equal(retryRecommendation["attempts"], float64(1))
	is.Equal(retryRecommendation["delaySeconds"], float64(60))
	is.Equal(retryRecommendation["ready"], false)
	is.Equal(retryRecommendation["reason"], "waiting_for_backoff")
	retryFatigueBody, _ := json.Marshal(map[string]any{
		"query": `query {
			reviewHandoffRetryFatigue(namespace: "graphql-test") {
				targetUrl
				candidates
				totalAttempts
				ready
				waiting
				statusFamilies { family count }
				owners { owner count }
				escalationLevels { escalationLevel count }
				lastStatusCode
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(retryFatigueBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql retry fatigue errors: %v", errs)
	}
	retryFatigue := resp["data"].(map[string]any)["reviewHandoffRetryFatigue"].([]any)
	is.Equal(len(retryFatigue), 1)
	retryFatigueSummary := retryFatigue[0].(map[string]any)
	is.Equal(retryFatigueSummary["targetUrl"], graphQLRetryTarget.URL)
	is.Equal(retryFatigueSummary["candidates"], float64(1))
	is.Equal(retryFatigueSummary["totalAttempts"], float64(1))
	is.Equal(retryFatigueSummary["waiting"], float64(1))
	is.Equal(retryFatigueSummary["lastStatusCode"], float64(http.StatusBadGateway))
	retryFatigueStatusFamilies := retryFatigueSummary["statusFamilies"].([]any)
	is.Equal(retryFatigueStatusFamilies[0].(map[string]any)["family"], "5xx")
	retryFatigueOwners := retryFatigueSummary["owners"].([]any)
	is.Equal(retryFatigueOwners[0].(map[string]any)["owner"], "alice")
	retryFatigueEscalations := retryFatigueSummary["escalationLevels"].([]any)
	is.Equal(retryFatigueEscalations[0].(map[string]any)["escalationLevel"], "review_overdue")
	retryFatigueFilteredBody, _ := json.Marshal(map[string]any{
		"query": `query {
			reviewHandoffRetryFatigue(namespace: "graphql-test", owner: "bob") {
				targetUrl
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(retryFatigueFilteredBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql retry fatigue filtered errors: %v", errs)
	}
	retryFatigueFiltered := resp["data"].(map[string]any)["reviewHandoffRetryFatigue"].([]any)
	is.Equal(len(retryFatigueFiltered), 0)
	retryFatiguePresetBody, _ := json.Marshal(map[string]any{
		"query": `query {
			reviewHandoffRetryFatigue(namespace: "graphql-test", preset: "review-overdue") {
				targetUrl
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(retryFatiguePresetBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql retry fatigue preset errors: %v", errs)
	}
	retryFatiguePreset := resp["data"].(map[string]any)["reviewHandoffRetryFatigue"].([]any)
	is.Equal(len(retryFatiguePreset), 1)
	failGraphQLWebhook = false

	retryWebhookBody, _ := json.Marshal(map[string]any{
		"query": `mutation($digestEventId: ID!, $target: String!) {
			retryReviewHandoffWebhook(
				namespace: "graphql-test"
				digestEventId: $digestEventId
				targetUrl: $target
				secret: "test-secret"
				execute: true
				timeoutMs: 1000
			) {
				targetUrl
				executed
				statusCode
				responseBody
			}
		}`,
		"variables": map[string]any{
			"digestEventId": retryCandidate["digestEventId"],
			"target":        graphQLRetryTarget.URL,
		},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(retryWebhookBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql retry handoff webhook errors: %v", errs)
	}
	retryDelivery := resp["data"].(map[string]any)["retryReviewHandoffWebhook"].(map[string]any)
	is.Equal(retryDelivery["targetUrl"], graphQLRetryTarget.URL)
	is.Equal(retryDelivery["executed"], true)
	is.Equal(retryDelivery["statusCode"], float64(http.StatusAccepted))
	is.Equal(retryDelivery["responseBody"], "retry accepted")

	time.Sleep(5 * time.Millisecond)
	queryBody, _ = json.Marshal(map[string]any{
		"query": `query($id: ID!, $otherID: ID!) {
			narrative(namespace: "graphql-test", nodeId: $id) {
				summary
				claim { text }
			}
			knowledgeGaps(namespace: "graphql-test", maxGaps: 2) {
				totalNodes
				gapsDetected
			}
			acquisitionPlan(namespace: "graphql-test", budget: 3) {
				totalNodes
				tasks { type prompt }
			}
			feedbackEvents(namespace: "graphql-test") {
				nodeId
				action
				sourceId
				sourceCredibility
			}
			sourceTrustTimeline(namespace: "graphql-test", sourceId: "docs") {
				nodeId
				action
				sourceCredibility
			}
			explainRank(namespace: "graphql-test", nodeId: $id, otherNodeId: $otherID) {
				winnerNodeId
				summary
				node { evidence { supportCount links { nodeId edgeWeight } } }
				factors { factor delta }
			}
			reviewQueue(namespace: "graphql-test", lowConfidenceThreshold: 0.99, sourceTrustDropThreshold: 0.1) {
				id
				type
				nodeId
				sourceId
				action
				status
				owner
				suggestedAction
			}
			assignedReviews: reviewQueue(namespace: "graphql-test", lowConfidenceThreshold: 0.99, types: ["low_confidence"], status: "assigned", owner: "alice") {
				id
				type
				status
				owner
			}
			reviewEscalationDigest(namespace: "graphql-test", lowConfidenceThreshold: 0.99, status: "assigned", owner: "alice", escalationAfterHours: 0.000000001) {
				totalEscalated
				groups {
					owner
					type
					escalationLevel
					count
					reviewIds
				}
			}
			reviewEscalationDigests(namespace: "graphql-test") {
				note
				totalEscalated
			}
			reviewHandoffs(namespace: "graphql-test", owner: "alice", escalationLevel: "review_overdue") {
				note
				totalEscalated
				groups { owner escalationLevel count }
			}
			reviewHandoffWebhookPlan(namespace: "graphql-test", owner: "alice", escalationLevel: "review_overdue", targetUrl: "https://ops.example.test/contextdb/handoffs", secret: "test-secret") {
				targetUrl
				method
				dryRun
				totalEscalated
				payloadSha256
				signature
				maxAttempts
			}
			reviewHandoffDeliveryReceipts(namespace: "graphql-test") {
				targetUrl
				success
				statusCode
				payloadSha256
				responseSha256
				error
			}
			reviewHandoffRetryCandidates(namespace: "graphql-test") {
				targetUrl
				attempts
				lastStatusCode
				payloadSha256
				lastError
			}
			reviewHandoffRetryRecommendations(namespace: "graphql-test") {
				targetUrl
				ready
				reason
			}
			sourceAnomalies: reviewQueue(namespace: "graphql-test", sourceTrustDropThreshold: 0.1, types: ["source_trust_anomaly"], sourceId: "docs") {
				id
				type
				sourceId
				action
			}
			reviewDecisions(namespace: "graphql-test") {
				reviewId
				status
				owner
				decision
			}
		}`,
		"variables": map[string]any{"id": nodeID, "otherID": otherNodeID},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql narrative/gaps errors: %v", errs)
	}
	data = resp["data"].(map[string]any)
	narrative := data["narrative"].(map[string]any)
	is.Equal(narrative["claim"].(map[string]any)["text"], "Go uses goroutines for concurrency")
	gaps := data["knowledgeGaps"].(map[string]any)
	is.Equal(gaps["totalNodes"], float64(2))
	plan := data["acquisitionPlan"].(map[string]any)
	is.Equal(plan["totalNodes"], float64(2))
	is.True(len(plan["tasks"].([]any)) > 0)
	events := data["feedbackEvents"].([]any)
	is.Equal(len(events), 2)
	event := events[0].(map[string]any)
	is.Equal(event["nodeId"], nodeID)
	is.Equal(event["action"], "validated")
	is.Equal(event["sourceId"], "docs")
	points := data["sourceTrustTimeline"].([]any)
	is.Equal(len(points), 2)
	point := points[0].(map[string]any)
	is.Equal(point["nodeId"], nodeID)
	is.Equal(point["action"], "validated")
	is.True(point["sourceCredibility"].(float64) > 0.5)
	rank := data["explainRank"].(map[string]any)
	is.True(rank["winnerNodeId"].(string) != "")
	is.True(rank["summary"].(string) != "")
	is.True(len(rank["factors"].([]any)) == 4)
	rankNode := rank["node"].(map[string]any)
	rankEvidence := rankNode["evidence"].(map[string]any)
	is.Equal(rankEvidence["supportCount"], float64(1))
	queue := data["reviewQueue"].([]any)
	is.True(len(queue) > 0)
	var queueItem map[string]any
	for _, raw := range queue {
		candidate := raw.(map[string]any)
		if candidate["id"] == reviewID {
			queueItem = candidate
			break
		}
	}
	is.True(queueItem != nil)
	is.True(queueItem["type"] != "")
	is.Equal(queueItem["id"], reviewID)
	is.Equal(queueItem["status"], "assigned")
	is.Equal(queueItem["owner"], "alice")
	assignedReviews := data["assignedReviews"].([]any)
	is.Equal(len(assignedReviews), 1)
	assignedReview := assignedReviews[0].(map[string]any)
	is.Equal(assignedReview["id"], reviewID)
	is.Equal(assignedReview["status"], "assigned")
	digest := data["reviewEscalationDigest"].(map[string]any)
	is.Equal(digest["totalEscalated"], float64(1))
	groups := digest["groups"].([]any)
	is.Equal(len(groups), 1)
	group := groups[0].(map[string]any)
	is.Equal(group["owner"], "alice")
	is.Equal(group["type"], "low_confidence")
	is.Equal(group["escalationLevel"], "review_overdue")
	is.Equal(group["count"], float64(1))
	savedGraphQLDigests := data["reviewEscalationDigests"].([]any)
	is.Equal(len(savedGraphQLDigests), 1)
	is.Equal(savedGraphQLDigests[0].(map[string]any)["note"], "graphql handoff")
	graphQLHandoffs := data["reviewHandoffs"].([]any)
	is.Equal(len(graphQLHandoffs), 1)
	is.Equal(graphQLHandoffs[0].(map[string]any)["note"], "graphql handoff")
	graphQLWebhookPlan := data["reviewHandoffWebhookPlan"].([]any)
	is.Equal(len(graphQLWebhookPlan), 1)
	graphQLDelivery := graphQLWebhookPlan[0].(map[string]any)
	is.Equal(graphQLDelivery["targetUrl"], "https://ops.example.test/contextdb/handoffs")
	is.Equal(graphQLDelivery["dryRun"], true)
	is.Equal(graphQLDelivery["method"], "POST")
	is.Equal(graphQLDelivery["totalEscalated"], float64(1))
	is.True(strings.HasPrefix(graphQLDelivery["signature"].(string), "sha256="))
	is.Equal(graphQLDelivery["maxAttempts"], float64(3))
	graphQLReceipts := data["reviewHandoffDeliveryReceipts"].([]any)
	is.Equal(len(graphQLReceipts), 3)
	var graphQLReceipt map[string]any
	for _, raw := range graphQLReceipts {
		receipt := raw.(map[string]any)
		if receipt["targetUrl"] == graphQLWebhookTarget.URL {
			graphQLReceipt = receipt
			break
		}
	}
	is.True(graphQLReceipt != nil)
	is.Equal(graphQLReceipt["targetUrl"], graphQLWebhookTarget.URL)
	is.Equal(graphQLReceipt["success"], true)
	is.Equal(graphQLReceipt["statusCode"], float64(http.StatusAccepted))
	is.True(graphQLReceipt["payloadSha256"].(string) != "")
	is.True(graphQLReceipt["responseSha256"].(string) != "")
	graphQLRetryCandidates := data["reviewHandoffRetryCandidates"].([]any)
	is.Equal(len(graphQLRetryCandidates), 0)
	graphQLRetryRecommendations := data["reviewHandoffRetryRecommendations"].([]any)
	is.Equal(len(graphQLRetryRecommendations), 0)
	foundAnomaly := false
	for _, raw := range queue {
		candidate := raw.(map[string]any)
		if candidate["type"] == "source_trust_anomaly" {
			foundAnomaly = true
			is.Equal(candidate["sourceId"], "docs")
			is.Equal(candidate["action"], "credibility_drop")
		}
	}
	is.True(foundAnomaly)
	sourceAnomalies := data["sourceAnomalies"].([]any)
	is.Equal(len(sourceAnomalies), 1)
	sourceAnomaly := sourceAnomalies[0].(map[string]any)
	is.Equal(sourceAnomaly["type"], "source_trust_anomaly")
	is.Equal(sourceAnomaly["sourceId"], "docs")
	reviewDecisions := data["reviewDecisions"].([]any)
	is.Equal(len(reviewDecisions), 1)
	reviewDecision := reviewDecisions[0].(map[string]any)
	is.Equal(reviewDecision["reviewId"], reviewID)
	is.Equal(reviewDecision["decision"], "needs_evidence")
}

func TestGraphQLServer_Introspection(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	queryBody, _ := json.Marshal(map[string]any{
		"query": `{
			version {
				version
				apiVersion
				latestMigration
				features { name since status }
				migrations { version name }
			}
			features { name }
			migrations { version }
		}`,
	})
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var resp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql introspection errors: %v", errs)
	}
	data := resp["data"].(map[string]any)
	version := data["version"].(map[string]any)
	is.Equal(version["version"], buildinfo.Version)
	is.Equal(version["apiVersion"], "v1")
	is.Equal(version["latestMigration"], float64(2))
	is.True(len(version["features"].([]any)) > 0)
	is.Equal(len(version["migrations"].([]any)), 2)
	is.True(len(data["features"].([]any)) > 0)
	is.Equal(len(data["migrations"].([]any)), 2)
}

func TestRESTServer_Stats(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/v1/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
}

func TestTenantMiddleware(t *testing.T) {
	is := is.New(t)

	var gotTenant string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = server.TenantFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := server.TenantMiddleware(inner)

	// Via X-Tenant-ID header
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "acme-corp")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(gotTenant, "acme-corp")

	// Via Bearer token
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer tenant123:secrettoken")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	is.Equal(gotTenant, "tenant123")
}
