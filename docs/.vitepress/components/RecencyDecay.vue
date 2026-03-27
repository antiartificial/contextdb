<template>
  <div class="rd-root">
    <div class="rd-header">
      <h3 class="rd-title">Recency Decay Visualizer</h3>
      <p class="rd-subtitle">How exponential decay affects facts of different ages. Formula: <code>exp(-alpha * age_hours)</code></p>
    </div>

    <div class="rd-types">
      <button
        v-for="(alpha, type) in alphas"
        :key="type"
        class="rd-type-btn"
        :class="{ 'rd-type-btn--active': selected === type }"
        @click="selected = type"
      >
        {{ type }}
      </button>
    </div>

    <div class="rd-alpha-info">
      alpha = <strong>{{ alphas[selected] }}</strong> &nbsp;|&nbsp; half-life: <strong>{{ halfLife }}</strong>
    </div>

    <div class="rd-facts">
      <div v-for="fact in facts" :key="fact.text" class="rd-fact">
        <div class="rd-fact-text">{{ fact.text }}</div>
        <div class="rd-fact-age">{{ ageLabel(fact.age) }}</div>
        <div class="rd-bar-wrap">
          <div
            class="rd-bar"
            :style="{ width: Math.max(0.5, score(fact.age) * 100) + '%', background: barColor(score(fact.age)) }"
          ></div>
          <span class="rd-score-label">{{ score(fact.age).toFixed(3) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const facts = [
  { text: 'User asked about auth module',   age: 1   },
  { text: 'Sprint planning notes',          age: 24  },
  { text: 'API redesign discussion',        age: 72  },
  { text: 'Q1 roadmap priorities',          age: 168 },
  { text: 'Architecture decision record',   age: 720 },
]

const alphas = {
  Working:    999,
  Episodic:   0.08,
  Semantic:   0.02,
  Procedural: 0.001,
  General:    0.05,
}

const selected = ref('Episodic')

function score(age) {
  return Math.exp(-alphas[selected.value] * age)
}

const halfLife = computed(() => {
  const a = alphas[selected.value]
  const h = Math.LN2 / a
  if (h < 1)        return (h * 60).toFixed(0) + ' min'
  if (h < 48)       return h.toFixed(1) + ' hours'
  if (h < 720)      return (h / 24).toFixed(1) + ' days'
  return (h / 720).toFixed(1) + ' months'
})

function ageLabel(hours) {
  if (hours < 24)   return hours + 'h ago'
  if (hours < 168)  return (hours / 24).toFixed(0) + 'd ago'
  if (hours < 720)  return (hours / 168).toFixed(0) + 'w ago'
  return (hours / 720).toFixed(0) + 'mo ago'
}

function barColor(s) {
  if (s > 0.7) return '#22c55e'
  if (s > 0.3) return '#f59e0b'
  return '#ef4444'
}
</script>

<style scoped>
.rd-root {
  background: var(--vp-c-bg-soft);
  border: 1px solid var(--vp-c-divider);
  border-radius: 12px;
  padding: 1.25rem 1.5rem;
  margin: 1.5rem 0;
  font-size: 0.875rem;
  color: var(--vp-c-text-1);
}
.rd-title    { margin: 0 0 0.25rem; font-size: 1rem; font-weight: 600; }
.rd-subtitle { margin: 0 0 1rem; color: var(--vp-c-text-2, #888); font-size: 0.8rem; }

.rd-types { display: flex; flex-wrap: wrap; gap: 0.4rem; margin-bottom: 0.75rem; }
.rd-type-btn {
  padding: 0.25rem 0.75rem; border-radius: 20px; border: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg); color: var(--vp-c-text-1); cursor: pointer; font-size: 0.8rem;
  transition: all 0.2s;
}
.rd-type-btn:hover { border-color: var(--vp-c-brand-1, #646cff); }
.rd-type-btn--active {
  background: var(--vp-c-brand-1, #646cff); color: #fff;
  border-color: var(--vp-c-brand-1, #646cff);
}

.rd-alpha-info {
  font-size: 0.78rem; color: var(--vp-c-text-2, #888); margin-bottom: 1rem;
}
.rd-alpha-info strong { color: var(--vp-c-text-1); }

.rd-facts { display: flex; flex-direction: column; gap: 0.6rem; }
.rd-fact { display: grid; grid-template-columns: 13rem 4rem 1fr; align-items: center; gap: 0.5rem; }
@media (max-width: 600px) { .rd-fact { grid-template-columns: 1fr; } }

.rd-fact-text { font-size: 0.82rem; font-weight: 500; }
.rd-fact-age  { font-size: 0.75rem; color: var(--vp-c-text-2, #888); text-align: right; }

.rd-bar-wrap {
  display: flex; align-items: center; gap: 0.5rem;
  background: var(--vp-c-divider); border-radius: 4px; overflow: visible; height: 20px; position: relative;
}
.rd-bar {
  height: 100%; border-radius: 4px;
  transition: width 0.45s ease, background 0.45s ease;
  min-width: 2px;
}
.rd-score-label {
  font-size: 0.72rem; color: var(--vp-c-text-2, #888);
  white-space: nowrap; margin-left: 0.25rem;
}
</style>
