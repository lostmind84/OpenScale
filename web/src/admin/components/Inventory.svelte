<script lang="ts">
  import type { ImportDTO, MotiveDTO } from '../lib/dto'
  import { inventoryOf } from '../lib/inventory'

  /**
   * L'inventaire du dernier import, écrit mot pour mot comme §14.4 le donne.
   *
   * C'est la phrase qui décide si un bénévole s'inquiète ou non. Les chiffres viennent de
   * l'API, jamais d'un gabarit : ce composant ne fait que les aligner, et
   * {@link inventoryOf} choisit les mots.
   *
   * Les deux formes du document sortent des mêmes chiffres — le bloc aligné et la ligne
   * unique « 153 reçus · 107 pesables · … » — pour qu'elles ne puissent jamais se
   * contredire.
   */
  interface Props {
    record: ImportDTO
    motives: MotiveDTO[]
    /** Ouvre le rapport de correction d'Odoo sur les lignes d'un motif (§10.3 bis). */
    onshowrows?: (what: 'anomalies' | 'units') => void
  }

  const { record, motives, onshowrows }: Props = $props()

  const inventory = $derived(inventoryOf(record, motives))
</script>

<div class="inventory" data-inventory>
  <p class="headline">{inventory.headline}</p>
  <dl>
    {#each inventory.lines as line (line.label)}
      <div class="row" data-row={line.label}>
        <dt>{line.count}</dt>
        <dd>
          <span class="label">{line.label}</span>
          {#if line.note !== ''}<span class="note">{line.note}</span>{/if}
          {#if line.link !== '' && onshowrows !== undefined}
            <button
              type="button"
              class="rows touch-target"
              onclick={() => onshowrows(line.label === 'anomalies' ? 'anomalies' : 'units')}
            >
              {line.link}
            </button>
          {/if}
        </dd>
      </div>
    {/each}
  </dl>
  <p class="oneline" data-oneline>{inventory.oneLine}</p>
</div>

<style>
  .inventory {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .headline {
    margin: 0;
    font-size: 1.375rem;
    font-weight: 700;
  }

  dl {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .row {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
  }

  /* Les chiffres sont alignés à DROITE sur une largeur fixe : « 331 », « 8 » et « 16 »
     doivent se lire en colonne, ce que §14.4 obtient par des espaces et une police à pas
     fixe. `tabular-nums` est déjà global (§14.2). */
  dt {
    flex: none;
    width: 4.5rem;
    text-align: right;
    font-size: 1.375rem;
    font-weight: 700;
  }

  dd {
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem;
    font-size: 1.125rem;
  }

  .label {
    font-weight: 700;
  }

  .note {
    color: var(--ink-muted);
  }

  /* Un lien, pas un gros bouton : il ouvre une liste et ne change rien. La cible tactile
     reste celle de §14.2 par la classe partagée. */
  .rows {
    padding: 0 0.5rem;
    min-height: var(--touch-min);
    text-decoration: underline;
    color: var(--ink-muted);
  }

  .oneline {
    margin: 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }
</style>
