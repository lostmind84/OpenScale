<script lang="ts">
  import { fetchCatalog } from '../../lib/api'
  import { ALL_CATEGORIES, filterProducts, type Product } from '../../lib/catalog'
  import Inventory from '../components/Inventory.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { FindingDTO, HealthDTO, ImportDTO } from '../lib/dto'
  import { frenchDateTime, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Catalogue de §14.4.
   *
   * La liste des non-pesables est un **inventaire neutre** et jamais une liste d'erreurs :
   * un boulgour préemballé n'est pas en défaut, il ne relève pas de la balance. Les
   * anomalies, elles, portent chacune leur numéro de ligne du CSV, son motif en français et
   * la valeur fautive — c'est ce qui fait la différence entre « 16 anomalies » et un plan de
   * travail que quelqu'un peut suivre dans Odoo (§10.3 bis).
   */
  interface Props {
    admin: Admin
    health: HealthDTO
  }

  const { admin, health }: Props = $props()

  /** Combien de produits la recherche montre. Au-delà, on précise sa recherche. */
  const MATCHES_SHOWN = 20

  /** Les motifs qui décrivent un produit SANS TUILE, et qui ne sont pas des défauts. */
  const NOT_WEIGHABLE_CODES = [
    'NO_BARCODE',
    'PREPACKAGED_PRODUCT',
    'INTERNAL_CODE_NOT_WEIGHABLE',
  ]

  let imports = $state<ImportDTO[]>([])
  let findings = $state<FindingDTO[]>([])
  let products = $state<Product[]>([])
  let query = $state('')
  let chosen = $state<Product | null>(null)
  let reason = $state('')
  let waiver = $state('')

  const anomalies = $derived(findings.filter((finding) => finding.issue === 'anomaly'))
  const neutral = $derived(findings.filter((finding) => NOT_WEIGHABLE_CODES.includes(finding.code)))
  const mismatches = $derived(findings.filter((finding) => finding.code === 'UNIT_MISMATCH'))
  /**
   * Les produits que la recherche retient.
   *
   * C'est le MÊME filtre que la grille du client — insensible aux accents, un mot par
   * fragment tapé —, parce qu'un produit introuvable ici serait introuvable là-bas, et
   * qu'une deuxième normalisation dans le navigateur est exactement ce que le fichier
   * partagé `web/testdata/normalization.json` existe pour empêcher.
   */
  const matches = $derived(
    query === '' ? [] : filterProducts(products, ALL_CATEGORIES, query).slice(0, MATCHES_SHOWN),
  )

  void load()

  /** Lit l'historique des imports, les signalements du dernier, et le catalogue en service. */
  async function load(): Promise<void> {
    const history = await admin.load(() => api.fetchImports(health.catalog?.id))
    if (history !== null) {
      imports = history.imports
      findings = history.findings
    }
    // Le catalogue complet vient de la route CLIENT, qui ne demande pas de mot de passe et
    // que le navigateur a peut-être déjà en cache : c'est la seule façon de nommer un
    // produit dans une décision locale sans réinventer une route de recherche.
    const catalog = await admin.load(() => fetchCatalog())
    if (catalog !== null) products = catalog.products
  }

  /** Enregistre une décision humaine sur le produit choisi (§10.6, ADR-017). */
  async function decide(offered: boolean): Promise<void> {
    const product = chosen
    if (product === null) return
    const grams = waiver === '' ? null : Number(waiver)
    await admin.run(() =>
      api.saveDecision(product.id, { offered, min_weight_g: grams, reason }),
    )
    reason = ''
    waiver = ''
  }
</script>

<div class="pages">
  <Panel title="Dernier import">
    {#if health.catalog === null}
      <p class="fact">Aucun import enregistré sur ce poste.</p>
    {:else}
      <Inventory record={health.catalog} motives={health.catalog_motives} />
    {/if}
    <p class="fact muted" data-source>
      {health.catalog_source === null
        ? 'Aucune source de catalogue publiée par ce poste.'
        : 'Source : ' + health.catalog_source.label}
    </p>
    <div class="actions">
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(api.reloadCatalogAsExpert)}
      >
        Recharger le catalogue
      </button>
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(api.forgetQuarantine)}
      >
        Oublier la quarantaine
      </button>
    </div>
  </Panel>

  <Panel
    title="Anomalies à corriger dans Odoo"
    note="Chaque ligne porte son numéro dans le CSV, son motif et la valeur fautive."
  >
    {#if anomalies.length === 0}
      <p class="fact">Aucune anomalie sur le dernier import.</p>
    {:else}
      <ul class="rows">
        {#each anomalies as finding (finding.csv_line)}
          <li>
            <span class="line">ligne {frenchInteger(finding.csv_line)}</span>
            <span class="id">{finding.product_id}</span>
            <span class="value">{finding.value}</span>
            <span class="message">{finding.message}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel
    title="Unités divergentes"
    note="Le produit reste proposé : le code-barres fait foi, seul le libellé du prix est faux (§10.2)."
  >
    {#if mismatches.length === 0}
      <p class="fact">Aucune unité divergente sur le dernier import.</p>
    {:else}
      <ul class="rows">
        {#each mismatches as finding (finding.csv_line)}
          <li>
            <span class="line">ligne {frenchInteger(finding.csv_line)}</span>
            <span class="id">{finding.product_id}</span>
            <span class="message">{finding.message}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel
    title="Produits non pesables"
    note="Un inventaire, pas une liste d’erreurs : ces produits portent déjà leur code-barres et n’ont aucune raison d’être pesés."
  >
    {#if neutral.length === 0}
      <p class="fact">Aucun produit non pesable sur le dernier import.</p>
    {:else}
      <ul class="rows">
        {#each neutral as finding (finding.csv_line)}
          <li>
            <span class="line">ligne {frenchInteger(finding.csv_line)}</span>
            <span class="id">{finding.product_id}</span>
            <span class="value">{finding.value}</span>
            <span class="message">{finding.message}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel
    title="Décider d’un produit"
    note="Une seule table de décisions humaines : « ne plus proposer » et la dérogation de poids en sont deux colonnes (§14.5)."
  >
    <label class="search" for="product-search">Chercher un produit</label>
    <input id="product-search" type="search" bind:value={query} placeholder="ail, tomme, œufs…" />
    {#if query !== ''}
      <ul class="rows">
        {#each matches as product (product.id)}
          <li>
            <button type="button" class="pick touch-target" onclick={() => (chosen = product)}>
              {product.name}
            </button>
            <span class="id">{product.id}</span>
            <span class="value">{product.unit_price_text}{product.price_suffix}</span>
          </li>
        {/each}
      </ul>
    {/if}

    {#if chosen !== null}
      <div class="decision">
        <p class="fact">Produit choisi : <strong>{chosen.name}</strong> ({chosen.id})</p>
        <label for="decision-reason">
          Motif — ce qui rendra cette décision lisible dans six mois, par quelqu’un qui
          n’était pas là
        </label>
        <input id="decision-reason" type="text" bind:value={reason} />
        <label for="decision-waiver">
          Autoriser ce produit à peser moins que la limite générale, en grammes (facultatif)
        </label>
        <input id="decision-waiver" type="number" bind:value={waiver} />
        <div class="actions">
          <button
            type="button"
            class="action touch-target"
            disabled={admin.busy}
            onclick={() => void decide(false)}
          >
            Ne plus proposer ce produit
          </button>
          <button
            type="button"
            class="action touch-target"
            disabled={admin.busy}
            onclick={() => void decide(true)}
          >
            Le proposer de nouveau
          </button>
        </div>
      </div>
    {/if}
  </Panel>

  <Panel title="Vingt derniers imports">
    {#if imports.length === 0}
      <p class="fact">Aucun import dans l’historique.</p>
    {:else}
      <div class="scroll">
        <table>
          <thead>
            <tr>
              <th>Quand</th>
              <th>Fichier</th>
              <th>Résultat</th>
              <th>Lues</th>
              <th>Pesables</th>
              <th>Non pesables</th>
              <th>Anomalies</th>
              <th>Retirés</th>
            </tr>
          </thead>
          <tbody>
            {#each imports as record (record.id)}
              <tr>
                <td>{frenchDateTime(record.occurred_at)}</td>
                <td>{record.file_name}</td>
                <td>{record.result}{record.code === '' ? '' : ' (' + record.code + ')'}</td>
                <td>{frenchInteger(record.rows_read_count)}</td>
                <td>{frenchInteger(record.weighable_count)}</td>
                <td>{frenchInteger(record.not_weighable_count)}</td>
                <td>{frenchInteger(record.anomalies_count)}</td>
                <td>{frenchInteger(record.products_withdrawn_count)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
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

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    margin: 0.75rem 0 0;
  }

  .action {
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .rows {
    margin: 0.5rem 0 0;
    padding: 0;
    list-style: none;
    max-height: 24rem;
    overflow-y: auto;
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

  .line,
  .id,
  .value {
    color: var(--ink-muted);
  }

  .line {
    flex: none;
    width: 7rem;
  }

  .message {
    flex: 1 1 20rem;
  }

  .pick {
    padding: 0 0.5rem;
    font-weight: 700;
    text-decoration: underline;
  }

  .search,
  label {
    display: block;
    margin-top: 0.5rem;
    font-size: 1.0625rem;
    font-weight: 700;
  }

  input {
    min-height: 3rem;
    width: 100%;
    max-width: 34rem;
    padding: 0 0.75rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .decision {
    margin-top: 0.75rem;
    padding-top: 0.75rem;
    border-top: 1px solid var(--border);
  }

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
    white-space: nowrap;
    border-bottom: 1px solid var(--border);
  }

  th {
    color: var(--ink-muted);
    font-size: 1rem;
  }
</style>
