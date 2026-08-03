<script lang="ts">
  import type { Difference } from '../lib/diff'
  import { labelOf } from '../lib/fields'
  import { preferences } from '../lib/preferences.svelte'

  /**
   * Ce qu'un fichier importé changerait sur ce poste, champ par champ.
   *
   * Bornée et défilant dans son propre cadre : une configuration porte plus de cent trente
   * feuilles, et un poste cloné sur un autre diffère sur la plupart — le tableau poussait
   * « Recopier » un écran entier sous la ligne de flottaison, et le corps de la page ne
   * défile jamais latéralement pour lui.
   */
  interface Props {
    /** Les lignes à dessiner, plafond déjà appliqué : c'est la page qui l'annonce. */
    rows: Difference[]
  }

  const { rows }: Props = $props()
</script>

<div class="scroll">
  <table data-diff>
    <thead>
      <tr>
        <th>Champ</th>
        <th>En service</th>
        <th>Dans le fichier</th>
      </tr>
    </thead>
    <tbody>
      <!--
        The path stays the KEY of the row and its `data-path`, and stops being what
        the row is named by. A dotted key identifies a line to whoever wrote the
        file; it identifies nothing to the volunteer this table was widened for,
        and the switch is what tells the two apart.

        `labelOf` falls back to the path, so a row the index does not name stays
        readable — a clone diff always carries a few of those, `pricing.tiers` and
        `catalog.categories` among them. The `name !== entry.path` guard is what
        keeps that fallback from being written twice in the same cell once the
        switch is on.
      -->
      {#each rows as entry (entry.path)}
        {@const name = labelOf(entry.path)}
        <tr data-path={entry.path}>
          <td>
            {name}
            {#if preferences.showTechnicalNames && name !== entry.path}
              <code>{entry.path}</code>
            {/if}
          </td>
          <td>{entry.before}</td>
          <td>{entry.after}</td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .scroll {
    max-height: 24rem;
    overflow: auto;
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-lg);
    background: var(--bg);
  }

  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.75rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  th {
    position: sticky;
    top: 0;
    color: var(--ink-muted);
    font-size: 1rem;
    background: var(--bg);
  }
</style>
