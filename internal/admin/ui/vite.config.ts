import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'
import { fileURLToPath } from 'node:url'

const adminUiDir = fileURLToPath(new URL('.', import.meta.url))

export default defineConfig({
  root: adminUiDir,
  base: '/admin/',
  plugins: [svelte()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    rollupOptions: {
      input: fileURLToPath(new URL('index.html', import.meta.url)),
    },
  },
})
