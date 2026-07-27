<script lang="ts">
  import type { Chip } from '../lib/catalog'
  import { readable, wash } from '../lib/color'
  import Icon from './Icon.svelte'

  /**
   * The bottom bar: the category filters, the search key and the health dot.
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
    searchOpen: boolean
    /** Both devices nominal: one dot a volunteer reads at a glance (§14.3). */
    healthy: boolean
    onselect: (code: string) => void
    ontogglesearch: () => void
    /** Opens the administration — one press on a named key (ADR-032). */
    onadmin: () => void
  }

  const { chips, active, searchOpen, healthy, onselect, ontogglesearch, onadmin }: Props = $props()
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
    class:active={searchOpen}
    aria-pressed={searchOpen}
    onclick={ontogglesearch}
  >
    <Icon name="search" size="1.75rem" />
    <span class="visually-hidden">Chercher un produit</span>
  </button>

  <span
    class="health"
    class:fault={!healthy}
    role="status"
    aria-label={healthy ? 'Matériel disponible' : 'Matériel indisponible'}
  ></span>

  <!--
    The way into the administration, and it is VISIBLE (ADR-032).
    A three second press on an unmarked corner is a way in nobody finds without
    being told; the long press stays, on this same button, for the day a station
    is put behind a counter where a customer could reach the screen.
  -->
  <button type="button" class="chip touch-target admin" onclick={onadmin}>
    <Icon name="settings" size="1.75rem" />
    <span class="admin-label">Réglages</span>
  </button>
</nav>

<style>
  .filters {
    display: flex;
    /* PERMANENT means it keeps its height whatever the grid above weighs. */
    flex: 0 0 auto;
    align-items: center;
    gap: 0.75rem;
    height: var(--filter-height);
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
    border-radius: var(--radius-pill);
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

  .health {
    flex: 0 0 auto;
    width: 0.875rem;
    height: 0.875rem;
    border-radius: 50%;
    background: var(--ready);
    box-shadow: 0 0 0 0.375rem var(--ready-wash);
    transition: background-color var(--slide) var(--ease);
  }

  .health.fault {
    background: var(--fault);
    box-shadow: 0 0 0 0.375rem var(--fault-wash);
  }

  /* Quiet, and named. It sits after the health dot, at the far edge, where a
     customer's hand does not go and a volunteer's eye does. */
  .admin {
    flex: 0 0 auto;
    gap: 0.5rem;
    padding: 0 1.25rem;
    border-radius: var(--radius);
    color: var(--ink-muted);
    font-size: 1.25rem;
  }

  .admin-label {
    letter-spacing: 0.02em;
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
