<script setup>
import { ref, computed, onMounted } from 'vue'
import { animate } from 'motion'

const alpha = ref(1)
const beta = ref(1)
const log = ref([])
const barRefs = ref([])
const credBarRef = ref(null)
const admitRef = ref(null)
const NUM_BARS = 20
const THRESHOLD_IDX = Math.floor(0.05 * NUM_BARS)

const credibility = computed(() => alpha.value / (alpha.value + beta.value))
const credPct = computed(() => (credibility.value * 100).toFixed(1))
const admitted = computed(() => credibility.value > 0.05)

function logGamma(z) {
  const c = [0.99999999999980993,676.5203681218851,-1259.1392167224028,771.32342877765313,-176.61502916214059,12.507343278686905,-0.13857109526572012,9.9843695780195716e-6,1.5056327351493116e-7]
  if (z < 0.5) return Math.log(Math.PI) - Math.log(Math.sin(Math.PI * z)) - logGamma(1 - z)
  z -= 1
  let x = c[0]
  for (let i = 1; i < 9; i++) x += c[i] / (z + i)
  const t = z + 7.5
  return 0.5 * Math.log(2 * Math.PI) + (z + 0.5) * Math.log(t) - t + Math.log(x)
}

const barHeights = computed(() => {
  const vals = Array.from({ length: NUM_BARS }, (_, i) => {
    const x = (i + 0.5) / NUM_BARS
    const lv = (alpha.value - 1) * Math.log(x) + (beta.value - 1) * Math.log(1 - x) - (logGamma(alpha.value) + logGamma(beta.value) - logGamma(alpha.value + beta.value))
    return isFinite(lv) ? Math.exp(lv) : 0
  })
  const mx = Math.max(...vals, 0.001)
  return vals.map(v => v / mx)
})

function animateBars() {
  barHeights.value.forEach((h, i) => {
    const el = barRefs.value[i]
    if (el) animate(el, { scaleY: h, opacity: 0.4 + h * 0.6 }, { duration: 0.4, easing: [0.4, 0, 0.2, 1] })
  })
}
function animateCredBar() {
  if (credBarRef.value) animate(credBarRef.value, { width: `${credPct.value}%` }, { duration: 0.5, easing: [0.4, 0, 0.2, 1] })
}
function addLog(msg) {
  log.value.unshift({ id: Date.now(), msg, time: new Date().toLocaleTimeString() })
  if (log.value.length > 8) log.value.pop()
}
function doValidate() {
  alpha.value++
  addLog(`+1 validated → Beta(${alpha.value}, ${beta.value})`)
  animateBars(); animateCredBar()
}
function doRefute() {
  beta.value++
  addLog(`+1 refuted → Beta(${alpha.value}, ${beta.value})`)
  animateBars(); animateCredBar()
}
function setPreset(a, b, label) {
  alpha.value = a; beta.value = b
  addLog(`Preset: ${label} → Beta(${a}, ${b})`)
  animateBars(); animateCredBar()
}
onMounted(() => animateBars())
</script>

<template>
  <div class="ce-wrap">
    <div class="ce-source">
      <div class="ce-source-header">
        <span class="ce-name">docs-crawler</span>
        <span class="ce-params">Beta({{ alpha }}, {{ beta }})</span>
      </div>
      <div class="ce-cred-row">
        <span class="ce-cred-label">Credibility</span>
        <div class="ce-bar-track">
          <div ref="credBarRef" class="ce-bar-fill" :style="{ width: credPct + '%' }"></div>
          <div class="ce-threshold-marker"></div>
        </div>
        <span class="ce-cred-val">{{ credPct }}%</span>
      </div>
    </div>

    <div class="ce-actions">
      <button class="ce-btn ce-validate" @click="doValidate">Validate</button>
      <button class="ce-btn ce-refute" @click="doRefute">Refute</button>
      <span class="ce-divider"></span>
      <button class="ce-btn ce-preset" @click="setPreset(1,1,'Reset')">Reset</button>
      <button class="ce-btn ce-preset" @click="setPreset(20,3,'Trusted Source')">Trusted</button>
      <button class="ce-btn ce-preset" @click="setPreset(1,1,'New Source')">New Source</button>
      <button class="ce-btn ce-preset" @click="setPreset(3,15,'Unreliable')">Unreliable</button>
    </div>

    <div class="ce-chart-wrap">
      <div class="ce-chart-title">Beta({{ alpha }}, {{ beta }}) distribution — P(credibility = x)</div>
      <div class="ce-chart">
        <div v-for="(h, i) in barHeights" :key="i" class="ce-bar-col">
          <div
            :ref="el => barRefs[i] = el"
            class="ce-dist-bar"
            :class="{ 'ce-bar-below': i < THRESHOLD_IDX }"
            :style="{ transform: `scaleY(${h})`, opacity: 0.4 + h * 0.6 }"
          ></div>
        </div>
        <div class="ce-threshold-line"></div>
      </div>
      <div class="ce-chart-labels"><span>0</span><span>↑ threshold</span><span>0.5</span><span>1</span></div>
    </div>

    <div class="ce-admit" :class="admitted ? 'ce-admit-yes' : 'ce-admit-no'">
      <span class="ce-admit-badge">{{ admitted ? 'ADMITTED' : 'REJECTED' }}</span>
      <span class="ce-admit-reason">{{ admitted ? 'credibility > 0.05' : 'credibility ≤ 0.05 — troll floor' }}</span>
    </div>

    <div class="ce-log">
      <div class="ce-log-title">Event log</div>
      <TransitionGroup name="slide">
        <div v-for="entry in log" :key="entry.id" class="ce-log-entry">
          <span class="ce-log-time">{{ entry.time }}</span>
          <span class="ce-log-msg">{{ entry.msg }}</span>
        </div>
      </TransitionGroup>
      <div v-if="log.length === 0" class="ce-log-empty">Click Validate or Refute to start</div>
    </div>
  </div>
