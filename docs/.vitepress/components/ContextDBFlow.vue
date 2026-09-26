<template>
  <div class="contextdb-flow" :aria-label="title">
    <div ref="mountPoint" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import type { Root } from 'react-dom/client'
import type { Figure } from '../interfig/vendor/model'

const props = defineProps<{ story: 'write' | 'retrieve' | 'review'; title: string }>()
const mountPoint = ref<HTMLElement | null>(null)
let root: Root | undefined

onMounted(async () => {
  if (!mountPoint.value) return
  const [{ createElement }, { createRoot }, { Flow }, { figures }] = await Promise.all([
    import('react'),
    import('react-dom/client'),
    import('../interfig/vendor/index'),
    import('../interfig/figures'),
  ])
  const figure: Figure = figures[props.story]
  if (!mountPoint.value) return
  root = createRoot(mountPoint.value)
  root.render(createElement(Flow, figure.props))
})

onBeforeUnmount(() => root?.unmount())
</script>
