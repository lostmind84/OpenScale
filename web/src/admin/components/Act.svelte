<script lang="ts">
  /**
   * A button of the administration screens.
   *
   * The colour states THE NATURE OF THE ACT and nothing else: neutral when it questions
   * the station, solid blue when it writes to it, solid red when one click will not undo
   * it. That is the only piece of information a volunteer can read without a legend.
   *
   * It exists because `.act` was redefined in four files, in variants that had drifted
   * apart, and because each of the thirty-seven buttons copied its badge and its
   * « En cours… » by hand.
   */
  interface Props {
    /** What the act does to the station: it reads it, it writes it, or it cannot be undone. */
    kind?: 'read' | 'write' | 'destructive'
    label: string
    /** True while THIS button is working: it says so, and refuses a second click. */
    busy?: boolean
    disabled?: boolean
    /**
     * True when the act will ask for the password (ADR-033).
     *
     * Said BEFORE the click: someone without the password must know what is open to them
     * without going to fetch a manager. The badge is orthogonal to the family — a neutral
     * act can be protected too.
     */
    protected?: boolean
    /**
     * The name of the act, for the tests.
     *
     * The label turns into « En cours… » while the work runs, so it cannot find a button
     * at the very moment one wants to question it. This name does not move.
     */
    act?: string
    onrun: () => void
  }

  const {
    kind = 'read',
    label,
    busy = false,
    disabled = false,
    protected: guarded = false,
    act = undefined,
    onrun,
  }: Props = $props()
</script>

<button
  type="button"
  class="act {kind}"
  class:touch-target={kind === 'destructive'}
  class:busy
  data-kind={kind}
  data-act={act}
  disabled={disabled || busy}
  onclick={onrun}
>
  {busy ? 'En cours…' : label}
  {#if guarded}<span class="key" title="Demande le mot de passe">clé</span>{/if}
</button>

<style>
  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    min-height: 2.75rem;
    padding: 0 1rem;
    font-size: 1.0625rem;
    font-weight: 700;
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
    transition:
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease),
      box-shadow var(--slide) var(--ease);
  }

  /* Reading or testing changes nothing on the station: the button stays quiet. */
  .read {
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
  }

  .write {
    color: var(--surface);
    background: var(--action);
    border: 1px solid var(--action);
  }

  .destructive {
    /* The 72 px of §14.2 are REPEATED here rather than left to `.touch-target`: that
       class weighs one class, whereas the `min-height` of `.act` weighs two once Svelte
       scoping is applied to it. The touch target would have shrunk silently. */
    min-height: var(--touch-min);
    color: var(--surface);
    background: var(--danger);
    border: 1px solid var(--danger);
  }

  @media (hover: hover) {
    .read:hover:not(:disabled) {
      border-color: var(--ink-muted);
      box-shadow: var(--shadow-2);
    }

    /* The rim keeps the hue of its family: `button:hover` in the global stylesheet lays
       down a grey which, over a solid fill, reads as the edge of a different button. */
    .write:hover:not(:disabled) {
      border-color: var(--action);
    }

    .destructive:hover:not(:disabled) {
      border-color: var(--danger);
    }

    /* A solid fill DARKENS on hover instead of lightening: lightening #17518f by 12 %
       brings it back to 6.9:1 under white ink, below the 7:1 for which these two hues
       were chosen in the first place. Darkening works with the contrast, not against. */
    .write:hover:not(:disabled),
    .destructive:hover:not(:disabled) {
      box-shadow: var(--shadow-2);
      filter: brightness(0.92);
    }
  }

  .act:disabled {
    opacity: 0.5;
    box-shadow: none;
    cursor: default;
  }

  /* The button that is working stays FULLY legible: it is the one being watched. */
  .act.busy:disabled {
    opacity: 1;
  }

  /* A key, not a red padlock: the act IS possible, it only asks who you are. The word is
     spelled out — an icon alone teaches nothing to someone who does not already know
     it. */
  .key {
    padding: 0.0625rem 0.375rem;
    border-radius: var(--radius-pill);
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    background: var(--bg);
    color: var(--ink-muted);
  }

  /* Over a solid fill the badge inverts rather than sinking into it. It keeps the ink:
     a colour does not carry letters, that is the rule of §14.2. */
  .write .key,
  .destructive .key {
    background: var(--surface);
    color: var(--ink);
  }
</style>
