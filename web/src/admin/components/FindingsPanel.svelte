<script lang="ts">
  import Panel from './Panel.svelte'
  import type { FindingDTO } from '../lib/dto'
  import { frenchInteger } from '../lib/format'
  import { findingsUnknownSentence, type ReadState } from '../lib/read-state'
  import { tally } from '../lib/tally'

  /**
   * One list of what the last import reported, capped and announcing its total.
   *
   * Three panels of the Catalogue page are this component under three headings, and they
   * are the same object seen three times: a row, its CSV line, the offending value and
   * the reason in French. What tells them apart is what the reader is meant to DO — the
   * anomalies are a work plan for Odoo, the diverging units a wrong label on a product
   * that stays on sale, and the not-weighable ones a neutral inventory where nobody is at
   * fault (§10.3 bis).
   *
   * The name comes from the import itself and never from the catalog in service: it is
   * the one the file carried, so the findings of an import from March keep saying what
   * that import said, and a batch the station refused — whose products never entered the
   * base at all — is named too.
   */
  interface Props {
    title: string
    /** What the list is, in one sentence, under its heading. */
    note: string
    /** The name this list answers to in the DOM, for the tests and for nothing else. */
    list: string
    /** Where the read of the import history stands. */
    state: ReadState
    /** Every finding of this kind, cap included: the cap is applied here. */
    findings: FindingDTO[]
    /** What one of them is called. */
    singular: string
    /** What several of them are called. */
    plural: string
    /** What the panel reads when the last import reported none. */
    none: string
    /** What to do about the ones the cap hides, empty when there is nothing to add. */
    remedy?: string
  }

  const { title, note, list, state, findings, singular, plural, none, remedy = '' }: Props =
    $props()

  /** How many rows one list of findings draws. The file itself carries the rest. */
  const FINDINGS_SHOWN = 50

  const shown = $derived(findings.slice(0, FINDINGS_SHOWN))
  const truncated = $derived(findings.length > shown.length)
</script>

<Panel {title} {note}>
  {#if state !== 'read'}
    <p class="fact" data-unread={list}>{findingsUnknownSentence(state)}</p>
  {:else if findings.length === 0}
    <p class="fact">{none}</p>
  {:else}
    <p class="fact muted" data-tally={list}>
      {tally(shown.length, findings.length, singular, plural)}
      {#if truncated && remedy !== ''}
        {remedy}
      {/if}
    </p>
    <!--
      Deliberately UNKEYED: `csv_line` is not a key. One CSV row raises one finding per
      problem it has — a zero price AND a wrong check digit is two findings on line 42 —
      and a keyed each would have thrown `each_key_duplicate` on the first such file,
      taking the whole administration screen down with it.
    -->
    <div class="scroll" data-rows={list}>
      <ul class="rows">
        {#each shown as finding}
          <li>
            <span class="line">ligne {frenchInteger(finding.csv_line)}</span>
            <!--
              The NAME first, and the Odoo id after it: « 4412 » is a number somebody has to
              look up before they can start, « TOMATE GRAPPE » is the product they already
              know. The id stays because it is what opens the record in Odoo.

              Drawn only when the import recorded one. Two findings never carry a name — one
              that bears on no product, and a row too damaged to hold one, which is exactly
              what UNREADABLE_ROW says in its own message — and inventing a wording for that
              absence would state a fact this page has not read.
            -->
            {#if finding.product_name !== ''}
              <span class="what" data-name>{finding.product_name}</span>
            {/if}
            <span class="id">{finding.product_id}</span>
            <span class="value">{finding.value}</span>
            <span class="message">{finding.message}</span>
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

  /*
   * Every list of this page is BOUNDED and scrolls inside its own box.
   *
   * A real file carries 116 anomalies; unbounded, the page grew past 17 000 px and the
   * panels below it — the decisions, the history — stopped existing for whoever never
   * scrolled that far.
   */
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
</style>
