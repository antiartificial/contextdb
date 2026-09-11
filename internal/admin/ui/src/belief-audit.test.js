import assert from 'node:assert/strict'
import test from 'node:test'
import { normalizeBeliefAudit } from './belief-audit.js'

test('normalizes nullable optional epistemic collections returned by retained ranking evaluations', () => {
  const audit = normalizeBeliefAudit({
    epistemics: {
      source: null,
      confidence_timeline: null,
      source_trust_timeline: null,
      contradiction_paths: null,
      graph_context: null,
    },
  })

  assert.deepEqual(audit.epistemics.source, { labels: [] })
  assert.deepEqual(audit.epistemics.confidence_timeline, [])
  assert.deepEqual(audit.epistemics.source_trust_timeline, [])
  assert.deepEqual(audit.epistemics.contradiction_paths, [])
  assert.deepEqual(audit.epistemics.graph_context, [])
})
