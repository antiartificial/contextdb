<template>
  <div class="vh-root">
    <div class="vh-header">
      <h3 class="vh-title">Version History: API Rate Limit</h3>
      <p class="vh-subtitle">How a single fact evolves through transaction time.</p>
    </div>

    <div class="vh-controls">
      <button class="vh-btn" @click="togglePlay">{{ playing ? 'Pause' : 'Play' }}</button>
      <input
        type="range"
        class="vh-slider"
        :min="0"
        :max="versions.length - 1"
        v-model.number="current"
      />
      <span class="vh-slider-label">v{{ current + 1 }} / v{{ versions.length }}</span>
    </div>

    <div class="vh-timeline">
      <div class="vh-line"></div>
      <TransitionGroup name="vh-card" tag="div" class="vh-cards">
        <div
          v-for="(v, i) in visibleVersions"
          :key="v.id"
          class="vh-card"
          :class="{ 'vh-card--retracted': v.retracted, 'vh-card--active': i === visibleVersions.length - 1 && !v.retracted }"
        >
          <div class="vh-dot-wrap">
            <div class="vh-dot" :class="{ 'vh-dot--retracted': v.retracted }"></div>
          </div>
          <div class="vh-card-body">
            <div class="vh-card-top">
              <span class="vh-version">v{{ v.id }}</span>
              <span class="vh-tx">tx: {{ v.tx }}</span>
            </div>
            <div class="vh-value" :class="{ 'vh-value--strike': v.retracted }">{{ v.value }}</div>
            <div v-if="v.retracted" class="vh-retract-reason">{{ v.reason }}</div>
            <div v-else class="vh-conf-wrap">
              <span class="vh-conf-label">confidence</span>
              <div class="vh-conf-track">
                <div
                  class="vh-conf-bar"
                  :style="{ width: (v.confidence * 100) + '%' }"
                ></div>
              </div>
              <span class="vh-conf-val">{{ (v.confidence * 100).toFixed(0) }}%</span>
            </div>
          </div>
        </div>
      </TransitionGroup>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onUnmounted } from 'vue'

const versions = [
  { id: 1, value: '100 req/s',  tx: 'Jan 5',  confidence: 0.95, retracted: false },
  { id: 2, value: '200 req/s',  tx: 'Feb 10', confidence: 0.80, retracted: false },
  { id: 3, value: '500 req/s',  tx: 'Mar 20', confidence: 0.90, retracted: false },
  { id: 4, value: 'superseded', tx: 'Apr 5',  confidence: 0,    retracted: true,  reason: 'retracted — superseded by v5' },
  { id: 5, value: '1000 req/s', tx: 'Apr 5',  confidence: 0.99, retracted: false },
]

const current = ref(0)
const playing = ref(false)
let timer = null

const visibleVersions = computed(() => versions.slice(0, current.value + 1))

function togglePlay() {
  if (playing.value) {
    clearInterval(timer)
    playing.value = false
  } else {
    if (current.value >= versions.length - 1) current.value = 0
    playing.value = true
    timer = setInterval(() => {
      if (current.value < versions.length - 1) {
        current.value++
      } else {
        clearInterval(timer)
        playing.value = false
      }
    }, 900)
  }
}

onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.vh-root {
  background: var(--vp-c-bg-soft);
  border: 1px solid var(--vp-c-divider);
  border-radius: 12px;
  padding: 1.25rem 1.5rem;
  margin: 1.5rem 0;
  font-size: 0.875rem;
  color: var(--vp-c-text-1);
}
.vh-title    { margin: 0 0 0.25rem; font-size: 1rem; font-weight: 600; }
.vh-subtitle { margin: 0 0 1rem; color: var(--vp-c-text-2, #888); font-size: 0.8rem; }

.vh-controls { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1.25rem; }
.vh-btn {
  padding: 0.3rem 0.9rem; border-radius: 6px; border: 1px solid var(--vp-c-brand-1, #646cff);
  background: var(--vp-c-brand-1, #646cff); color: #fff; cursor: pointer; font-size: 0.8rem;
  transition: opacity 0.2s;
}
.vh-btn:hover { opacity: 0.85; }
.vh-slider { flex: 1; cursor: pointer; accent-color: var(--vp-c-brand-1, #646cff); }
.vh-slider-label { font-size: 0.75rem; color: var(--vp-c-text-2, #888); white-space: nowrap; }

.vh-timeline { position: relative; padding-left: 1.5rem; }
.vh-line {
  position: absolute; left: 0.9rem; top: 0; bottom: 0;
  width: 2px; background: var(--vp-c-divider);
}
.vh-cards { display: flex; flex-direction: column; gap: 0.75rem; }

.vh-card {
  display: flex; gap: 0.75rem; align-items: flex-start;
  background: var(--vp-c-bg); border: 1px solid var(--vp-c-divider);
  border-radius: 8px; padding: 0.65rem 0.85rem;
  transition: border-color 0.3s;
}
.vh-card--active   { border-color: #22c55e; }
.vh-card--retracted { border-color: #ef4444; }

.vh-dot-wrap { padding-top: 0.2rem; flex-shrink: 0; }
.vh-dot {
  width: 10px; height: 10px; border-radius: 50%;
  background: var(--vp-c-brand-1, #646cff); border: 2px solid var(--vp-c-bg);
  box-shadow: 0 0 0 2px var(--vp-c-brand-1, #646cff);
}
.vh-dot--retracted { background: #ef4444; box-shadow: 0 0 0 2px #ef4444; }

.vh-card-body { flex: 1; min-width: 0; }
.vh-card-top { display: flex; justify-content: space-between; margin-bottom: 0.3rem; }
.vh-version { font-weight: 600; font-size: 0.8rem; color: var(--vp-c-brand-1, #646cff); }
.vh-tx      { font-size: 0.75rem; color: var(--vp-c-text-2, #888); }
.vh-value   { font-weight: 500; font-size: 0.95rem; margin-bottom: 0.4rem; }
.vh-value--strike { text-decoration: line-through; color: var(--vp-c-text-2, #888); }
.vh-retract-reason { font-size: 0.75rem; color: #ef4444; }

.vh-conf-wrap { display: flex; align-items: center; gap: 0.4rem; }
.vh-conf-label { font-size: 0.7rem; color: var(--vp-c-text-2, #888); width: 4.5rem; flex-shrink: 0; }
.vh-conf-track { flex: 1; height: 6px; border-radius: 3px; background: var(--vp-c-divider); overflow: hidden; }
.vh-conf-bar   { height: 100%; border-radius: 3px; background: #22c55e; transition: width 0.4s ease; }
.vh-conf-val   { font-size: 0.7rem; color: var(--vp-c-text-2, #888); width: 2.5rem; text-align: right; }

/* TransitionGroup */
.vh-card-enter-active { transition: all 0.35s ease; }
.vh-card-leave-active { transition: all 0.25s ease; position: absolute; }
.vh-card-enter-from   { opacity: 0; transform: translateX(-12px); }
.vh-card-leave-to     { opacity: 0; transform: translateX(12px); }
</style>
