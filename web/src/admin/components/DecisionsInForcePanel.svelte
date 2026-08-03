<script lang="ts">
  import Panel from './Panel.svelte'
  import { decisionSentence } from '../lib/decisions'
  import type { DecisionDTO } from '../lib/dto'
  import { frenchDate } from '../lib/format'
  import { tally } from '../lib/tally'

  /**
   * What a human decided, product by product, and the one way back.
   *
   * This panel is REACHABLE on purpose and it is the whole reason it exists: the client
   * catalog does not serve a product a human refused, so the search above can never find
   * it again — and the one button that undoes a withdrawal was unreachable from the
   * moment it mattered. Every row opens its own decision again.
   */
  interface Props {
    /** The decisions in force, most recent first: the sort belongs to the page. */
    decisions: DecisionDTO[]
    /**
     * The name of a product, or an honest sentence when the page cannot give one.
     *
     * A function and not a table: the wording of a failed read belongs to the page, and
     * the two screens that list decisions do not say it in the same words.
     */
    nameOf: (id: string) => string
    /** Opens one decision again in the form above. */
    onchoose: (id: string) => void
  }

  const { decisions, nameOf, onchoose }: Props = $props()

  /** How many human decisions in force are drawn at once. */
  const DECISIONS_SHOWN = 20

  const shown = $derived(decisions.slice(0, DECISIONS_SHOWN))
</script>

<Panel
  title="Décisions en vigueur"
  note="Ce qu’un humain a décidé, produit par produit. C’est ici que se reprend un produit retiré de la grille."
>
  {#if decisions.length === 0}
    <p class="fact">Aucune décision locale : la grille est celle du fichier.</p>
  {:else}
    <p class="fact muted" data-tally="decisions">
      {tally(shown.length, decisions.length, 'décision en vigueur', 'décisions en vigueur')}
    </p>
    <div class="scroll" data-rows="decisions">
      <ul class="rows">
        {#each shown as decision (decision.product_id)}
          <li>
            <button type="button" class="pick" onclick={() => onchoose(decision.product_id)}>
              Reprendre cette décision
            </button>
            <span class="what">{nameOf(decision.product_id)}</span>
            <span class="id">{decision.product_id}</span>
            <span class="message">{decisionSentence(decision)}</span>
            <span class="value">{decision.reason}</span>
            <span class="line">{frenchDate(decision.decided_at)}</span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</Panel>

<style>
  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  /* Bounded, and scrolling inside its own box: the page body never scrolls because of a
     list — it is the list that scrolls. */
  .scroll {
    max-height: 24rem;
    overflow: auto;
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
    background: var(--bg);
  }

  .rows {
    margin: 0;
    padding: 0 0.75rem;
    list-style: none;
  }

  .rows li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .rows li:first-child {
    border-top: none;
  }

  .line,
  .id,
  .value {
    color: var(--ink-muted);
  }

  .line {
    flex: none;
    width: 7rem;
  }

  .what {
    font-weight: 700;
  }

  .message {
    flex: 1 1 20rem;
  }

  .pick {
    height: 2.75rem;
    padding: 0 0.5rem;
    font-weight: 700;
    color: var(--ink);
    text-decoration: underline;
    border-radius: var(--radius-sm);
    transition: background-color var(--tap) var(--ease);
  }

  @media (hover: hover) {
    .pick:hover:not(:disabled) {
      background: var(--surface);
    }
  }
</style>
