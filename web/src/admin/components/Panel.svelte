<script lang="ts">
  import type { Snippet } from 'svelte'

  /**
   * Une section titrée d'une page d'administration.
   *
   * Elle existe pour une raison de lecture et non de style : chaque page de §14.4 est une
   * liste de sujets, et un bénévole au téléphone doit pouvoir dire « je suis dans
   * Catalogue, encadré Dernier import » sans décrire une position à l'écran.
   */
  interface Props {
    title: string
    /** Ce que la section dit d'elle-même, quand elle a besoin de le dire. */
    note?: string
    children: Snippet
  }

  const { title, note = '', children }: Props = $props()
</script>

<section class="panel">
  <h2>{title}</h2>
  {#if note !== ''}<p class="note">{note}</p>{/if}
  {@render children()}
</section>

<style>
  .panel {
    padding: 1rem 1.25rem 1.25rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  h2 {
    margin: 0 0 0.5rem;
    font-size: 1.5rem;
    font-weight: 700;
  }

  .note {
    margin: 0 0 0.75rem;
    font-size: 1rem;
    color: var(--ink-muted);
  }
</style>
