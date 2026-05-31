<script lang="ts">
  type Metrics = {
    mode: string
    generated_at: string
    health: { status: string; signals: string[] }
    ingest: {
      total: number
      admitted: number
      rejected: number
      admission_rate: number
      rejection_rate: number
    }
    retrieval: { total: number; errors: number; error_rate: number }
    latency: { p95_ms: number; mean_ms: number }
  }

  type SearchResult = {
    id: string
    labels?: string[]
    text?: string
    source_id?: string
    confidence?: number
    match_reason: string
  }

  type RankExplanation = {
    winner_node_id?: string
    loser_node_id?: string
    margin: number
    summary: string
    factors: { factor: string; node_contribution: number; other_contribution: number; delta: number }[]
  }

  let metrics: Metrics | null = null
  let metricsError = ''
  let searchNamespace = 'default'
  let searchQuery = ''
  let searchLimit = '10'
  let searchResults: SearchResult[] = []
  let searchMessage = ''
  let debugNamespace = 'default'
  let debugID = ''
  let debugOutput =
    'Enter a namespace and node ID to inspect source, support, contradictions, provenance, and confidence history.'
  let compareLeft = ''
  let compareRight = ''
  let compareQuery = ''
  let compareOutput = 'Select two search results or paste node IDs to compare ranking factors.'
  let compareSummary = ''

  const percent = (value = 0) => `${Math.round(value * 1000) / 10}%`
  const ms = (value = 0) => `${value.toFixed(2)} ms`
  const width = (value = 0) => `${Math.max(0, Math.min(100, value * 100))}%`

  async function refreshMetrics() {
    try {
      const response = await fetch('/admin/api/metrics')
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      metrics = JSON.parse(text)
      metricsError = ''
    } catch (error) {
      metricsError = String(error instanceof Error ? error.message : error)
    }
  }

  async function runSearch() {
    searchMessage = 'Searching...'
    searchResults = []
    try {
      const params = new URLSearchParams({
        ns: searchNamespace.trim(),
        q: searchQuery.trim(),
        limit: searchLimit.trim(),
      })
      const response = await fetch(`/admin/api/search?${params}`)
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      const data = JSON.parse(text)
      searchResults = data.results || []
      searchMessage = searchResults.length === 0 ? 'No matching nodes.' : ''
    } catch (error) {
      searchMessage = String(error instanceof Error ? error.message : error)
    }
  }

  function inspectResult(result: SearchResult) {
    debugNamespace = searchNamespace
    debugID = result.id
  }

  function addCompareTarget(result: SearchResult) {
    if (!compareLeft || compareLeft === result.id) {
      compareLeft = result.id
      return
    }
    compareRight = result.id
  }

  async function inspectNode() {
    debugOutput = 'Loading...'
    try {
      const params = new URLSearchParams({
        ns: debugNamespace.trim(),
        id: debugID.trim(),
      })
      const response = await fetch(`/admin/api/belief?${params}`)
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      debugOutput = JSON.stringify(JSON.parse(text), null, 2)
    } catch (error) {
      debugOutput = String(error instanceof Error ? error.message : error)
    }
  }

  async function compareRank() {
    compareOutput = 'Comparing...'
    compareSummary = ''
    try {
      const response = await fetch('/admin/api/explain-rank', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          namespace: searchNamespace.trim(),
          node_id: compareLeft.trim(),
          other_node_id: compareRight.trim(),
          text: compareQuery.trim(),
          max_depth: 2,
        }),
      })
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      const data: RankExplanation = JSON.parse(text)
      compareSummary = data.summary
      compareOutput = JSON.stringify(data, null, 2)
    } catch (error) {
      compareOutput = String(error instanceof Error ? error.message : error)
    }
  }

  refreshMetrics()
  const interval = window.setInterval(refreshMetrics, 5000)
</script>

<svelte:window on:beforeunload={() => window.clearInterval(interval)} />

