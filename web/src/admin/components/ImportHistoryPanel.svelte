<script lang="ts">
  import Panel from './Panel.svelte'
  import type { ImportDTO } from '../lib/dto'
  import { frenchDateTime, frenchInteger } from '../lib/format'
  import { importResultWord, importSourceWord } from '../lib/inventory'
  import { historyUnknownSentence, type ReadState } from '../lib/read-state'
  import { importTally } from '../lib/tally'

  /**
   * The imports the station kept, one row each, with no English token on screen.
   *
   * The result and the source are the station's own words translated by the shared
   * module: three screens read the same tokens — the dashboard, this table, and the
   * sentence « Recharger le catalogue » leaves behind — and three copies would have
   * ended up saying two different things about one and the same fate.
   */
  interface Props {
    /** What the route served, twenty at most: the ceiling is the station's. */
    imports: ImportDTO[]
    /** Where the read of the import history stands. */
    state: ReadState
  }

  const { imports, state }: Props = $props()

  /** How an import ended, in French — never the token the service wrote. */
  function frenchResult(record: ImportDTO): string {
    const said = importResultWord(record.result)
    return record.code === '' ? said : `${said} (${record.code})`
  }
</script>

<Panel title="Vingt derniers imports">
  {#if state !== 'read'}
    <p class="fact" data-unread="imports">{historyUnknownSentence(state)}</p>
  {:else if imports.length === 0}
    <p class="fact">Aucun import dans l’historique.</p>
  {:else}
    <p class="fact muted" data-tally="imports">{importTally(imports.length)}</p>
    <div class="scroll">
      <table>
        <thead>
          <tr>
            <th>Quand</th>
            <th>Fichier</th>
            <th>Source</th>
            <th>Résultat</th>
            <th>Motif</th>
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
              <td>{importSourceWord(record.source)}</td>
              <td data-result>{frenchResult(record)}</td>
              <td class="why">{record.reason}</td>
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
     table — it is the table that scrolls. */
  .scroll {
    max-height: 24rem;
    overflow: auto;
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
    background: var(--bg);
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

  /* The reason a file was refused is a sentence, and the one cell allowed to wrap. */
  .why {
    white-space: normal;
    min-width: 16rem;
    color: var(--ink-muted);
  }

  th {
    position: sticky;
    top: 0;
    color: var(--ink-muted);
    font-size: 1rem;
    background: var(--bg);
  }
</style>
