<script lang="ts">
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
        {query}
      {/if}
    </p>
    <p class="matches">{matches} produit{matches > 1 ? 's' : ''}</p>
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
        <span aria-hidden="true">⌫</span>
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
    max-height: 33.333%;
    display: flex;
    flex-direction: column;
    gap: var(--touch-gap);
    padding: var(--touch-gap);
    background: var(--surface);
    border-top: 2px solid var(--border);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .typed {
    flex: 1 1 auto;
    margin: 0;
    font-size: 1.75rem;
    font-weight: 700;
    letter-spacing: 0.05em;
  }

  .hint {
    color: var(--ink-muted);
    font-weight: 400;
  }

  .matches {
    margin: 0;
    color: var(--ink-muted);
    font-size: 1.5rem;
  }

  .keys {
    display: flex;
    flex-direction: column;
    gap: var(--touch-gap);
  }

  .row {
    display: flex;
    justify-content: center;
    gap: var(--touch-gap);
  }

  .key {
    flex: 1 1 auto;
    max-width: 7rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
    font-size: 1.75rem;
    font-weight: 700;
  }

  .key.space {
    max-width: 20rem;
  }

  .key.wide,
  .key.close {
    max-width: 10rem;
    font-weight: 400;
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
