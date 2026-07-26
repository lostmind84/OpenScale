<script lang="ts">
  import Panel from '../components/Panel.svelte'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La porte des « Réglages avancés » (§14.4).
   *
   * Elle ne s'ouvre que pour ce qui ÉCRIT la configuration : le dépannage et le tableau de
   * bord sont derrière elle, jamais devant (ADR-018). Cinq essais par minute puis cinq
   * minutes de verrouillage, comptés par le service et par adresse.
   *
   * Le formulaire de secours est là parce que le poste est en Assigned Access : il n'y a
   * plus ni bureau ni invite de commande, donc « lancez openscale config password » n'est
   * pas une consigne que quelqu'un puisse suivre. Le code de huit caractères est imprimé
   * sur la fiche d'installation et rangé dans le classeur du magasin (important-10).
   */
  interface Props {
    admin: Admin
    /**
     * Ce qu'il faut faire dès que la session est ouverte.
     *
     * La porte ne charge RIEN elle-même : c'est la coquille qui sait quelle page attendait
     * derrière, et donc quel document lire. Une porte qui aurait lu la configuration aurait
     * décidé à la place de la page qu'elle ouvre.
     */
    onopened?: () => void
  }

  const { admin, onopened }: Props = $props()

  let password = $state('')
  let recovering = $state(false)
  let code = $state('')
  let replacement = $state('')

  /** Ouvre la session, puis vide le champ : rien ne reste en mémoire de l'écran. */
  async function submit(event: Event): Promise<void> {
    event.preventDefault()
    const typed = password
    password = ''
    if (await admin.login(typed)) onopened?.()
  }

  /** Remplace le mot de passe avec le code de la fiche d'installation. */
  async function reset(event: Event): Promise<void> {
    event.preventDefault()
    const typedCode = code
    const typedPassword = replacement
    code = ''
    replacement = ''
    if (await admin.recover(typedCode, typedPassword)) {
      recovering = false
      onopened?.()
    }
  }
</script>

<div class="login">
  <Panel
    title="Réglages avancés"
    note="Ces pages écrivent la configuration du poste. Le dépannage, lui, n’a jamais besoin de mot de passe."
  >
    {#if !recovering}
      <form onsubmit={(event) => void submit(event)}>
        <label for="admin-password">Mot de passe d’administration</label>
        <input
          id="admin-password"
          type="password"
          autocomplete="current-password"
          bind:value={password}
        />
        <button type="submit" class="submit touch-target" disabled={admin.busy}>Ouvrir</button>
      </form>
      <button type="button" class="link touch-target" onclick={() => (recovering = true)}>
        Mot de passe oublié : j’ai le code de secours
      </button>
    {:else}
      <form onsubmit={(event) => void reset(event)}>
        <label for="recovery-code">
          Code de secours — huit caractères, sur la fiche d’installation du poste
        </label>
        <input id="recovery-code" type="text" autocomplete="off" bind:value={code} />
        <label for="recovery-password">Nouveau mot de passe — huit caractères au moins</label>
        <input
          id="recovery-password"
          type="password"
          autocomplete="new-password"
          bind:value={replacement}
        />
        <button type="submit" class="submit touch-target" disabled={admin.busy}>
          Remplacer le mot de passe
        </button>
      </form>
      <button type="button" class="link touch-target" onclick={() => (recovering = false)}>
        Revenir au mot de passe
      </button>
    {/if}
  </Panel>
</div>

<style>
  .login {
    max-width: 40rem;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  label {
    font-size: 1.125rem;
    font-weight: 700;
  }

  input {
    min-height: 3rem;
    padding: 0 0.75rem;
    font: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .submit {
    margin-top: 0.5rem;
    padding: 0 1.5rem;
    font-size: 1.25rem;
    font-weight: 700;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .link {
    margin-top: 0.75rem;
    padding: 0;
    font-size: 1rem;
    color: var(--ink-muted);
    text-decoration: underline;
  }
</style>