</template>

<style scoped>
.ce-wrap { max-width:600px; margin:1.5rem 0; border:1px solid var(--vp-c-divider); border-radius:8px; padding:1rem; background:var(--vp-c-bg-soft); font-size:.875rem; }
.ce-source { background:var(--vp-c-bg); border:1px solid var(--vp-c-divider); border-radius:6px; padding:.75rem; margin-bottom:.75rem; }
.ce-source-header { display:flex; align-items:center; gap:.5rem; margin-bottom:.5rem; }
.ce-name { font-weight:600; font-family:var(--vp-font-family-mono); color:var(--vp-c-brand-1); }
.ce-params { font-family:var(--vp-font-family-mono); font-size:.8rem; color:var(--vp-c-text-2); }
.ce-cred-row { display:flex; align-items:center; gap:.5rem; }
.ce-cred-label { color:var(--vp-c-text-2); white-space:nowrap; min-width:4rem; }
.ce-bar-track { flex:1; height:10px; background:var(--vp-c-bg-soft); border-radius:5px; position:relative; overflow:visible; }
.ce-bar-fill { height:100%; border-radius:5px; background:linear-gradient(90deg,var(--vp-c-brand-2),var(--vp-c-brand-1)); }
.ce-threshold-marker { position:absolute; top:-3px; left:5%; width:2px; height:16px; background:#e67e22; border-radius:1px; }
.ce-cred-val { font-family:var(--vp-font-family-mono); min-width:3rem; text-align:right; }
.ce-actions { display:flex; flex-wrap:wrap; gap:.5rem; margin-bottom:.75rem; align-items:center; }
.ce-btn { padding:.35rem .8rem; border-radius:5px; border:1px solid transparent; cursor:pointer; font-size:.82rem; font-weight:500; transition:opacity .15s; }
.ce-btn:hover { opacity:.85; }
.ce-validate { background:#22c55e; color:#fff; border-color:#16a34a; }
.ce-refute { background:#ef4444; color:#fff; border-color:#dc2626; }
.ce-divider { width:1px; height:1.4em; background:var(--vp-c-divider); align-self:center; }
.ce-preset { background:var(--vp-c-bg); color:var(--vp-c-text-1); border-color:var(--vp-c-divider); }
.ce-chart-wrap { margin-bottom:.75rem; }
.ce-chart-title { font-size:.78rem; color:var(--vp-c-text-2); margin-bottom:.4rem; font-family:var(--vp-font-family-mono); }
.ce-chart { height:80px; display:flex; align-items:flex-end; gap:2px; position:relative; background:var(--vp-c-bg); border:1px solid var(--vp-c-divider); border-radius:5px; padding:6px 4px 4px; }
.ce-bar-col { flex:1; display:flex; align-items:flex-end; height:100%; }
.ce-dist-bar { width:100%; height:60px; border-radius:3px 3px 0 0; transform-origin:bottom; background:linear-gradient(180deg,var(--vp-c-brand-1),var(--vp-c-brand-2)); }
.ce-dist-bar.ce-bar-below { background:linear-gradient(180deg,#ef4444,#dc2626); }
.ce-threshold-line { position:absolute; top:0; bottom:0; left:5%; width:2px; background:#e67e22; opacity:.7; pointer-events:none; }
.ce-chart-labels { display:flex; justify-content:space-between; font-size:.72rem; color:var(--vp-c-text-2); margin-top:2px; padding:0 4px; }
.ce-admit { display:flex; align-items:center; gap:.6rem; padding:.5rem .75rem; border-radius:5px; margin-bottom:.75rem; border:1px solid transparent; transition:all .4s; }
.ce-admit-yes { background:color-mix(in srgb,#22c55e 12%,transparent); border-color:#22c55e; }
.ce-admit-no { background:color-mix(in srgb,#ef4444 12%,transparent); border-color:#ef4444; }
.ce-admit-badge { font-weight:700; font-family:var(--vp-font-family-mono); font-size:.8rem; letter-spacing:.05em; }
.ce-admit-yes .ce-admit-badge { color:#16a34a; }
.ce-admit-no .ce-admit-badge { color:#dc2626; }
.ce-admit-reason { font-size:.8rem; color:var(--vp-c-text-2); }
.ce-log { background:var(--vp-c-bg); border:1px solid var(--vp-c-divider); border-radius:5px; padding:.5rem .75rem; max-height:150px; overflow-y:auto; }
.ce-log-title { font-size:.75rem; font-weight:600; color:var(--vp-c-text-2); margin-bottom:.4rem; text-transform:uppercase; letter-spacing:.05em; }
.ce-log-entry { display:flex; gap:.5rem; padding:.2rem 0; border-bottom:1px solid var(--vp-c-divider); font-family:var(--vp-font-family-mono); font-size:.8rem; }
.ce-log-entry:last-child { border-bottom:none; }
.ce-log-time { color:var(--vp-c-text-2); white-space:nowrap; }
.ce-log-empty { color:var(--vp-c-text-2); font-style:italic; font-size:.8rem; }
.slide-enter-active { transition:all .25s cubic-bezier(.4,0,.2,1); }
.slide-enter-from { transform:translateX(20px); opacity:0; }
.slide-enter-to { transform:translateX(0); opacity:1; }
.slide-leave-active { transition:all .15s; }
.slide-leave-to { opacity:0; }
</style>
