"""Tests for the contextdb Python SDK.

These tests require a running contextdb server at localhost:7701.
Start one with: make run
"""

import os

import pytest

from contextdb import ContextDB


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
