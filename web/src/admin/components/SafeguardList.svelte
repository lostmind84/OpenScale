<script lang="ts">
  import Field from './Field.svelte'
  import Toggle from './Toggle.svelte'
  import type { Draft } from '../lib/draft.svelte'
  import { PLACEHOLDERS, SAFEGUARDS } from '../lib/safeguards'
  import { numberTyped, restoreBox } from '../lib/typed-values'

  /**
   * Les quatorze garde-fous, dans l'ordre d'évaluation, chacun dans son cadre.
   *
   * L'ordre compte : le premier verdict bloquant décide de ce que le client lit. Un
   * tableau les aurait mis côte à côte et forcé un défilement horizontal dès qu'une règle
   * porte deux seuils ; empilés, chaque règle se lit d'un bloc — ce qu'elle refuse, ce que
   * le client lit, et le nombre qui en décide.
   *
   * Le seuil se modifie ici et la sévérité jamais : elle dit si le poste refuse ou
   * avertit, ce qui ne dépend pas du magasin (§6.4, ADR-025).
   */
  interface Props {
    draft: Draft
  }

  const { draft }: Props = $props()

  /**
   * Writes a threshold, and writes NOTHING when the box holds nothing usable.
   *
   * @param path - the dotted path of the key.
   * @param typed - what the box holds.
   */
  function writeNumber(path: string, typed: string): void {
    const value = numberTyped(typed)
    if (value === null) return
    draft.set(path, value)
  }
</script>

<ol class="rules">
  {#each SAFEGUARDS as rule (rule.code)}
    <li class="rule" data-code={rule.code}>
      <p class="rule-head">
        <span class="rank">{rule.rank}</span>
        <span class="rule-label">{rule.label}</span>
        <code class="token">{rule.code}</code>
        <span class="severity" data-blocking={String(rule.blocking)}>{rule.severity}</span>
      </p>
      <p class="when">{rule.when}</p>

      {#if rule.message === ''}
        <p class="quote silent">Rien ne s’affiche au client : c’est une information.</p>
      {:else}
        <p class="quote">« {rule.message} »</p>
      {/if}

      {#if rule.switchPath !== ''}
        <Toggle
          path={rule.switchPath}
          label={rule.switchLabel}
          on={draft.flag(rule.switchPath)}
          onchange={(on) => draft.set(rule.switchPath, on)}
        />
      {/if}

      {#each rule.thresholds as threshold (threshold.path)}
        <!--
          The wrapper carries no layout — `display: contents` — and exists for the
          event alone: `focusout` bubbles to it, and that is where a box left empty
          gets the value of the file back. `Field` has no such hook, and it should not
          grow one for a rule that belongs to this page.
        -->
        <div
          class="box"
          onfocusout={(event) => restoreBox(event.target, draft.text(threshold.path))}
        >
          <Field
            label={threshold.label}
            path={threshold.path}
            kind="number"
            value={draft.text(threshold.path)}
            hint={threshold.hint}
            onchange={(value) => writeNumber(threshold.path, value)}
          />
        </div>
      {/each}

      {#if rule.note !== ''}<p class="note">{rule.note}</p>{/if}
    </li>
  {/each}
</ol>

<p class="fact muted">
  Un seuil vidé garde la valeur du fichier : il n’écrit pas zéro, et la case la retrouve
  dès qu’on quitte le champ. Pour changer un seuil, on tape l’autre valeur.
</p>

<p class="fact muted">
  Les marqueurs {PLACEHOLDERS.join(' et ')} sont remplacés par les valeurs de la pesée au
  moment où le message s’affiche.
</p>

<style>
  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  .rules {
    margin: 0.75rem 0 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .rule {
    padding: 0.75rem 1rem 1rem;
    background: var(--bg);
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
  }

  .rule-head {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
    margin: 0;
  }

  .rank {
    flex: none;
    min-width: 1.75rem;
    color: var(--ink-muted);
    font-variant-numeric: tabular-nums;
    font-weight: 700;
  }

  .rule-label {
    font-size: 1.125rem;
    font-weight: 700;
  }

  /* The English token of the service is only good for the telephone and the journal: it
     stands second, never in the place of the French label. */
  .token {
    color: var(--ink-muted);
    font-size: 0.9375rem;
  }

  .severity {
    margin-left: auto;
    padding: 0.125rem 0.625rem;
    font-size: 0.9375rem;
    border-radius: var(--radius-pill);
    background: var(--waiting-wash);
  }

  .severity[data-blocking='true'] {
    background: var(--warning-wash);
  }

  .when {
    margin: 0.375rem 0 0;
    font-size: 1rem;
  }

  /*
   * What the customer reads, laid out as a small screen inside the screen: it is a
   * QUOTATION of the shipped message (`internal/domain/safeguard.go`), not a sentence
   * written here.
   */
  .quote {
    margin: 0.5rem 0 0;
    padding: 0.5rem 0.75rem;
    font-size: 1.0625rem;
    background: var(--surface);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
  }

  .quote.silent {
    color: var(--ink-muted);
    font-size: 1rem;
    box-shadow: none;
    background: var(--waiting-wash);
  }

  .note {
    margin: 0.5rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  /* A frame for an EVENT and not for a layout: the field keeps the place it had. */
  .box {
    display: contents;
  }
</style>