<main class="shell">
  <header class="topbar">
    <div>
      <h1>contextdb admin</h1>
      <p>Metrics, debugger search, and belief evidence for local contextdb observability.</p>
    </div>
    <div class="health">
      <span>Health</span>
      <strong>{metrics?.health.status || 'loading'}</strong>
      <small>{metrics?.generated_at || 'waiting for metrics'}</small>
    </div>
  </header>

  <section class="dashboard" aria-labelledby="metrics-heading">
    <div class="section-heading">
      <h2 id="metrics-heading">Metrics</h2>
      <button type="button" on:click={refreshMetrics}>Refresh</button>
    </div>

    <div class="metrics-grid">
      <article class="metric">
        <span>Mode</span>
        <strong>{metrics?.mode || 'embedded'}</strong>
        <small>Runtime store profile</small>
      </article>
      <article class="metric">
        <span>Ingest Total</span>
        <strong>{metrics?.ingest.total ?? 0}</strong>
        <small>{percent(metrics?.ingest.admission_rate)} admitted</small>
      </article>
      <article class="metric">
        <span>Retrieval Total</span>
        <strong>{metrics?.retrieval.total ?? 0}</strong>
        <small>{percent(metrics?.retrieval.error_rate)} error rate</small>
      </article>
      <article class="metric">
        <span>P95 Latency</span>
        <strong>{ms(metrics?.latency.p95_ms)}</strong>
        <small>Mean {ms(metrics?.latency.mean_ms)}</small>
      </article>
    </div>

    <div class="metrics-detail">
      <div class="panel bars">
        <div>
          <div class="bar-label"><span>Admitted</span><span>{metrics?.ingest.admitted ?? 0} / {metrics?.ingest.total ?? 0}</span></div>
          <div class="bar-track"><span style={`width:${width(metrics?.ingest.admission_rate)}`}></span></div>
        </div>
        <div>
          <div class="bar-label"><span>Rejected</span><span>{metrics?.ingest.rejected ?? 0} / {metrics?.ingest.total ?? 0}</span></div>
          <div class="bar-track warn"><span style={`width:${width(metrics?.ingest.rejection_rate)}`}></span></div>
        </div>
        <div>
          <div class="bar-label"><span>Retrieval errors</span><span>{metrics?.retrieval.errors ?? 0} / {metrics?.retrieval.total ?? 0}</span></div>
          <div class="bar-track warn"><span style={`width:${width(metrics?.retrieval.error_rate)}`}></span></div>
        </div>
      </div>

      <div class="panel">
        <h3>Signals</h3>
        {#if metricsError}
          <p class="error">{metricsError}</p>
        {:else}
          <ul class="signals">
            {#each metrics?.health.signals || ['Loading metrics...'] as signal}
              <li>{signal}</li>
            {/each}
          </ul>
        {/if}
        <pre>{metrics ? JSON.stringify(metrics, null, 2) : 'Loading /admin/api/metrics...'}</pre>
      </div>
    </div>
  </section>

  <section class="debugger" aria-labelledby="debugger-heading">
    <h2 id="debugger-heading">Belief Debugger</h2>
    <div class="tool">
      <div class="search-row">
        <label>
          Namespace
          <input bind:value={searchNamespace} autocomplete="off" />
        </label>
        <label>
          Search
          <input bind:value={searchQuery} placeholder="text, source, label, or UUID" autocomplete="off" />
        </label>
        <label>
          Limit
          <input bind:value={searchLimit} inputmode="numeric" autocomplete="off" />
        </label>
        <button type="button" on:click={runSearch}>Search</button>
      </div>

      {#if searchMessage || searchResults.length > 0}
        <div class="results">
          {#if searchMessage}
            <p>{searchMessage}</p>
          {/if}
          {#each searchResults as result}
            <article class="result">
              <div>
                <strong>{result.text || result.id}</strong>
                <span>{result.id} | {result.source_id || 'unknown source'} | confidence {(result.confidence || 0).toFixed(2)} | {result.match_reason}</span>
              </div>
              <div class="result-actions">
                <button type="button" class="ghost" on:click={() => inspectResult(result)}>Inspect</button>
                <button type="button" class="ghost" on:click={() => addCompareTarget(result)}>Compare</button>
              </div>
            </article>
          {/each}
        </div>
      {/if}

      <div class="inspect-row">
        <label>
          Namespace
          <input bind:value={debugNamespace} autocomplete="off" />
        </label>
        <label>
          Node ID
          <input bind:value={debugID} placeholder="00000000-0000-0000-0000-000000000000" autocomplete="off" />
        </label>
        <button type="button" on:click={inspectNode}>Inspect</button>
      </div>
      <pre>{debugOutput}</pre>
    </div>
  </section>

  <section class="compare" aria-labelledby="compare-heading">
    <h2 id="compare-heading">Explain Rank Compare</h2>
    <div class="tool">
      <div class="compare-row">
        <label>
          Left node
          <input bind:value={compareLeft} placeholder="Node UUID" autocomplete="off" />
        </label>
        <label>
          Right node
          <input bind:value={compareRight} placeholder="Node UUID" autocomplete="off" />
        </label>
        <label>
          Query text
          <input bind:value={compareQuery} placeholder="optional query context" autocomplete="off" />
        </label>
        <button type="button" on:click={compareRank}>Compare</button>
      </div>
      {#if compareSummary}
        <p class="compare-summary">{compareSummary}</p>
      {/if}
      <pre>{compareOutput}</pre>
    </div>
  </section>

  <section class="links" aria-labelledby="links-heading">
    <h2 id="links-heading">Quick Links</h2>
    <a href="/v1/ping">Health Check</a>
    <a href="/v1/stats">API Stats</a>
    <a href="/metrics">Prometheus Metrics</a>
    <a href="/debug/pprof/">Profiling</a>
    <a href="/debug/vars">expvar</a>
  </section>
</main>
