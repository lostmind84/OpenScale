<script lang="ts">
  import type { Chip } from '../lib/catalog'

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
    /** Three seconds in the neutral bottom-right corner open the administration. */
    onadmin: () => void
  }

  const { chips, active, searchOpen, healthy, onselect, ontogglesearch, onadmin }: Props = $props()

  /** Milliseconds of press that open the administration screen (§14.3). */
  const ADMIN_PRESS_MS = 3_000

  let pressTimer: ReturnType<typeof setTimeout> | null = null

  /** Starts counting the long press on the neutral corner. */
  function startAdminPress(): void {
    cancelAdminPress()
    pressTimer = setTimeout(onadmin, ADMIN_PRESS_MS)
  }

  /** Abandons the count: a press that ends early is not an intent. */
  function cancelAdminPress(): void {
    if (pressTimer !== null) clearTimeout(pressTimer)
    pressTimer = null
  }
</script>

<nav class="filters" aria-label="Catégories">
  <div class="chips">
    {#each chips as chip (chip.code)}
      <button
        type="button"
        class="chip touch-target"
        class:active={chip.code === active}
        style:--chip-color={chip.color}
        aria-pressed={chip.code === active}
        onclick={() => onselect(chip.code)}
      >
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
    <span class="chip-label" aria-hidden="true">🔍</span>
    <span class="visually-hidden">Chercher un produit</span>
  </button>

  <span
    class="health"
    class:fault={!healthy}
    role="status"
    aria-label={healthy ? 'Matériel disponible' : 'Matériel indisponible'}
  ></span>

  <!--
    The administration is opened by three seconds on THIS neutral zone, and not on
    the health dot: making the way in depend on holding a display element condemns
    us to keeping that element for a reason that has nothing to do with it (§14.3).
  -->
  <div
    class="admin-corner"
    aria-hidden="true"
    onpointerdown={startAdminPress}
    onpointerup={cancelAdminPress}
    onpointerleave={cancelAdminPress}
    onpointercancel={cancelAdminPress}
  ></div>
</nav>

<style>
  .filters {
    display: flex;
    align-items: center;
    gap: var(--touch-gap);
    height: var(--filter-height);
    padding: 0 var(--touch-gap);
    background: var(--surface);
    border-top: 1px solid var(--border);
  }

  .chips {
    display: flex;
    align-items: center;
    gap: var(--touch-gap);
    flex: 1 1 auto;
    overflow-x: auto;
    overscroll-behavior: contain;
  }

  .chip {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    padding: 0 1rem;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    font-size: 1.75rem;
    white-space: nowrap;
  }

  .chip.active {
    border-color: var(--chip-color, var(--ink));
    background: var(--bg);
    font-weight: 700;
  }

  .chip-count {
    color: var(--ink-muted);
    font-size: 1.25rem;
  }

  .search-key {
    flex: 0 0 auto;
  }

  .health {
    flex: 0 0 auto;
    width: 1.25rem;
    height: 1.25rem;
    border-radius: 50%;
    background: var(--ready);
  }

  .health.fault {
    background: var(--fault);
  }

  .admin-corner {
    flex: 0 0 auto;
    width: var(--touch-min);
    height: var(--touch-min);
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
