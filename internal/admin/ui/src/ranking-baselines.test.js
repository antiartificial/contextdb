import assert from 'node:assert/strict'
import test from 'node:test'
import {
  MAX_RANKING_BASELINES,
  RANKING_BASELINE_STORAGE_KEY,
  deleteRankingBaseline,
  readRankingBaselines,
  saveRankingBaseline,
} from './ranking-baselines.js'

const report = () => ({
  generated_at: '2026-09-10T12:00:00Z', corpus: 'representative', queries: [],
  total_queries: 1, passed_queries: 1, failed_queries: 0, mean_reciprocal_rank: 1,
})

const storage = () => {
  const values = new Map()
  return { getItem: (key) => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) }
}

test('ranking baselines retain a bounded, newest-first valid history', () => {
  const local = storage()
  for (let index = 0; index < MAX_RANKING_BASELINES + 2; index += 1) {
    saveRankingBaseline(local, report(), new Date(1000 + index))
  }
  const entries = readRankingBaselines(local)
  assert.equal(entries.length, MAX_RANKING_BASELINES)
  assert.equal(entries[0].saved_at, new Date(1000 + MAX_RANKING_BASELINES + 1).toISOString())
})

test('ranking baselines ignore malformed storage and delete the selected entry', () => {
  const local = storage()
  local.setItem(RANKING_BASELINE_STORAGE_KEY, '{not json')
  assert.deepEqual(readRankingBaselines(local), [])
  const first = saveRankingBaseline(local, report()).entry
  const second = saveRankingBaseline(local, report()).entry
  const result = deleteRankingBaseline(local, first.id)
  assert.equal(result.entries.length, 1)
  assert.equal(result.entries[0].id, second.id)
})

test('ranking baselines reject incompatible reports and survive unavailable storage', () => {
	assert.equal(saveRankingBaseline(undefined, report()).saved, false)
  const unavailable = { getItem: () => { throw new Error('disabled') }, setItem: () => { throw new Error('disabled') } }
  assert.deepEqual(readRankingBaselines(unavailable), [])
  const invalid = saveRankingBaseline(unavailable, { corpus: 'not-a-report' })
  assert.equal(invalid.saved, false)
  const valid = saveRankingBaseline(unavailable, report())
  assert.equal(valid.saved, false)
  assert.equal(valid.entries.length, 1)
})
