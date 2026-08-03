<script lang="ts">
  import { fetchCatalog } from '../../lib/api'
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import SafeguardList from '../components/SafeguardList.svelte'
  import TierGrid from '../components/TierGrid.svelte'
  import Toggle from '../components/Toggle.svelte'
  import WaiverList from '../components/WaiverList.svelte'
  import type { Draft } from '../lib/draft.svelte'
  import type { HealthDTO } from '../lib/dto'
  import { frenchInteger } from '../lib/format'
  import { productNameOf, type ReadState } from '../lib/read-state'
  import { frenchSeverity, labelOfCode } from '../lib/safeguards'

  /**
   * The Rules page of §14.4: the tier grid, the two roundings, the FOURTEEN safeguards,
   * the barcode block and the waivers of §10.6.
   *
   * ONE thing here is a design decision, and TWO are gaps to §14.4 that this file writes
   * down rather than dresses up.
   *
   * The design decision is the severity: it is read-only because it says whether a
   * station refuses or warns, which is not a shop setting (§6.4, ADR-025).
   *
   * The first gap is the MESSAGE, quoted and not editable. §6.4 says the opposite -- the
   * messages "sont éditables depuis l'écran Règles" -- and what is missing is a
   * configuration key per message and the route that writes it, NOT the validation: the
   * service already carries `domain.CheckMessage` and `domain.MessagePlaceholders`,
   * written "so that the Rules screen can list them next to the field being edited".
   *
   * The second gap is the live preview §14.4 asks for by name ("à 8 g, ce produit serait
   * refusé"). `domain.Evaluate` returns EVERY diagnostic expressly so this screen can
   * show it, but no route evaluates a weighing that has not happened -- and the answer
   * must come from the kernel, never from a second implementation in the browser that
   * would end up contradicting the label coming out of the printer. What is shown
   * instead is what the safeguards ACTUALLY said about the cycle in flight.
   */
  interface Props {
    draft: Draft
    health: HealthDTO
  }

  const { draft, health }: Props = $props()

  const diagnostics = $derived(health.state.diagnostics)
  const waivers = $derived(health.decisions.filter((decision) => decision.min_weight_g !== null))

  /** Product names by Odoo id, read from the catalog in service. */
  let names = $state<Record<string, string>>({})
  /**
   * Where the reading of the catalog stands.
   *
   * Three states and not a boolean, because an unknown id means three different things.
   * While it is `loading` it means « not read yet », for the length of a round trip; when
   * it is `unread` it means « the station did not answer », which says nothing about the
   * product; only `read` allows the screen to say « absent from the catalog in service »,
   * which is an accusation and must be earned by an answer.
   */
  let namesState = $state<ReadState>('loading')

  void loadNames()

  /**
   * Reads the catalog in service, to NAME the products a waiver applies to.
   *
   * §14.4 asks for "un produit nommé par ligne", and the health payload carries only an
   * Odoo id. The catalog comes from the CLIENT route, which asks for no password and
   * which the browser may already hold in cache: it is the only way to name a product
   * here without inventing a second search route. The Catalogue page does the same.
   */
  async function loadNames(): Promise<void> {
    try {
      const catalog = await fetchCatalog()
      names = Object.fromEntries(
        catalog.products.map((product): [string, string] => [product.id, product.name]),
      )
      namesState = 'read'
    } catch {
      // A station that cannot answer must not empty the list of waivers: the id alone is
      // still enough to find the product in Odoo, and a blank line would say « none ».
      namesState = 'unread'
    }
  }

  /**
   * The name of a product, or an honest sentence when the catalog does not carry it.
   *
   * @param id - the Odoo id of the product.
   */
  function nameOf(id: string): string {
    return productNameOf(names[id], namesState, 'Nom non lu')
  }

  /** « 1 verdict » / « 3 verdicts », and what they are counted against. */
  const verdictCount = $derived(
    `${frenchInteger(diagnostics.length)} ` +
      `${diagnostics.length > 1 ? 'verdicts' : 'verdict'} sur les quatorze garde-fous.`,
  )
</script>

