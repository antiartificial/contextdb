import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'

const { ContextDB } = await import('../dist/client.js')
const requests = []
const originalFetch = globalThis.fetch

afterEach(() => {
  requests.length = 0
  globalThis.fetch = originalFetch
})

function mockFetch(handler) {
  globalThis.fetch = async (url, init = {}) => {
    const request = { url: String(url), method: init.method ?? 'GET', headers: new Headers(init.headers), body: init.body ? JSON.parse(init.body) : undefined }
    requests.push(request)
    return handler(request)
  }
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

test('acquisition execution sends review-before-admission and escapes its namespace', async () => {
  mockFetch(() => json({ namespace: 'team/a b', dry_run: false, executed: true, runs: [], connectors: [], plan: {}, summary: {} }))
  await new ContextDB('https://api.example/').namespace('team/a b', 'belief system').acquisitionExecution({
    connectors: [{ id: 'web', type: 'search' }], execute: true, reviewBeforeAdmission: true,
  })
  assert.equal(requests[0].url, 'https://api.example/v1/namespaces/team%2Fa%20b/acquisition/execute')
  assert.deepEqual(requests[0].body, {
    mode: 'belief system', connectors: [{ id: 'web', type: 'search' }],
    execute: true, review_before_admission: true,
  })
})

test('candidate decisions and review worker use the documented mocked transport contract', async () => {
  mockFetch((request) => {
    if (request.url.includes('/candidates?')) return json({ candidates: [{ candidate_id: 'candidate/1' }] })
    if (request.url.endsWith('/approve')) return json({ decision: { status: 'admitted' } })
    if (request.url.endsWith('/cycle')) return json({ dry_run: true, evaluated: 2 })
    return json({ runs: [{ run_id: 'run-1' }] })
  })
  const ns = new ContextDB('https://api.example').namespace('a/b', 'general mode')
  assert.deepEqual(await ns.acquisitionReviewCandidates(), [{ candidate_id: 'candidate/1' }])
  assert.deepEqual(await ns.decideAcquisitionCandidate('candidate/1', 'approve', 'ana', 'verified'), { decision: { status: 'admitted' } })
  assert.deepEqual(await ns.runReviewWorker(), { dry_run: true, evaluated: 2 })
  assert.deepEqual(await ns.reviewWorkerRuns(), [{ run_id: 'run-1' }])
  assert.match(requests[0].url, /namespaces\/a%2Fb\/acquisition\/review\/candidates\?mode=general%20mode$/)
  assert.match(requests[1].url, /candidates\/candidate%2F1\/approve$/)
  assert.deepEqual(requests[1].body, { mode: 'general mode', actor: 'ana', note: 'verified' })
  assert.deepEqual(requests[2].body, { execute: false })
})

test('review APIs surface HTTP errors and reject unsupported decisions before transport', async () => {
  mockFetch(() => json({ error: 'nope' }, 503))
  const ns = new ContextDB('https://api.example').namespace('n')
  await assert.rejects(ns.acquisitionReviewCandidates(), /acquisitionReviewCandidates failed: 503/)
  await assert.rejects(ns.runReviewWorker(), /runReviewWorker failed: 503/)
  await assert.rejects(ns.decideAcquisitionCandidate('id', 'delete'), /Invalid acquisition decision/)
  assert.equal(requests.length, 2)
})

test('optional bearer token reaches top-level and review requests without changing content type', async () => {
  mockFetch(() => json({ status: 'ok' }))
  const db = new ContextDB('https://api.example', { token: 'secret' })
  await db.ping()
  await db.namespace('ns').runReviewWorker()
  assert.equal(requests[0].headers.get('authorization'), 'Bearer secret')
  assert.equal(requests[1].headers.get('authorization'), 'Bearer secret')
  assert.equal(requests[1].headers.get('content-type'), 'application/json')
})
