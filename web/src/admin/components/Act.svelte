<script lang="ts">
  /**
   * Un bouton de l'administration.
   *
   * La couleur dit LA NATURE DE L'ACTE et rien d'autre : neutre quand il interroge le
   * poste, plein bleu quand il l'écrit, plein rouge quand il ne se défait pas d'un
   * clic. C'est la seule information qu'un bénévole peut lire sans légende.
   *
   * Il existe parce que `.act` était redéfinie dans quatre fichiers avec des variantes
   * qui avaient divergé, et parce que chacun des trente-sept boutons recopiait à la
   * main sa pastille et son « En cours… ».
   */
  interface Props {
    /** Ce que l'acte fait au poste : il le lit, il l'écrit, ou il ne se défait pas. */
    kind?: 'read' | 'write' | 'destructive'
    label: string
    /** Vrai pendant que CE bouton travaille : il le dit et refuse un second clic. */
    busy?: boolean
    disabled?: boolean
    /**
     * Vrai quand l'acte demandera le mot de passe (ADR-033).
     *
     * Dit AVANT le clic : quelqu'un qui n'a pas le mot de passe doit savoir ce qui lui
     * est accessible sans aller chercher un responsable. La pastille est orthogonale à
     * la famille — un acte neutre peut être protégé.
     */
    protected?: boolean
    onrun: () => void
  }

  const {
    kind = 'read',
    label,
    busy = false,
    disabled = false,
    protected: guarded = false,
    onrun,
  }: Props = $props()
</script>

<button
  type="button"
  class="act {kind}"
  class:touch-target={kind === 'destructive'}
  class:busy
  data-kind={kind}
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

  /* Lire ou tester ne change rien au poste : le bouton se tait. */
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
    /* Les 72 px de §14.2 sont RÉPÉTÉS ici et non laissés à `.touch-target` : cette
       classe-là pèse une classe, et le `min-height` de `.act` en pèse deux une fois
       porté par la portée de Svelte. La cible aurait rétréci en silence. */
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

    /* Le liseré reste de la teinte de la famille : `button:hover` de la feuille globale
       pose un gris qui, sur un fond plein, se lit comme le bord d'un autre bouton. */
    .write:hover:not(:disabled) {
      border-color: var(--action);
    }

    .destructive:hover:not(:disabled) {
      border-color: var(--danger);
    }

    /* Un fond plein FONCE au survol au lieu de s'éclaircir : éclaircir #17518f de 12 %
       le ramène à 6,9:1 sous l'encre blanche, sous le 7:1 pour lequel ces deux teintes
       ont été choisies. Foncer va dans le sens du contraste. */
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

  /* Le bouton qui travaille reste PLEINEMENT lisible : c'est celui qu'on regarde. */
  .act.busy:disabled {
    opacity: 1;
  }

  /* Une clé, pas un cadenas rouge : l'acte est possible, il demande seulement qui vous
     êtes. Le mot est écrit — une icône seule n'apprend rien à qui ne la connaît pas. */
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

  /* Sur un fond plein, la pastille s'inverse plutôt que de disparaître dedans. Elle
     garde l'encre : une couleur ne porte pas de lettres, c'est la règle de §14.2. */
  .write .key,
  .destructive .key {
    background: var(--surface);
    color: var(--ink);
  }
</style>