<div class="pages">
  <Panel
    title="Grille de tarifs"
    note="Un second tarif n’est pas une case à cocher : c’est une ligne de plus dans cette grille."
  >
    <TierGrid {draft} />
    <Field
      label="Tarif imprimé en grand"
      path="pricing.primary_code"
      value={draft.text('pricing.primary_code')}
      hint="Le prix que le client lit sur l’étiquette (A7)."
      onchange={(value) => draft.set('pricing.primary_code', value)}
    />
    <Field
      label="Tarif qui serait encodé si le code-barres portait un prix"
      path="pricing.reference_code"
      value={draft.text('pricing.reference_code')}
      hint="Le plan livré ne porte pas de prix, mais un poids ou un nombre d’unités : la caisse retrouve le prix par la référence, dans Odoo, et ne doit jamais sous-facturer."
      onchange={(value) => draft.set('pricing.reference_code', value)}
    />
  </Panel>

  <Panel title="Les deux arrondis">
    <Field
      label="Arrondi du prix unitaire dérivé"
      path="pricing.unit_price_rounding"
      value={draft.text('pricing.unit_price_rounding')}
      hint="Il s’applique au prix au kilo calculé par la remise."
      onchange={(value) => draft.set('pricing.unit_price_rounding', value)}
    />
    <Field
      label="Arrondi du montant"
      path="pricing.amount_rounding"
      value={draft.text('pricing.amount_rounding')}
      hint="Il s’applique au montant de l’étiquette."
      onchange={(value) => draft.set('pricing.amount_rounding', value)}
    />
    <p class="fact muted">
      Les deux arrondis sont distincts parce qu’ils tombent à des endroits différents du
      calcul : arrondir le prix au kilo puis le multiplier, ou multiplier puis arrondir, ne
      donne pas le même centime sur une étiquette. Le poste applique l’arrondi commercial,
      et l’écart se voit au centime près sur la même pesée.
    </p>
  </Panel>

  <Panel
    title="Les quatorze garde-fous, dans l’ordre d’évaluation"
    note="Le seuil se modifie ici. La sévérité ne se règle pas : elle dit si le poste refuse ou avertit, et cela ne dépend pas du magasin. Le message affiché au client n’est pas encore modifiable depuis cet écran."
  >
    <p class="fact muted">
      L’ordre compte : le premier verdict bloquant décide de ce que le client lit. Les
      garde-fous 1 à 7 portent sur l’état de la balance, c’est-à-dire le poids brut ; les
      garde-fous 8 à 14 portent sur la vente, c’est-à-dire le net. Le code en gris est
      celui du journal, celui qu’on lit au téléphone.
    </p>

    <SafeguardList {draft} />

    <h3>Ce que les garde-fous disent de la pesée en cours</h3>
    {#if diagnostics.length === 0}
      <!--
        What an empty list says, and what it does not. These verdicts bear on the CYCLE
        IN FLIGHT: the machine clears them on its way back to rest. « No weighing has
        been submitted since start-up » was therefore false from the second second of
        operation, and cast doubt on a station that had just printed a label.
      -->
      <p class="fact" data-verdicts="0">
        Aucun verdict en ce moment : ces lignes portent sur la pesée EN COURS, et le poste
        est au repos. Elles apparaissent le temps d’un cycle, quand un client pose son sac
        et touche une tuile.
      </p>
    {:else}
      <p class="fact muted" data-verdicts={String(diagnostics.length)}>{verdictCount}</p>
      <ul class="verdicts">
        {#each diagnostics as verdict (verdict.code)}
          <li data-blocking={String(verdict.blocking)}>
            <span class="what">{labelOfCode(verdict.code)}</span>
            <code class="token">{verdict.code}</code>
            <span class="detail">{frenchSeverity(verdict.severity)}</span>
            {#if verdict.message !== ''}<span class="message">« {verdict.message} »</span>{/if}
            <!--
              The product id, when the verdict carries one. Rule 13 is the only one that
              fills it and its message is EMPTY: without this, the line read « Produit
              léger autorisé » without saying which product — that is, without the one
              piece of information the rule exists to record (§6.4, ADR-017).
            -->
            {#if verdict.product_id !== ''}
              <span class="detail">{nameOf(verdict.product_id)}</span>
              <code class="token">{verdict.product_id}</code>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel
    title="Code-barres"
    note="Un seul réglage, et c’est voulu : le reste du plan de numérotation n’est pas de la configuration."
  >
    <Toggle
      path="barcode.verify_reference_check_digit"
      label="Refuser une référence dont la clé de contrôle est fausse"
      hint="Décoché, le poste recalcule une clé juste sur une référence fausse, en silence — et la caisse encaisse un autre article."
      on={draft.flag('barcode.verify_reference_check_digit')}
      onchange={(on) => draft.set('barcode.verify_reference_check_digit', on)}
    />
    <p class="fact muted">
      Le plan de numérotation n’est PAS ici, et c’est tout l’intérêt : préfixes, largeur de
      la référence, largeur de la charge utile, décimales et mode de vente sont une
      constante du binaire, indexée par préfixe et vérifiée au démarrage.
      Un champ qui change le SENS du code lu par la caisse n’est pas un réglage, c’est un
      contrat externe : il change avec une version du binaire, relue et testée, jamais
      depuis l’écran d’un poste.
    </p>
  </Panel>

  <!--
    « Dérogations de poids minimum », and no longer « en vigueur » in the title: one row of
    `local_decisions` carries both the waiver and the choice to withdraw the product, so
    part of this list may be in force for nothing. Which ones is said line by line, and in
    the total above them, rather than promised by a heading.
  -->
  <Panel
    title="Dérogations de poids minimum"
    note="En lecture ici ; elles se modifient depuis l’onglet Catalogue, là où se trouve le produit."
  >
    <WaiverList {waivers} {names} {namesState} />
  </Panel>
</div>

<style>
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  h3 {
    margin: 1.5rem 0 0.5rem;
    font-size: 1.25rem;
  }

  .verdicts {
    margin: 0;
    padding: 0;
    list-style: none;
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

  .verdicts li[data-blocking='true'] {
    border-left: 0.25rem solid var(--warning);
    background: var(--warning-wash);
    padding-left: 0.5rem;
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
</style>
