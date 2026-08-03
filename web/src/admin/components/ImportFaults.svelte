<script lang="ts">
  import type { FaultDTO } from '../lib/dto'
  import { labelOf } from '../lib/fields'
  import { preferences } from '../lib/preferences.svelte'
  import { tally } from '../lib/tally'

  /**
   * Ce que les contrôles de §11.3 disent d'un fichier qu'on vient d'importer.
   *
   * Un fichier qui serait refusé n'est pas une panne du poste : c'est quelque chose à
   * corriger dans le brouillon avant d'enregistrer, d'où le lavis d'avertissement et non
   * celui de la panne. Recopier reste possible, et la phrase du bas le dit.
   */
  interface Props {
    /** Tout ce que les contrôles ont refusé, plafond compris : il s'applique ici. */
    faults: FaultDTO[]
  }

  const { faults }: Props = $props()

  /** How many refused controls are drawn, out of the 45 of §11.3. */
  const FAULTS_SHOWN = 20

  const shown = $derived(faults.slice(0, FAULTS_SHOWN))
  const total = $derived(
    tally(shown.length, faults.length, 'contrôle refuse une clé', 'contrôles refusent une clé'),
  )
</script>

<div class="faults" data-faults>
  <p class="fact">Ce fichier serait refusé en l’état : {total}</p>
  <!--
    Deliberately UNKEYED: one field carries as many controls as it breaks, so a
    `each` keyed by `field` would throw `each_key_duplicate` on the first file that
    breaks two rules on the same key, and take the whole screen with it.
  -->
  <ul>
    {#each shown as fault}
      <li>
        <!--
          The French name FIRST, as the save bar of `App.svelte` renders the very
          same refusals: the service answers a key plus a message, and « 99999 hors
          bornes [1, 50000] » names nothing on its own once the key is hidden. The
          two lists show the same object and must read the same way — one of them
          spelling the key out would put back on screen what the switch hides.
        -->
        <strong>{labelOf(fault.field)}</strong>
        {#if preferences.showTechnicalNames}<code>{fault.field}</code>{/if}
        {fault.message}
        <!--
          Half the control was being thrown away: `allowed` carries the values that
          WOULD work, and §11.4 step 1 asks for them in so many words. « Ce port
          n'existe pas sur ce poste » without the list of the ports that do exist
          is a refusal nobody can act on — least of all here, where the file is not
          correctable field by field.
        -->
        {#if fault.allowed !== undefined && fault.allowed.length > 0}
          <span class="allowed">Valeurs acceptées : {fault.allowed.join(', ')}.</span>
        {/if}
      </li>
    {/each}
  </ul>
  <p class="fact muted">
    Recopier reste possible : les valeurs entrent dans le brouillon, où elles se
    corrigent champ par champ avant l’enregistrement.
  </p>
</div>

<style>
  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  /* A file that would be refused is not a failure of the station: it is something to fix
     in the draft before saving, hence the warning wash and not the fault one. */
  .faults {
    margin-top: 0.75rem;
    padding: 0.25rem 1rem 0.5rem;
    border-left: 0.375rem solid var(--warning);
    border-radius: var(--radius);
    background: var(--warning-wash);
  }

  .faults ul {
    margin: 0;
    padding-left: 1.25rem;
    font-size: 1.0625rem;
  }

  /* The values that WOULD work, on their own line: they are what somebody types next. */
  .allowed {
    display: block;
    color: var(--ink-muted);
    font-size: 1rem;
  }
</style>
