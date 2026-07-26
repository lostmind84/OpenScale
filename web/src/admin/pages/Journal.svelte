<script lang="ts">
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { TechnicalLineDTO, WeighingDTO } from '../lib/dto'
  import { frenchDateTime, frenchDuration, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Journal de §14.4 : les 200 dernières pesées, et le journal technique dessous.
   *
   * Le détail porte la TRAME BRUTE et le bouton « Rejouer cette trame ». C'est ce qui fait
   * d'un refus inexpliqué un test permanent, sans déplacement au magasin et sans balance :
   * la trame repart dans le décodeur du poste, et « ça se décode » veut alors dire « ça se
   * décode en service » (§15.4).
   */
  interface Props {
    admin: Admin
  }

  const { admin }: Props = $props()

  let weighings = $state<WeighingDTO[]>([])
  let technical = $state<TechnicalLineDTO[]>([])
  let result = $state('')
  let opened = $state<number | null>(null)

  /** Les filtres du journal, tels qu'ils partent en paramètres de requête. */
  const filters = $derived<Record<string, string>>(result === '' ? {} : { result })
  const detail = $derived(weighings.find((row) => row.id === opened) ?? null)

  void load()

  /** Lit une page du journal et une page du journal technique. */
  async function load(): Promise<void> {
    weighings = (await admin.load(() => api.fetchJournal(filters))) ?? []
    technical = (await admin.load(() => api.fetchTechnical({ limit: '50' }))) ?? []
  }

  /**
   * Rejoue une trame.
   *
   * Une trame VIDE n'est pas rejouable, et l'écran le dit plutôt que d'envoyer une requête
   * dont il connaît la réponse : `weighings.frame` n'est pas encore alimenté sur toutes les
   * lignes, et « aucune trame n'est fournie » ne dit rien à un bénévole.
   */
  async function replay(frame: string): Promise<void> {
    if (frame === '') {
      admin.error = 'Cette ligne ne porte aucune trame brute : il n’y a rien à rejouer.'
      return
    }
    await admin.run(() => api.replayFrame(frame))
  }
</script>

<div class="pages">
  <Panel title="Deux cents dernières pesées">
    <div class="filters">
      <label for="journal-result">Résultat</label>
      <select
        id="journal-result"
        bind:value={result}
        onchange={() => {
          void load()
        }}
      >
        <option value="">tous</option>
        <option value="printed">imprimées</option>
        <option value="rejected">refusées</option>
        <option value="failed">en échec</option>
      </select>
      <button type="button" class="action touch-target" onclick={() => void load()}>
        Rafraîchir
      </button>
      <a class="action touch-target" href={api.journalCSVURL(filters)} download>
        Exporter en CSV
      </a>
    </div>

    {#if weighings.length === 0}
      <p class="fact">Aucune pesée ne correspond.</p>
    {:else}
      <div class="scroll">
        <table>
          <thead>
            <tr>
              <th>Quand</th>
              <th>Produit</th>
              <th>Net</th>
              <th>Code-barres</th>
              <th>Résultat</th>
              <th>Durée</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {#each weighings as row (row.id)}
              <tr>
                <td>{frenchDateTime(row.occurred_at)}</td>
                <td>{row.product_name}</td>
                <td>{frenchInteger(row.net_g)} g</td>
                <td>{row.barcode}</td>
                <td>{row.result}</td>
                <td>{frenchDuration(row.duration_ms)}</td>
                <td>
                  <button
                    type="button"
                    class="pick touch-target"
                    onclick={() => (opened = opened === row.id ? null : row.id)}
                  >
                    {opened === row.id ? 'fermer' : 'détail'}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    {#if detail !== null}
      <div class="detail" data-detail>
        <h3>Pesée {frenchInteger(detail.id)}</h3>
        <dl>
          <dt>Produit</dt>
          <dd>{detail.product_name} ({detail.product_id})</dd>
          <dt>Référence</dt>
          <dd>{detail.reference}</dd>
          <dt>Brut / tare / net</dt>
          <dd>
            {frenchInteger(detail.gross_g)} g / {frenchInteger(detail.tare_g)} g /
            {frenchInteger(detail.net_g)} g
          </dd>
          <dt>Stabilité</dt>
          <dd>{detail.stability} — cadence {frenchDuration(detail.rate_ms)}</dd>
          <dt>Origine du poids</dt>
          <dd>{detail.source}</dd>
          <dt>Détail</dt>
          <dd>{detail.detail === '' ? 'aucun' : detail.detail}</dd>
          <dt>Trame brute</dt>
          <dd><code>{detail.frame === '' ? 'aucune trame enregistrée' : detail.frame}</code></dd>
        </dl>
        <button
          type="button"
          class="action touch-target"
          disabled={admin.busy}
          onclick={() => void replay(detail.frame)}
        >
          Rejouer cette trame
        </button>
      </div>
    {/if}
  </Panel>

  <Panel title="Journal technique">
    {#if technical.length === 0}
      <p class="fact">Aucune ligne technique.</p>
    {:else}
      <ul class="lines">
        {#each technical as line (line.id)}
          <li data-level={line.level}>
            <span class="when">{frenchDateTime(line.occurred_at)}</span>
            <span class="source">{line.source}</span>
            {#if line.code !== ''}<span class="code">{line.code}</span>{/if}
            <span class="message">{line.message}</span>
            {#if line.detail !== ''}<span class="detail-text">{line.detail}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>
</div>

<style>
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    align-items: center;
    margin-bottom: 0.75rem;
    font-size: 1.0625rem;
  }

  select {
    min-height: 3rem;
    padding: 0 0.5rem;
    font: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .action {
    display: inline-flex;
    align-items: center;
    padding: 0 1rem;
    font-size: 1.0625rem;
    font-weight: 700;
    text-decoration: none;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
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

  .pick {
    padding: 0 0.5rem;
    text-decoration: underline;
  }

  .detail {
    margin-top: 1rem;
    padding-top: 0.75rem;
    border-top: 1px solid var(--border);
  }

  .detail h3 {
    margin: 0 0 0.5rem;
    font-size: 1.25rem;
  }

  .detail dl {
    margin: 0 0 0.75rem;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
    font-size: 1.0625rem;
  }

  .detail dt {
    color: var(--ink-muted);
  }

  .detail dd {
    margin: 0;
  }

  .lines {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: 28rem;
    overflow-y: auto;
  }

  .lines li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .lines li[data-level='error'] {
    border-left: 0.25rem solid var(--fault);
    padding-left: 0.5rem;
  }

  .lines li[data-level='warn'] {
    border-left: 0.25rem solid var(--warning);
    padding-left: 0.5rem;
  }

  .when,
  .source,
  .code,
  .detail-text {
    color: var(--ink-muted);
  }

  .message {
    font-weight: 700;
  }
</style>
