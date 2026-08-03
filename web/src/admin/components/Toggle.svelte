<script lang="ts">
  import { preferences } from '../lib/preferences.svelte'

  /**
   * A configuration flag, drawn the same way wherever one is offered.
   *
   * `Field` has no boolean kind and should not grow one: a switch is not a box one types
   * in, and the two do not read alike. What this component exists for is that the Rules
   * page and the Catalogue page each drew their own, by hand, and the two had drifted.
   *
   * THE DRIFT WAS ON THE HINT, and merging them made a choice that is worth stating.
   * Rules wrote `{#if hint !== ''}<span class="hint">…`, the Catalogue page wrote the span
   * unconditionally. This component keeps Rules' version: `.hint` carries `flex: 1 1 20rem`,
   * so an empty span there still claims a flex track — a blank column beside a switch that
   * has nothing to explain.
   *
   * That case is NOT hypothetical, and that is what settles the choice. Of the three
   * callers, two pass a sentence — the by-unit switch of the Catalogue page and the check
   * digit of the Rules page — and the third passes none: `SafeguardList` draws the switch
   * of safeguard 3, whose whole explanation is the rule around it. Rules already fed it an
   * empty string (`{@render toggle(rule.switchPath, rule.switchLabel, '')}`), so its
   * conditional was the version that carried the case. Keeping the other one would have
   * added an empty span, and a flex track, under safeguard 3.
   *
   * A NOTE ON HOW THIS PARAGRAPH WAS WRONG BEFORE, because the mistake is easy to repeat.
   * It used to claim the drift was that "one of them showed its key even with the switch
   * off". Neither did: both pages carried the same `{#if preferences.showTechnicalNames}`.
   * The sentence had been lifted from a comment in Rules.svelte written in the PAST tense
   * about a defect already fixed — and a past-tense sentence, moved into a new file, is
   * read in the present. A comment that travels can turn false without a word of it
   * changing.
   *
   * It IS a field, so the same rule applies: the technical name sits behind « Montrer les
   * noms techniques », or it sits nowhere.
   *
   * The key travels as a NAMED property and never as a positional argument, so that it
   * reads as `path=` — the form the field index checks for, and the only thing that keeps
   * a switch from being added without its French name.
   */
  interface Props {
    /** The dotted path of the key, shown only when the technical names are asked for. */
    path: string
    label: string
    /** Ce que ce réglage décide, en une phrase. */
    hint?: string
    /** Whether the flag is on, as the draft holds it. */
    on: boolean
    onchange: (on: boolean) => void
  }

  const { path, label, hint = '', on, onchange }: Props = $props()
</script>

<label class="toggle" data-flag={path} data-on={String(on)}>
  <input type="checkbox" checked={on} onchange={(event) => onchange(event.currentTarget.checked)} />
  <span class="toggle-text">
    <span class="toggle-label">{label}</span>
    <!--
      Behind the switch, exactly as `Field` puts it. The key written unconditionally was
      read on screen as part of the sentence: « Ce poste travaille avec un panier taré
      limits.basket_check_enabled ». The switch has to hide it here too, or it hides
      nothing.
    -->
    {#if preferences.showTechnicalNames}<code>{path}</code>{/if}
    {#if hint !== ''}<span class="hint">{hint}</span>{/if}
  </span>
</label>

<style>
  .toggle {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-height: 2.75rem;
    margin: 0.5rem 0 0;
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--waiting-wash);
    /* Only the switch's own name is bold, and the sentence behind it is prose. Declared
       here rather than left to the page: one of the two pages that draw a switch styles
       its labels bold, and the switch may not read differently there. */
    font-weight: 400;
    cursor: pointer;
    transition:
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease);
  }

  .toggle[data-on='true'] {
    background: var(--ready-wash);
  }

  /* What a mouse expects, and a finger never asked for (app.css). */
  @media (hover: hover) {
    .toggle:hover {
      border-color: var(--ink-muted);
    }
  }

  /*
   * The box itself.
   *
   * The last six declarations are the ones the `input` rule of a settings page used to
   * hand it — both pages carry such a rule, and both spell those six the same way. They
   * are repeated here rather than dropped: taking a component out of a page must change
   * where a rule is written, never what the browser draws.
   */
  .toggle input {
    flex: none;
    width: 1.5rem;
    height: 1.5rem;
    min-height: 0;
    min-width: 0;
    padding: 0;
    accent-color: var(--focus);
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .toggle-text {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
  }

  .toggle-label {
    font-size: 1.0625rem;
    font-weight: 700;
  }

  .hint {
    flex: 1 1 20rem;
    color: var(--ink-muted);
    font-size: 1rem;
  }
</style>
