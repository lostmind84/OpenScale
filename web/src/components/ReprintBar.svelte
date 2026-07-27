<script lang="ts">
  import type { LabelDTO } from '../lib/dto'
  import { lastLabelSummary } from '../lib/format'
  import Icon from './Icon.svelte'

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
    /** When the catalog entered service, already formatted, or empty (§14.3). */
    catalogAt?: string
    onreprint: () => void
  }

  const { label, available, catalogAt = '', onreprint }: Props = $props()
</script>

<div class="bar" class:live={label !== null}>
  <span class="glyph" class:quiet={label === null} aria-hidden="true">
    <Icon name="printer" size="1.5rem" />
  </span>

  {#if label === null}
    <p class="idle">Dernière étiquette : aucune pour le moment</p>
  {:else}
    <p class="summary">
      <span class="lead">Dernière étiquette :</span>
      <strong>{lastLabelSummary(label)}</strong>
    </p>
    <button type="button" class="reprint touch-target" disabled={!available} onclick={onreprint}>
      Réimprimer
    </button>
  {/if}

  <!--
    Quand le catalogue est entré en service, en permanence.
    « Ces prix datent de quand ? » est la question qu'un bénévole pose devant une
    grille ; une date qui cesse d'avancer est aussi la façon dont un poste dit
    qu'il ne reçoit plus rien.
  -->
  {#if catalogAt !== ''}
    <p class="catalog">Catalogue du <strong>{catalogAt}</strong></p>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    /* PERMANENT means it keeps its height whatever the grid above weighs. */
    flex: 0 0 auto;
    align-items: center;
    gap: 1rem;
    height: var(--reprint-height);
    padding: 0 1rem 0 1.25rem;
    background: var(--bg);
    border-top: 1px solid var(--border);
    transition: background-color var(--slide) var(--ease);
  }

  /* A label has just come out: the strip lifts onto the surface colour, which is
     the whole acknowledgement §14.3 asks for — the real one is the paper. */
  .bar.live {
    background: var(--surface);
  }

  .glyph {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 2.5rem;
    height: 2.5rem;
    border-radius: var(--radius-sm);
    background: var(--ready-wash);
    color: var(--ink);
  }

  /* `quiet` and not `idle`: `.idle` is also the paragraph below, which declares
     `flex: 1 1 auto` — the glyph took the whole bar. */
  .glyph.quiet {
    background: var(--waiting-wash);
    color: var(--ink-muted);
  }

  .idle,
  .summary {
    flex: 1 1 auto;
    margin: 0;
    font-size: 1.375rem;
    color: var(--ink-muted);
  }

  .summary .lead {
    color: var(--ink-muted);
  }

  .summary strong {
    color: var(--ink);
    font-size: 1.5rem;
  }

  .reprint {
    padding: 0 2rem;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-1);
    font-size: 1.5rem;
    font-weight: 600;
    transition:
      border-color var(--slide) var(--ease),
      opacity var(--slide) var(--ease),
      transform var(--tap) var(--ease);
  }

  .reprint:disabled {
    opacity: 0.35;
    box-shadow: none;
    cursor: default;
  }

  /* La date passe APRÈS le bouton dans la barre, et donc à l'extrême droite : ce
     qu'un client touche reste plus près du centre que ce qu'un bénévole lit. */
  .catalog {
    flex: 0 0 auto;
    margin: 0;
    padding-left: 1.25rem;
    border-left: 1px solid var(--border);
    color: var(--ink-muted);
    font-size: 1.125rem;
    white-space: nowrap;
  }

  .catalog strong {
    color: var(--ink);
    font-weight: 600;
  }
</style>
