<script lang="ts">
  import type { TechnicalLineDTO } from '../lib/dto'
  import { logSourceLabelOf } from '../lib/fields'
  import { frenchDateTime } from '../lib/format'
  import { LEVELS, french } from '../lib/journal-words'
  import { preferences } from '../lib/preferences.svelte'

  /**
   * Les lignes du journal technique, bornées et défilant dans leur propre cadre.
   *
   * Deux écrans lisent le même journal — le tableau de bord ses dix dernières lignes, la
   * page Journal ses cinquante —, et rien de ce qui est ici ne décide combien : la liste
   * arrive faite, et ce composant la dessine.
   */
  interface Props {
    lines: TechnicalLineDTO[]
  }

  const { lines }: Props = $props()
</script>

<div class="lines-box" data-scroll="technical">
  <ul class="lines">
    {#each lines as line (line.id)}
      <li data-level={line.level}>
        <span class="when">{frenchDateTime(line.occurred_at)}</span>
        <span class="level">{french(LEVELS, line.level, 'niveau inconnu')}</span>
        <span class="from">{logSourceLabelOf(line.source)}</span>
        <!--
          The event code is a TECHNICAL NAME and sits behind the switch, like the
          configuration keys: `ERR-CAT-05` teaches nothing to whoever will never open
          the source, and the French message beside it says what happened on its own.

          It stays reachable in three places, which is why hiding it costs nothing:
          the switch is two clicks away in the rail, `technical.csv` inside
          `diagnostic.zip` carries the `code` column whatever the screen shows
          (internal/diag/archive.go), and the station's own text log keeps it. The CSV
          export of the Journal page never carried it — that one exports weighings, and a
          weighing has no event code (internal/web/admin.go).
        -->
        {#if line.code !== '' && preferences.showTechnicalNames}
          <span class="code">{line.code}</span>
        {/if}
        <span class="message">{line.message}</span>
        {#if line.detail !== ''}<span class="detail-text">{line.detail}</span>{/if}
      </li>
    {/each}
  </ul>
</div>

<style>
  /*
   * Nothing on this list may grow without a bound, and `overflow: auto` is what lets it
   * be as wide as it needs without the body of the page ever scrolling sideways.
   */
  .lines-box {
    overflow: auto;
    background: var(--bg);
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
    max-height: 28rem;
  }

  .lines {
    margin: 0;
    padding: 0 0.75rem;
    list-style: none;
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

  .lines li:first-child {
    border-top: none;
  }

  /* A line in fault carries a rule and never red ink: --fault reaches 6,54:1 on
     --surface, under the 7:1 that §14.2 demands of anything read. */
  .lines li[data-level='error'],
  .lines li[data-level='critical'] {
    border-left: 0.25rem solid var(--fault);
    padding-left: 0.5rem;
  }

  .lines li[data-level='warn'] {
    border-left: 0.25rem solid var(--warning);
    padding-left: 0.5rem;
  }

  .when,
  .level,
  .from,
  .code,
  .detail-text {
    color: var(--ink-muted);
  }

  .level {
    flex: none;
    width: 8rem;
  }

  .message {
    flex: 1 1 20rem;
    font-weight: 700;
  }
</style>
