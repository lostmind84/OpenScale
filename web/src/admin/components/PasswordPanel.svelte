<script lang="ts">
  import Icon from '../../components/Icon.svelte'
  import type { Admin } from '../lib/session.svelte'
  import Act from './Act.svelte'

  /**
   * Le mot de passe, demandé à l'ACTE et non à la porte (ADR-033).
   *
   * Ce n'est pas une page : on ne vient pas ici, on y est amené par un geste qu'on vient
   * de faire, et qui sera REJOUÉ dès que ce panneau aura répondu. C'est ce qui autorise à
   * ne demander le mot de passe qu'une fois par demi-heure, au moment où il sert.
   *
   * Deux formes, et le service dit laquelle : une session à rouvrir (401), ou un poste
   * qui n'a jamais eu de mot de passe (409). Le second cas n'a rien à taper — il a une
   * fiche d'installation, et le code de secours qui y est imprimé.
   */
  interface Props {
    admin: Admin
  }

  const { admin }: Props = $props()

  const first = $derived(admin.pending?.kind === 'first-password')

  let password = $state('')
  let code = $state('')

  /** Assez court pour ne pas ralentir, assez long pour ne pas être deviné (§14.4). */
  const MIN_PASSWORD = 8

  const ready = $derived(
    first ? code.trim().length > 0 && password.length >= MIN_PASSWORD : password.length > 0,
  )

  /** Répond au panneau, par l'un ou l'autre des deux chemins. */
  async function answer(): Promise<void> {
    if (!ready || admin.busy) return
    if (first) await admin.answerRecovery(code, password)
    else await admin.answerPassword(password)
    password = ''
    code = ''
  }
</script>

<div class="scrim" role="presentation">
  <div class="panel" role="dialog" aria-modal="true" aria-labelledby="password-title">
    <header>
      <span class="glyph" aria-hidden="true"><Icon name="settings" size="1.5rem" /></span>
      <h2 id="password-title">
        {first ? 'Ce poste n’a pas encore de mot de passe' : 'Mot de passe d’administration'}
      </h2>
    </header>

    <p class="why">{admin.pending?.message ?? ''}</p>

    {#if first}
      <p class="how">
        Le <strong>code de secours</strong> a été tiré à l’installation de ce poste et imprimé
        sur sa fiche, rangée dans le classeur du magasin. Il n’existe nulle part ailleurs.
      </p>
      <label class="field">
        <span>Code de secours</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input
          type="text"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="characters"
          bind:value={code}
          onkeydown={(e) => e.key === 'Enter' && void answer()}
          autofocus
        />
      </label>
      <label class="field">
        <span>Nouveau mot de passe — {MIN_PASSWORD} caractères au moins</span>
        <input
          type="password"
          autocomplete="new-password"
          bind:value={password}
          onkeydown={(e) => e.key === 'Enter' && void answer()}
        />
      </label>
    {:else}
      <label class="field">
        <span>Mot de passe</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input
          type="password"
          autocomplete="current-password"
          bind:value={password}
          onkeydown={(e) => e.key === 'Enter' && void answer()}
          autofocus
        />
      </label>
      <p class="how">
        Oublié ? Le <strong>code de secours</strong> de la fiche d’installation en repose un.
        Un responsable peut aussi le faire en ligne de commande, avec
        <code>openscale config password</code>.
      </p>
    {/if}

    {#if admin.actionError !== ''}
      <p class="refusal" data-panel-error>{admin.actionError}</p>
    {/if}

    <footer>
      <Act label="Annuler" onrun={() => admin.cancelPassword()} />
      <!-- Blue: answering here OPENS the session and replays the act that brought this
           panel up — and, the first time, SETS the station's password. -->
      <Act
        kind="write"
        label={first ? 'Poser ce mot de passe' : 'Continuer'}
        disabled={!ready || admin.busy}
        onrun={() => void answer()}
      />
    </footer>
  </div>
</div>

<style>
  /* Il couvre l'écran : ce qui est derrière est ce qu'on vient de demander à écrire. */
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 95;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem;
    background: rgb(28 27 25 / 45%);
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    width: min(34rem, 100%);
    padding: 1.75rem;
    background: var(--surface);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-2);
  }

  header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .glyph {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.5rem;
    height: 2.5rem;
    flex: none;
    border-radius: var(--radius-sm);
    background: var(--bg);
    color: var(--ink-muted);
  }

  h2 {
    margin: 0;
    font-size: 1.375rem;
    line-height: 1.2;
  }

  .why,
  .how {
    margin: 0;
    color: var(--ink-muted);
    font-size: 1rem;
    line-height: 1.4;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  input {
    height: 2.75rem;
    padding: 0 0.75rem;
    font: inherit;
    font-size: 1.125rem;
    color: var(--ink);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  input:focus-visible {
    outline: 3px solid var(--focus);
    outline-offset: 1px;
  }

  /* Le liseré porte le refus, jamais les lettres : --fault plafonne à 6,54:1 (§14.2). */
  .refusal {
    margin: 0;
    padding: 0.625rem 0.75rem;
    border-left: 0.25rem solid var(--fault);
    background: var(--fault-wash);
    font-size: 1rem;
  }

  footer {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 0.25rem;
  }
</style>
