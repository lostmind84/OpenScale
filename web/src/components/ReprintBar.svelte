<script lang="ts">
  import type { LabelDTO } from '../lib/dto'
  import { lastLabelSummary } from '../lib/format'

  /**
   * The reprint bar, and it is PERMANENT.
   *
   * It is not inside a success overlay, and the reason is physical: the customer
   * steps away from the plate precisely to look for their label, so an overlay
   * that closes « when the bag leaves the scale » would close at the exact moment
   * it is needed. Such an overlay does not exist here (§14.3, ADR-023).
   *
   * One reprint only, and the label goes out carrying the word RÉIMPRESSION.
   */
  interface Props {
    /** What came out last, or null when nothing has been printed yet. */
    label: LabelDTO | null
    /** The window of `reprint_window_s` is still open, and nothing was reprinted. */
    available: boolean
    onreprint: () => void
  }

  const { label, available, onreprint }: Props = $props()
</script>

<div class="bar">
  {#if label === null}
    <p class="idle">Dernière étiquette : aucune pour le moment</p>
  {:else}
    <p class="summary">
      Dernière étiquette :
      <strong>{lastLabelSummary(label)}</strong>
    </p>
    <button type="button" class="reprint touch-target" disabled={!available} onclick={onreprint}>
      Réimprimer
    </button>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 1rem;
    height: var(--reprint-height);
    padding: 0 var(--touch-gap);
    background: var(--bg);
    border-top: 1px solid var(--border);
  }

  .idle,
  .summary {
    flex: 1 1 auto;
    margin: 0;
    font-size: 1.25rem;
    color: var(--ink-muted);
  }

  .summary strong {
    color: var(--ink);
  }

  .reprint {
    padding: 0 1.5rem;
    /* The bar is 56 px high and the button is a touch target: it therefore grows
       past the bar rather than shrinking below 72 px (§14.2). */
    border: 2px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    font-size: 1.5rem;
  }

  .reprint:disabled {
    opacity: 0.4;
  }
</style>
