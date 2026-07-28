<script lang="ts">
  import Icon from './Icon.svelte'

  /**
   * Le champ de recherche du bandeau : rien ne s'affiche tant que rien n'est
   * tapé — la première frappe le fait apparaître (App.svelte l'écoute) —, et
   * il porte le focus dès qu'il existe pour que la frappe continue au
   * clavier physique du poste, jamais tactile (le poste n'a pas d'écran
   * tactile à ce jour).
   */
  interface Props {
    query: string
    onquery: (q: string) => void
    onclose: () => void
    onenter: () => void
  }

  const { query, onquery, onclose, onenter }: Props = $props()

  let inputEl = $state<HTMLInputElement | null>(null)

  $effect(() => {
    inputEl?.focus()
  })
</script>

<div class="search-field">
  <Icon name="search" size="1.75rem" />
  <input
    type="text"
    bind:this={inputEl}
    value={query}
    oninput={(event) => onquery(event.currentTarget.value)}
    onkeydown={(event) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onclose()
      } else if (event.key === 'Enter') {
        event.preventDefault()
        onenter()
      }
    }}
    placeholder="Tapez le nom du produit"
  />
  {#if query !== ''}
    <button type="button" class="touch-target" onclick={onclose}>Effacer</button>
  {/if}
  <!-- Fermer et Effacer appellent le MÊME gestionnaire : dans ce mode,
       fermer la recherche EST l'effacer (les deux boutons sont équivalents,
       reproduit tel quel depuis la maquette). -->
  <button type="button" class="touch-target close" aria-label="Fermer la recherche" onclick={onclose}>
    <Icon name="close" size="1.5rem" />
  </button>
</div>

<style>
  .search-field {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.5rem 1.0625rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  input {
    flex: 1 1 auto;
    min-width: 0;
    height: var(--touch-min);
    padding: 0 1.25rem;
    background: var(--bg);
    border: 2px solid var(--border);
    border-radius: var(--radius);
    font: inherit;
    font-size: 1.75rem;
    font-variant-numeric: tabular-nums;
    color: var(--ink);
  }

  button {
    flex: 0 0 auto;
    padding: 0 1.5rem;
    background: var(--bg);
    border: 2px solid var(--border);
    border-radius: var(--radius);
    font-size: 1.375rem;
  }

  button.close {
    display: flex;
    align-items: center;
    justify-content: center;
    width: var(--touch-min);
    padding: 0;
  }
</style>
