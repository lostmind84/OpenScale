<script lang="ts">
  import Icon from './Icon.svelte'

  /**
   * The search: a filter in place, never a view.
   *
   * The panel is anchored to the bottom third; the grid stays VISIBLE above it and
   * shrinks letter after letter. No « OK » key, no validation, no result screen,
   * no cap on the number of results — on 331 entries two or three letters are
   * enough. Closing the panel IS the clearing (§14.3-3).
   *
   * The layout is REDUCED on purpose: the 26 letters, the space and the backspace.
   * No accented keys — the normalization takes care of them — and no symbols: no
   * name of either real file contains a semicolon or a quote, and the ten
   * apostrophes never need to be typed.
   */
  interface Props {
    /** What has been typed so far. */
    query: string
    /** How many products the grid is showing right now, for the live count. */
    matches: number
    onquery: (query: string) => void
    onclose: () => void
  }

  const { query, matches, onquery, onclose }: Props = $props()

  /** The 26 letters, in the three rows of a keyboard, and nothing else. */
  const ROWS = ['AZERTYUIOP', 'QSDFGHJKLM', 'WXCVBN'] as const

  /** Appends a letter. There is nothing to validate: the grid has already moved. */
  function press(letter: string): void {
    onquery(query + letter)
  }

  /** Removes the last character typed. */
  function backspace(): void {
    onquery(query.slice(0, -1))
  }
</script>

<section class="panel" aria-label="Chercher un produit">
  <header class="head">
    <p class="typed" aria-live="polite">
      {#if query.length === 0}
        <span class="hint">Tapez les premières lettres</span>
      {:else}
        {query}<span class="caret" aria-hidden="true"></span>
      {/if}
    </p>
    <p class="matches" class:none={matches === 0}>
      {matches} produit{matches > 1 ? 's' : ''}
    </p>
    <button type="button" class="key touch-target close" onclick={onclose}>Fermer</button>
  </header>

  <div class="keys">
    {#each ROWS as row (row)}
      <div class="row">
        {#each row.split('') as letter (letter)}
          <button type="button" class="key touch-target" onclick={() => press(letter)}>
            {letter}
          </button>
        {/each}
      </div>
    {/each}
    <div class="row">
      <button type="button" class="key touch-target space" onclick={() => press(' ')}>
        Espace
      </button>
      <button type="button" class="key touch-target wide" onclick={backspace}>
        <Icon name="backspace" size="1.75rem" />
        <span class="visually-hidden">Effacer la dernière lettre</span>
      </button>
    </div>
  </div>
</section>

<style>
  .panel {
    /* Anchored to the bottom third: the grid above stays visible and keeps
       filtering. A full-screen overlay hides exactly what it is filtering. */
    flex: 0 0 auto;
    max-height: 40%;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 0.75rem var(--touch-gap) 1rem;
    background: var(--surface);
    border-top: 1px solid var(--border);
    box-shadow: 0 -0.5rem 2rem rgb(28 27 25 / 8%);
    z-index: 2;
    animation: rise var(--slide) var(--ease);
  }

  /* It arrives from where it lives — the bottom edge — and it is over in 170 ms. */
  @keyframes rise {
    from {
      transform: translateY(1.5rem);
      opacity: 0;
    }
  }

  .head {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0 0.5rem;
  }

  .typed {
    display: flex;
    align-items: center;
    flex: 1 1 auto;
    margin: 0;
    font-size: 2rem;
    font-weight: 700;
    letter-spacing: 0.06em;
  }

  .hint {
    color: var(--ink-muted);
    font-weight: 400;
    letter-spacing: 0;
  }

  /* Where the next letter lands. It does not blink — nothing on this screen does. */
  .caret {
    width: 0.1875rem;
    height: 1.75rem;
    margin-left: 0.25rem;
    border-radius: 0.125rem;
    background: var(--ink);
  }

  .matches {
    margin: 0;
    padding: 0.25rem 1rem;
    border-radius: var(--radius-pill);
    background: var(--bg);
    color: var(--ink-muted);
    font-size: 1.375rem;
  }

  .matches.none {
    background: var(--warning-wash);
    color: var(--ink);
  }

  .keys {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--touch-gap);
  }

  .row {
    display: flex;
    justify-content: center;
    gap: var(--touch-gap);
  }

  /*
   * A key is a fixed rectangle, so the three rows line up on one grid instead of
   * stretching to whatever width their letter count gives them.
   */
  .key {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 5rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    box-shadow: var(--shadow-1);
    font-size: 1.75rem;
    font-weight: 600;
  }

  .key:active {
    background: var(--bg);
  }

  .key.space {
    width: 22rem;
  }

  .key.wide {
    width: 8rem;
  }

  .key.close {
    width: auto;
    padding: 0 1.75rem;
    font-size: 1.5rem;
    font-weight: 500;
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
