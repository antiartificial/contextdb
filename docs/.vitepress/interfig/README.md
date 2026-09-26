# interfig source used by ContextDB docs

`vendor/index.tsx`, `vendor/model.ts`, and `vendor/geometry.ts` are a pinned copy of the React interfig renderer from [vectorize-io/hindsight](https://github.com/vectorize-io/hindsight/tree/ccfe85b4851957ac2adf88b4a9ddf9668b2882f1/hindsight-interfig/src), commit `ccfe85b4851957ac2adf88b4a9ddf9668b2882f1`. Hindsight distributes this package from source rather than npm. The copied files are MIT licensed; see [LICENSE](./LICENSE).

`figures.ts` contains ContextDB's original figure specifications. `ContextDBFlow.vue` mounts the React renderer client-side in VitePress.

The vendored `index.tsx` adds a default React import for the JSX transform used by VitePress and removes the Next.js-only `use client` directive. The Vue wrapper mounts the renderer only in the browser.
