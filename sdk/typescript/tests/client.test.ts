/**
 * Integration tests for the contextdb TypeScript SDK.
 * Requires a running server at localhost:7701.
 * Start with: make run
 *
 * Run with: CONTEXTDB_INTEGRATION=1 npx tsx --test tests/client.test.ts
 */

import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

// Skip if not integration mode
const INTEGRATION = process.env.CONTEXTDB_INTEGRATION === '1';
const SERVER_URL = process.env.CONTEXTDB_URL ?? 'http://localhost:7701';

// Dynamic import to avoid TypeScript issues in test runner
const { ContextDB } = await import('../src/client');

describe('ContextDB TypeScript SDK', { skip: !INTEGRATION }, () => {
  const db = new ContextDB(SERVER_URL);

  it('should ping the server', async () => {
    const result = await db.ping();
    assert.equal(result.status, 'ok');
  });

  it('should get stats', async () => {
    const result = await db.stats();
    assert.ok(result);
  });

  it('should write and retrieve', async () => {
    const ns = db.namespace('ts-test', 'general');

    const writeResult = await ns.write({
      content: 'TypeScript SDK works',
      sourceId: 'ts-test',
      labels: ['Claim'],
      vector: [0.1, 0.2, 0.3, 0.4],
    });

    assert.ok(writeResult.admitted);
    assert.ok(writeResult.nodeId);

    const results = await ns.retrieve({
      vector: [0.1, 0.2, 0.3, 0.4],
      topK: 5,
    });

    assert.ok(results.length > 0);
    assert.ok(results[0].score > 0);
  });

  it('should retrieve with text query', async () => {
    const ns = db.namespace('ts-test-text', 'general');

    await ns.write({
      content: 'Go is a fast language',
      sourceId: 'test',
      labels: ['Claim'],
      vector: [0.5, 0.5, 0.5, 0.5],
    });

    // Text queries require server-side embedder
    const results = await ns.retrieve({ text: 'What is Go?', topK: 5 });
    assert.ok(Array.isArray(results));
  });

  it('should filter by labels', async () => {
    const ns = db.namespace('ts-test-filter', 'general');

    await ns.write({
      content: 'labeled node',
      sourceId: 'test',
      labels: ['Special'],
      vector: [0.3, 0.3, 0.3, 0.3],
    });

    const results = await ns.retrieve({
      vector: [0.3, 0.3, 0.3, 0.3],
      topK: 5,
      labels: ['Special'],
    });

    for (const r of results) {
      assert.ok(r.labels.includes('Special'));
    }
  });
});
