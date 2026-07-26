<script lang="ts">
  /**
   * Un des gros boutons de la page Dépannage (§14.4).
   *
   * Gros, espacés, tactiles : c'est un bénévole DEBOUT devant un poste en panne, pas un
   * administrateur assis. La hauteur est de 6rem, soit 96 px sur l'écran de référence —
   * au-delà de la cible tactile de 72 px de §14.2, parce que ces boutons se touchent
   * sans lunettes et parfois avec un sac dans l'autre main.
   */
  interface Props {
    label: string
    /** Ce que le bouton fait, en une phrase. Il n'y a pas de deuxième écran pour l'expliquer. */
    hint?: string
    disabled?: boolean
    /** Un bouton qui change un état du poste plutôt que de lire quelque chose. */
    engaged?: boolean
    onrun: () => void
  }

  const { label, hint = '', disabled = false, engaged = false, onrun }: Props = $props()
</script>

<button type="button" class="big touch-target" class:engaged {disabled} onclick={onrun}>
  <span class="label">{label}</span>
  {#if hint !== ''}<span class="hint">{hint}</span>{/if}
</button>

<style>
  .big {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 0.25rem;
    min-height: 6rem;
    padding: 1rem 1.25rem;
    text-align: left;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .big:disabled {
    opacity: 0.5;
  }

  /* Un état ENGAGÉ se voit au liseré : la saisie manuelle et l'imprimante de secours sont
     des états dans lesquels le poste RESTE, et un bouton qui n'en dirait rien laisserait
     un poste en mode dégradé jusqu'au lendemain (§11.4). */
  .big.engaged {
    border-left: 0.5rem solid var(--warning);
  }

  .label {
    font-size: 1.375rem;
    font-weight: 700;
  }

  .hint {
    font-size: 1rem;
    color: var(--ink-muted);
  }
</style>
