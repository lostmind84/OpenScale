import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

/**
 * Vite build of the two screens of a weighing station.
 *
 * The output lands in `internal/web/dist`, which is COMMITTED: `go build` has to
 * work on a machine with no Node at all, so that in three years someone fixing a
 * business rule never installs a JS toolchain (§14.1).
 *
 * Two entries, `index.html` and `admin.html`: the separation is about WEIGHT and
 * LOADING, not about the execution context — a station that never opens the
 * administration screen, which is the nominal case all day long, downloads,
 * parses and runs none of its bytes (§14.1).
 */
export default defineConfig({
  plugins: [svelte()],
  base: '/',
  build: {
    outDir: fileURLToPath(new URL('../internal/web/dist', import.meta.url)),
    emptyOutDir: true,
    target: 'es2022',
    assetsInlineLimit: 0,
    rollupOptions: {
      input: {
        index: fileURLToPath(new URL('./index.html', import.meta.url)),
        admin: fileURLToPath(new URL('./admin.html', import.meta.url)),
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['test/**/*.test.ts'],
    setupFiles: ['./test/setup.ts'],
    // The browser condition is what makes `mount()` from Svelte 5 resolve to the
    // client runtime under Vitest: without it a component test would silently
    // exercise the server renderer and assert nothing about the DOM.
    server: { deps: { inline: ['svelte'] } },
  },
  // `browser` is what makes `mount()` resolve to the CLIENT runtime of Svelte 5, and it
  // is set for Vitest ONLY.
  //
  // `resolve.conditions` REPLACES Vite's defaults — it does not add to them — and
  // `['browser']` was previously applied to the production build as well. Vite's own
  // defaults (`module`, `browser`, `production`) were therefore lost there, `svelte`
  // resolved to `src/index-server.js`, and both entries were built against a runtime whose
  // `mount()` is `function(){ throw lifecycle_function_unavailable }`. The bundle could not
  // start a single screen, and nothing failed loudly: esbuild even dropped the arguments of
  // the parameterless stub, which is why the administration chunk came out 288 bytes long
  // with none of its own code in it. `npm run budget` now refuses a bundle carrying that
  // runtime, so the mistake cannot come back in silence.
  ...(process.env.VITEST === undefined ? {} : { resolve: { conditions: ['browser'] } }),
})
