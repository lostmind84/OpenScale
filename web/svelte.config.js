import { vitePreprocess } from '@sveltejs/vite-plugin-svelte'

/**
 * Svelte configuration.
 *
 * `vitePreprocess` is what lets a `<script lang="ts">` block be TypeScript. There
 * is nothing else to configure: no adapter, no router, no store library — this is
 * one page served from an `embed.FS` by the station itself.
 *
 * @type {import('@sveltejs/vite-plugin-svelte').SvelteConfig}
 */
export default {
  preprocess: vitePreprocess(),
}
