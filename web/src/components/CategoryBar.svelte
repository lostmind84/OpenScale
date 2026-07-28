<script lang="ts">
  import type { Chip } from '../lib/catalog'
  import { readable, wash } from '../lib/color'
  import Icon from './Icon.svelte'

  /**
   * The bottom bar: the category filters and the search key.
   *
   * The health dot and the settings entry moved to StatusBar (ADR-036 addendum,
   * Task 10) — this bar is now purely about narrowing what the grid shows.
   *
   * Categories are FILTERS, not four screens. The legacy application had four
   * hard-coded buttons because four forms were prebuilt at start-up; the real
   * distribution forbids that parity and inverts itself between exports — `A` held
   * ONE product in 2022 and is the most populated shelf in 2026. So: the resting
   * view is « Tout », a chip appears only for a POPULATED category, and the order
   * and the colour come from the configuration (§14.3-2, ADR-024).
   */
  interface Props {
    chips: Chip[]
    active: string
    /** Whether the physical search field is currently revealed (Task 6). */
    searchFieldOpen: boolean
    onselect: (code: string) => void
    /** Reveals the physical search field — no touch keyboard to toggle anymore. */
    onopensearch: () => void
  }

  const { chips, active, searchFieldOpen, onselect, onopensearch }: Props = $props()
</script>

<nav class="filters" aria-label="Catégories">
  <div class="chips">
    {#each chips as chip (chip.code)}
      <button
        type="button"
        class="chip touch-target"
        class:active={chip.code === active}
        style:--chip-ink={readable(chip.color)}
        style:--chip-wash={wash(chip.color)}
        aria-pressed={chip.code === active}
        onclick={() => onselect(chip.code)}
      >
        <span class="dot" aria-hidden="true"></span>
        <span class="chip-label">{chip.label}</span>
        <span class="chip-count">{chip.count}</span>
      </button>
    {/each}
  </div>

  <button
    type="button"
    class="chip touch-target search-key"
    class:active={searchFieldOpen}
    aria-pressed={searchFieldOpen}
    onclick={onopensearch}
  >
    <Icon name="search" size="1.75rem" />
    <span class="visually-hidden">Chercher un produit</span>
  </button>
</nav>

<style>
  .filters {
    display: flex;
    /* PERMANENT means it keeps its height whatever the grid above weighs. */
    flex: 0 0 auto;
    align-items: center;
    gap: 0.75rem;
    height: var(--category-height);
    padding: 0 var(--touch-gap);
    background: var(--surface);
    border-top: 1px solid var(--border);
    z-index: 2;
  }

  .chips {
    display: flex;
    align-items: center;
    gap: var(--touch-gap);
    flex: 1 1 auto;
    overflow-x: auto;
    overscroll-behavior: contain;
    /* The scrollbar of an overflowing row of chips has no business on a screen
       that is touched: the row scrolls under the finger. */
    scrollbar-width: none;
  }

  .chip {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0 1.375rem;
    border: 2px solid var(--border);
    /* Le rayon des touches de la maquette, et non une gélule : à cette taille,
       un arrondi complet fait lire une pastille décorative là où il y a une
       commande. */
    border-radius: var(--radius-lg);
    background: var(--surface);
    font-size: 1.75rem;
    white-space: nowrap;
    transition:
      border-color var(--slide) var(--ease),
      background-color var(--slide) var(--ease),
      transform var(--tap) var(--ease);
  }

  /* The shelf colour identifies, it never carries letters: a configured hex can
     be anything, and `readable()` is what keeps this dot visible whatever it is. */
  .dot {
    width: 0.75rem;
    height: 0.75rem;
    border-radius: 50%;
    background: var(--chip-ink, var(--ink-muted));
  }

  .chip.active {
    border-color: var(--chip-ink, var(--ink));
    background: var(--chip-wash, var(--bg));
    font-weight: 700;
  }

  .chip-count {
    color: var(--ink-muted);
    font-size: 1.25rem;
    font-weight: 400;
  }

  .search-key {
    flex: 0 0 auto;
    justify-content: center;
    padding: 0;
    width: var(--touch-min);
    border-radius: var(--radius);
  }

  .search-key.active {
    border-color: var(--ink);
    background: var(--bg);
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
