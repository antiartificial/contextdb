"""Tests for the contextdb Python SDK.

These tests require a running contextdb server at localhost:7701.
Start one with: make run
"""

import asyncio
import json
import os

import httpx
import pytest

from contextdb import ContextDB
from contextdb.async_client import AsyncNamespace
from contextdb.client import Namespace


SERVER_URL = os.environ.get("CONTEXTDB_URL", "http://localhost:7701")


@pytest.fixture
def db():
    client = ContextDB(SERVER_URL)
    yield client
    client.close()


@pytest.mark.skipif(
    os.environ.get("CONTEXTDB_INTEGRATION") != "1",
    reason="Set CONTEXTDB_INTEGRATION=1 and start server to run",
)
class TestContextDBIntegration:
    def test_ping(self, db):
        result = db.ping()
        assert result["status"] == "ok"

    def test_stats(self, db):
        result = db.stats()
        assert "Mode" in result or "mode" in result

    def test_write_and_retrieve(self, db):
        ns = db.namespace("py-test", mode="general")

        # Write
        result = ns.write(
            content="Python SDK works",
            source_id="pytest",
            labels=["Claim"],
            vector=[0.1, 0.2, 0.3, 0.4],
        )
        assert result.admitted is True
        assert result.node_id != ""

        # Retrieve
        results = ns.retrieve(
            vector=[0.1, 0.2, 0.3, 0.4],
            top_k=5,
        )
        assert len(results) > 0
        assert results[0].score > 0

    def test_retrieve_with_text(self, db):
        ns = db.namespace("py-test-text", mode="general")

        # Write with vector
        ns.write(
            content="Go is a fast language",
            source_id="test",
            labels=["Claim"],
            vector=[0.5, 0.5, 0.5, 0.5],
        )

        # Retrieve with text (requires server-side embedder)
        results = ns.retrieve(text="What is Go?", top_k=5)
        # Without embedder, this returns empty - that's OK
        assert isinstance(results, list)

    def test_label_filter(self, db):
        ns = db.namespace("py-test-filter", mode="general")

        ns.write(
            content="labeled node",
            source_id="test",
            labels=["Special"],
            vector=[0.3, 0.3, 0.3, 0.3],
        )

        results = ns.retrieve(
            vector=[0.3, 0.3, 0.3, 0.3],
            top_k=5,
            labels=["Special"],
        )
        for r in results:
            assert "Special" in r.labels


def test_acquisition_review_sync_contract_uses_escaped_paths_and_dry_run_default():
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/candidates"):
            return httpx.Response(200, json={"candidates": [{"candidate_id": "candidate/1"}]})
        if request.url.path.endswith("/approve"):
            return httpx.Response(200, json={"decision": {"status": "admitted"}})
        if request.url.path.endswith("/cycle"):
            return httpx.Response(200, json={"dry_run": True})
        if request.url.path.endswith("/runs"):
            return httpx.Response(200, json={"runs": [{"run_id": "run-1"}]})
        return httpx.Response(200, json={"summary": {"review_candidates": 1}})

    transport = httpx.MockTransport(handler)
    with httpx.Client(base_url="https://api.example", transport=transport) as http_client:
        namespace = Namespace(http_client, "team/a b", "belief system")
        assert namespace.acquisition_execution([], execute=True, review_before_admission=True)["summary"]["review_candidates"] == 1
        assert namespace.acquisition_review_candidates() == [{"candidate_id": "candidate/1"}]
        assert namespace.decide_acquisition_candidate("candidate/1", "approve", actor="ana", note="verified")["decision"]["status"] == "admitted"
        assert namespace.run_review_worker()["dry_run"] is True
        assert namespace.review_worker_runs() == [{"run_id": "run-1"}]

    assert requests[0].url.raw_path.decode().startswith("/v1/namespaces/team%2Fa%20b/acquisition/execute")
    assert json.loads(requests[0].content) ["review_before_admission"] is True
    assert json.loads(requests[0].content)["execute"] is True
    assert requests[2].url.raw_path.decode().endswith("/candidates/candidate%2F1/approve")
    assert json.loads(requests[2].content) == {"mode": "belief system", "actor": "ana", "note": "verified"}
    assert json.loads(requests[3].content) == {"execute": False, "limit": 25, "allowed_actions": [], "evaluator": "rules"}


def test_acquisition_review_sync_errors_are_propagated_and_invalid_actions_skip_transport():
    calls = 0

    def handler(_: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(503, json={"error": "unavailable"})

    with httpx.Client(base_url="https://api.example", transport=httpx.MockTransport(handler)) as http_client:
        namespace = Namespace(http_client, "ns", "general")
        with pytest.raises(httpx.HTTPStatusError):
            namespace.acquisition_review_candidates()
        with pytest.raises(ValueError, match="approve or reject"):
            namespace.decide_acquisition_candidate("candidate", "delete")
    assert calls == 1


def test_optional_bearer_token_propagates_to_top_level_and_review_requests():
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"status": "ok", "dry_run": True})

    db = ContextDB("https://api.example", token="secret")
    db._client.close()
    db._client = httpx.Client(base_url="https://api.example", transport=httpx.MockTransport(handler), headers={"Authorization": "Bearer secret"})
    try:
        assert db.ping()["status"] == "ok"
        assert db.namespace("ns").run_review_worker()["dry_run"] is True
    finally:
        db.close()
    assert requests[0].headers["authorization"] == "Bearer secret"
    assert requests[1].headers["authorization"] == "Bearer secret"
    assert requests[1].headers["content-type"] == "application/json"


def test_acquisition_review_async_contract_and_errors():
    async def scenario() -> None:
        requests: list[httpx.Request] = []

        def handler(request: httpx.Request) -> httpx.Response:
            requests.append(request)
            if request.url.path.endswith("/candidates"):
                return httpx.Response(200, json={"candidates": []})
            if request.url.path.endswith("/runs"):
                return httpx.Response(200, json={"runs": []})
            if request.url.path.endswith("/reject"):
                return httpx.Response(503, json={"error": "unavailable"})
            if request.url.path.endswith("/cycle"):
                return httpx.Response(200, json={"dry_run": True})
            return httpx.Response(200, json={"summary": {"review_candidates": 1}})

        async with httpx.AsyncClient(base_url="https://api.example", transport=httpx.MockTransport(handler)) as http_client:
            namespace = AsyncNamespace(http_client, "a/b", "general")
            await namespace.acquisition_execution([], review_before_admission=True)
            assert await namespace.acquisition_review_candidates() == []
            assert (await namespace.run_review_worker())["dry_run"] is True
            assert await namespace.review_worker_runs() == []
            with pytest.raises(httpx.HTTPStatusError):
                await namespace.decide_acquisition_candidate("candidate/1", "reject")
            with pytest.raises(ValueError, match="approve or reject"):
                await namespace.decide_acquisition_candidate("candidate", "invalid")
        assert requests[0].url.raw_path.decode().startswith("/v1/namespaces/a%2Fb/acquisition/execute")
        assert json.loads(requests[0].content)["review_before_admission"] is True
        assert requests[-1].url.raw_path.decode().endswith("/candidates/candidate%2F1/reject")

    asyncio.run(scenario())
