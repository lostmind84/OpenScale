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
    /**
     * Vrai pendant que CE bouton travaille.
     *
     * Trois des neuf actions n'en avaient pas : on appuyait, le port s'ouvrait pendant
     * trois secondes, rien ne bougeait, et on appuyait de nouveau.
     */
    busy?: boolean
    /**
     * Vrai quand l'acte demandera le mot de passe (ADR-033).
     *
     * Dit AVANT le clic, pas après : un bénévole qui n'a pas le mot de passe doit savoir
     * lesquels de ces boutons lui sont accessibles sans aller chercher quelqu'un.
     */
    protected?: boolean
    onrun: () => void
  }

  const {
    label,
    hint = '',
    disabled = false,
    engaged = false,
    busy = false,
    protected: guarded = false,
    onrun,
  }: Props = $props()
</script>

<button
  type="button"
  class="big touch-target"
  class:engaged
  class:busy
  disabled={disabled || busy}
  onclick={onrun}
>
  <span class="label">
    {label}
    {#if guarded}<span class="guarded" title="Demande le mot de passe">clé</span>{/if}
  </span>
  {#if hint !== ''}<span class="hint">{busy ? 'En cours…' : hint}</span>{/if}
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

  /* Le bouton qui travaille reste LISIBLE : c'est celui qu'on regarde. */
  .big.busy {
    opacity: 1;
    border-color: var(--waiting);
    background: var(--waiting-wash);
  }

  /* Une clé, pas un cadenas rouge : l'acte est possible, il demande seulement qui vous
     êtes. Le mot est écrit — une icône seule n'apprend rien à qui ne la connaît pas. */
  .guarded {
    margin-left: 0.5rem;
    padding: 0.0625rem 0.375rem;
    border-radius: var(--radius-pill);
    background: var(--bg);
    color: var(--ink-muted);
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    vertical-align: middle;
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
