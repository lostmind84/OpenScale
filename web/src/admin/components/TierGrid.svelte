<script lang="ts">
  import type { Draft } from '../lib/draft.svelte'
  import { discountText, previewOf, tiersOf } from '../lib/tiers'
  import { discountTyped, restoreBox } from '../lib/typed-values'

  /**
   * La grille de tarifs, telle que le fichier la porte et telle qu'on l'édite.
   *
   * Un second tarif n'est pas une case à cocher : c'est une ligne de plus dans cette
   * grille. Ce qui est éditable ici est le libellé, l'abrégé et la remise ; le code et
   * l'ordre viennent du fichier, et le tarif de référence ne porte jamais de remise —
   * c'est le prix du catalogue Odoo.
   */
  interface Props {
    draft: Draft
  }

  const { draft }: Props = $props()

  const tiers = $derived(tiersOf(draft))

  /** The code of the tier that IS the catalog price, and carries no discount. */
  const referenceCode = $derived(String(draft.value('pricing.reference_code') ?? ''))

  /** A French plural the screen must not get wrong. */
  function tierCount(count: number): string {
    return count <= 1 ? `${String(count)} tarif déclaré` : `${String(count)} tarifs déclarés`
  }

  /**
   * Writes a discount, and writes NOTHING when the box holds nothing usable.
   *
   * @param path - the dotted path of the key.
   * @param typed - what the box holds.
   */
  function writeDiscount(path: string, typed: string): void {
    const value = discountTyped(typed)
    if (value === null) return
    draft.set(path, value)
  }
</script>

{#if tiers.length === 0}
  <p class="fact">Aucun tarif déclaré dans la configuration lue.</p>
{:else}
  <p class="fact muted" data-tier-count>{tierCount(tiers.length)}.</p>
  <div class="scroll">
    <table>
      <thead>
        <tr>
          <th>Code</th>
          <th>Libellé</th>
          <th>Abrégé</th>
          <th>Remise</th>
          <th>Ordre</th>
        </tr>
      </thead>
      <tbody>
        <!--
          Keyed by POSITION, and that is not a shortcut. `tiersOf` replaces a missing
          code by the empty string, so two tiers without one collided on the same key
          and Svelte drew a single row. The position is also what the edits below
          write -- `pricing.tiers.<n>.<field>` -- so the row and its key say the same
          thing, and a reordered grid can no longer write into the wrong tier.
        -->
        {#each tiers as tier, index (index)}
          <tr>
            <td>{tier.code}</td>
            <td>
              <input
                aria-label="Libellé du tarif {index + 1}"
                value={tier.label}
                oninput={(event) =>
                  draft.set(`pricing.tiers.${String(index)}.label`, event.currentTarget.value)}
              />
            </td>
            <td>
              <input
                aria-label="Abrégé du tarif {index + 1}"
                value={tier.abbrev}
                oninput={(event) =>
                  draft.set(`pricing.tiers.${String(index)}.abbrev`, event.currentTarget.value)}
              />
            </td>
            <td>
              <!--
                The reference tier is split in two by PRESENCE, not by legality. An
                operator can retarget `pricing.reference_code` onto a tier that already
                carries an ordinary, legal discount (the field just below does exactly
                that, mid-session, with no file edit involved) -- and a screen that then
                printed « pas de remise » over a document that says 20 % would be
                hiding a declared value, which is as dishonest as inventing one. So the
                reference tier with NO key gets the reassuring sentence, and the
                reference tier WITH one gets told what saving will do to it.
              -->
              {#if tier.code === referenceCode && tier.written === null}
                <span class="locked">Prix du catalogue Odoo — pas de remise</span>
              {:else if tier.code === referenceCode}
                <span class="locked">
                  {tier.written} — le tarif de référence est le prix du catalogue : il
                  ne peut pas porter de remise, et l’enregistrement la refusera.
                </span>
              {:else if tier.written !== null && tier.discount === null}
                <span class="locked">
                  {tier.written} — une remise s’écrit au dixième de point ; celle-ci se
                  change dans le fichier de configuration.
                </span>
              {:else}
                <!--
                  Catches BOTH the ordinary case (tier.discount holds a value) and a
                  non-reference tier that carries no `discount_percent` key at all
                  (tier.written === null): for a tier that is not the reference, an
                  absent key means exactly 0 % (the kernel's own rule), and an editable
                  field showing 0 is honest, not invented -- unlike the reference row
                  above, which has nothing to show because it has no discount to hold.
                -->
                <input
                  type="text"
                  inputmode="decimal"
                  aria-label="Remise du tarif {index + 1}"
                  value={discountText(tier.discount ?? 0)}
                  oninput={(event) =>
                    writeDiscount(
                      `pricing.tiers.${String(index)}.discount_percent`,
                      event.currentTarget.value,
                    )}
                  onfocusout={(event) =>
                    restoreBox(event.currentTarget, discountText(tier.discount ?? 0))}
                /> %
                <span class="hint">
                  un produit à 10,00 €/kg s’affiche {previewOf(tier.discount ?? 0)} €/kg
                </span>
              {/if}
            </td>
            <td>{tier.rank}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  <p class="fact muted">
    Un champ vidé garde la valeur du fichier : il n’écrit pas zéro, et la case la
    retrouve dès qu’on quitte le champ. Une remise effacée serait le plein tarif pour
    tous les adhérents.
  </p>
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

  /* A cell this screen must not let the operator edit: the catalog price, or a discount
     it cannot show without inventing a figure nobody declared. Text only, no field
     border -- there is nothing here to click into. */
  .locked {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  /* A wide table scrolls INSIDE its frame: the body of the page never scrolls
     horizontally. */
  .scroll {
    overflow-x: auto;
  }

  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.5rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  th {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  input {
    /* 44 px: the density of the settings pages, which are driven with a mouse (ADR-033).
       The 72 px of the client screen stay for destructive gestures, and this page has
       none. */
    min-height: 2.75rem;
    width: 100%;
    min-width: 6rem;
    padding: 0 0.5rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .hint {
    flex: 1 1 20rem;
    color: var(--ink-muted);
    font-size: 1rem;
  }
</style>
