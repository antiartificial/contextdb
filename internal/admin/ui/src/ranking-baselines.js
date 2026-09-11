export const RANKING_BASELINE_STORAGE_KEY = 'contextdb.admin.ranking-baselines.v1'
export const MAX_RANKING_BASELINES = 12
const MAX_BASELINE_BYTES = 400000

function isFiniteNumber(value) {
  return typeof value === 'number' && Number.isFinite(value)
}

// Keep storage corruption and incompatible uploaded artifacts out of the UI.
export function isRankingEval(value) {
  return Boolean(
    value && typeof value === 'object' &&
    typeof value.generated_at === 'string' &&
    typeof value.corpus === 'string' &&
    Array.isArray(value.queries) &&
    isFiniteNumber(value.total_queries) &&
    isFiniteNumber(value.passed_queries) &&
    isFiniteNumber(value.failed_queries) &&
    isFiniteNumber(value.mean_reciprocal_rank),
  )
}

export function readRankingBaselines(storage) {
  try {
    const raw = storage?.getItem(RANKING_BASELINE_STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((entry) => entry && typeof entry.id === 'string' && typeof entry.name === 'string' &&
        typeof entry.saved_at === 'string' && isRankingEval(entry.report))
      .slice(0, MAX_RANKING_BASELINES)
  } catch {
    return []
  }
}

export function writeRankingBaselines(storage, entries) {
  const valid = Array.isArray(entries) ? entries
    .filter((entry) => entry && typeof entry.id === 'string' && typeof entry.name === 'string' &&
      typeof entry.saved_at === 'string' && isRankingEval(entry.report))
    .slice(0, MAX_RANKING_BASELINES) : []
  if (!storage || typeof storage.setItem !== 'function') return false
  try {
    const serialized = JSON.stringify(valid)
    if (serialized.length > MAX_BASELINE_BYTES) return false
    storage?.setItem(RANKING_BASELINE_STORAGE_KEY, serialized)
    return true
  } catch {
    return false
  }
}

export function saveRankingBaseline(storage, report, now = new Date()) {
  if (!isRankingEval(report)) return { entries: readRankingBaselines(storage), saved: false }
  const id = `${now.getTime()}-${Math.random().toString(36).slice(2, 8)}`
  const name = `${report.corpus} · ${report.generated_at || now.toISOString()}`
  const entry = { id, name, saved_at: now.toISOString(), report }
  const entries = [entry, ...readRankingBaselines(storage)].slice(0, MAX_RANKING_BASELINES)
  return { entries, saved: writeRankingBaselines(storage, entries), entry }
}

export function deleteRankingBaseline(storage, id) {
  const entries = readRankingBaselines(storage).filter((entry) => entry.id !== id)
  return { entries, saved: writeRankingBaselines(storage, entries) }
}
