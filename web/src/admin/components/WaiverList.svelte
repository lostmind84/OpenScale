<script lang="ts">
  import type { DecisionDTO } from '../lib/dto'
  import { frenchDate, frenchInteger } from '../lib/format'
  import { productNameOf, type ReadState } from '../lib/read-state'

  /**
   * Les dérogations de poids minimum, en lecture.
   *
   * « Dérogations de poids minimum », et non plus « en vigueur » : une ligne de
   * `local_decisions` porte À LA FOIS la dérogation et le choix de retirer le produit
   * (§10.6), donc une partie de cette liste peut être en vigueur pour rien. Le garde-fou
   * 14 refuse alors le produit avant que le 8 ait un sens, et la ligne le dit — plutôt
   * qu'un titre qui promettrait le contraire.
   */
  interface Props {
    /** Toutes les dérogations enregistrées, plafond compris : il s'applique ici. */
    waivers: DecisionDTO[]
    /** The name of every product the station serves, by Odoo id. */
    names: Readonly<Record<string, string>>
    /** Where the read of the client catalog stands, so a missing name is explained. */
    namesState: ReadState
  }

  const { waivers, names, namesState }: Props = $props()

  /**
   * The name of a product, or an honest sentence when the catalog does not carry it.
   *
   * @param id - the Odoo id of the product.
   */
  function nameOf(id: string): string {
    return productNameOf(names[id], namesState, 'Nom non lu')
  }

  /** How many waivers are drawn before the list defers to the Catalogue page. */
  const WAIVERS_SHOWN = 20

  /**
   * The waivers that will never be reached, because the product is withdrawn.
   *
   * Calling those « en vigueur » would be false: they are in force for nothing.
   */
  const dead = $derived(waivers.filter((decision) => !decision.offered))
  const shown = $derived(waivers.slice(0, WAIVERS_SHOWN))

  /**
   * How many waivers are drawn, out of how many there are, and how many are in force.
   *
   * A waiver is counted in PRODUCTS: nothing bounds this list but the shop itself, so it
   * is capped when drawn — and a cap that does not say what it hides is a lie by
   * omission. The sentence therefore always carries the total, and it stops saying « en
   * vigueur » of the whole set as soon as one of them sits on a withdrawn product.
   */
  const total = $derived(
    `${frenchInteger(shown.length)} ` +
      `${shown.length > 1 ? 'lignes affichées' : 'ligne affichée'} sur ` +
      `${frenchInteger(waivers.length)} ` +
      (dead.length === 0
        ? `${waivers.length > 1 ? 'dérogations en vigueur' : 'dérogation en vigueur'}.`
        : `${waivers.length > 1 ? 'dérogations enregistrées' : 'dérogation enregistrée'}, ` +
          `dont ${frenchInteger(dead.length)} sur un produit retiré, sans effet : ` +
          'le garde-fou 14 refuse le produit avant que le 8 ait un sens.') +
      (waivers.length > shown.length
        ? ' Les autres se lisent produit par produit depuis l’onglet Catalogue.'
        : ''),
  )

  /**
   * The floor of a waiver, said at the BOUND the kernel actually applies.
   *
   * Rule 8 fires on `net <= floor` (`internal/domain/safeguard.go`), so a net weight
   * EQUAL to the floor is refused. « À partir de 8 g » said the opposite about the very
   * gram the waiver was posed for.
   *
   * @param grams - the floor this product may weigh.
   */
  function waiverFloor(grams: number): string {
    return `plancher ${frenchInteger(grams)} g : refusé à ${frenchInteger(grams)} g et en dessous`
  }
</script>

{#if waivers.length === 0}
  <p class="fact">Aucune dérogation : la limite générale s’applique à tous les produits.</p>
{:else}
  <p class="fact muted" data-waiver-total>{total}</p>
  {#if namesState === 'unread'}
    <p class="unread">
      Les noms de produits n’ont pas pu être lus : le catalogue en service n’a pas
      répondu. Les identifiants Odoo restent affichés.
    </p>
  {/if}
  <ul class="verdicts waivers">
    {#each shown as waiver (waiver.product_id)}
      <li data-withdrawn={String(!waiver.offered)}>
        <span class="what">{nameOf(waiver.product_id)}</span>
        <code class="token">{waiver.product_id}</code>
        <span class="detail">{waiverFloor(waiver.min_weight_g ?? 0)}</span>
        <span class="message">{waiver.reason}</span>
        <span class="detail">{frenchDate(waiver.decided_at)}, {waiver.decided_by}</span>
        {#if !waiver.offered}
          <span class="dead">
            Produit retiré : le garde-fou 14 refuse le produit avant que cette
            dérogation ait un sens.
          </span>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  .verdicts {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  /*
   * A list of waivers is counted in products, not in configuration lines: it is capped
   * when drawn AND inside its frame, and its total is announced above it.
   */
  .waivers {
    max-height: 24rem;
    overflow-y: auto;
  }

  .verdicts li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  /* The English token of the service is only good for the telephone and the journal: it
     stands second, never in the place of the French label. */
  .token {
    color: var(--ink-muted);
    font-size: 0.9375rem;
  }

  .what,
  .message {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }

  /* A waiver in force for nothing: the row stays, and it says why it decides nothing. */
  .dead {
    flex: 1 1 20rem;
    padding: 0.125rem 0.625rem;
    font-size: 1rem;
    background: var(--warning-wash);
    border-radius: var(--radius-sm);
  }

  /* A read that failed says so: a silent list would be read as « there are none », which
     is false. */
  .unread {
    margin: 0.5rem 0;
    padding: 0.5rem 0.75rem;
    font-size: 1rem;
    background: var(--fault-wash);
    border-left: 0.25rem solid var(--fault);
    border-radius: var(--radius-sm);
  }
</style>
