<script lang="ts">
  import { onMount } from 'svelte'
  import { Draft } from './lib/draft.svelte'
  import { Admin, needsPassword, type PageID } from './lib/session.svelte'
  import Catalog from './pages/Catalog.svelte'
  import Dashboard from './pages/Dashboard.svelte'
  import Hardware from './pages/Hardware.svelte'
  import Journal from './pages/Journal.svelte'
  import Label from './pages/Label.svelte'
  import Login from './pages/Login.svelte'
  import Rules from './pages/Rules.svelte'
  import Station from './pages/Station.svelte'
  import Troubleshooting from './pages/Troubleshooting.svelte'

  /**
   * L'écran d'administration, et ses DEUX NIVEAUX explicites (§14.4).
   *
   * 99 % des utilisateurs ne sont pas à l'aise avec l'informatique : mettre neuf pages
   * d'expert devant eux est une faute. Ce qui s'ouvre est donc le tableau de bord, et la
   * barre ne montre d'abord que deux onglets. « Réglages avancés » est une porte, et c'est
   * la seule qui demande un mot de passe.
   *
   * L'écran est monté dans la MÊME fenêtre que l'écran client (§14.1) : il partage son
   * `window` et son filet d'erreurs. Le bouton « Fermer » le retire et rend la grille — il
   * n'y a pas d'autre sortie sur un poste en kiosque, où `Alt+F4` n'existe pas.
   */
  interface Props {
    /** Ce qu'il faut faire quand le bénévole ferme l'écran. Absent sur la page /admin. */
    onclose?: () => void
  }

  const { onclose }: Props = $props()

  const admin = new Admin()
  const draft = new Draft(admin)

  /** Les onglets bénévoles, et ceux qui apparaissent quand la session est ouverte. */
  const VOLUNTEER_TABS: { id: PageID; label: string }[] = [
    { id: 'dashboard', label: 'Tableau de bord' },
    { id: 'troubleshooting', label: 'Dépannage' },
  ]

  const EXPERT_TABS: { id: PageID; label: string }[] = [
    { id: 'hardware', label: 'Matériel' },
    { id: 'label', label: 'Étiquette' },
    { id: 'rules', label: 'Règles' },
    { id: 'catalog', label: 'Catalogue' },
    { id: 'journal', label: 'Journal' },
    { id: 'station', label: 'Poste' },
  ]

  /** La page réellement affichée : une page experte sans session est la porte. */
  const locked = $derived(needsPassword(admin.page) && !admin.expert)

  /**
   * Comment le poste se nomme en tête d'écran.
   *
   * Le nom du poste PORTE DÉJÀ son numéro dans les fichiers réels — « Poste 2 — fruits » —
   * donc le préfixer à nouveau donnait « Poste 2 — Poste 2 — fruits ». Le numéro seul reste
   * affiché dans l'encadré « Ce poste », où il est ce qu'un bénévole lit au téléphone.
   */
  const title = $derived(
    admin.health === null || admin.health.station_name === ''
      ? `Poste ${String(admin.health?.station ?? '')}`
      : admin.health.station_name,
  )

  onMount(() => {
    admin.start()
    return () => admin.stop()
  })

  /**
   * Ouvre une page, et charge la configuration la première fois qu'une page experte
   * l'affiche.
   *
   * Le document n'est PAS lu au montage : le tableau de bord n'en a pas besoin, et
   * `GET /admin/api/config` demanderait un mot de passe à un bénévole qui n'en a pas.
   */
  async function open(page: PageID): Promise<void> {
    admin.open(page)
    if (needsPassword(page) && admin.expert && draft.config === null) await draft.load()
  }

  /** Enregistre le brouillon : les cinq étapes de §11.4 et leurs refus. */
  async function save(): Promise<void> {
    if (await draft.save()) admin.notice = 'La configuration est enregistrée et appliquée.'
  }
</script>

<div class="admin" data-admin>
  <header>
    <div class="who">
      <h1>Administration</h1>
      {#if admin.health !== null}
        <p class="station">{title} · {admin.health.coop}</p>
        <p class="build">
          version {admin.health.version} · configuration {admin.health.config_fingerprint}
        </p>
      {/if}
    </div>

    <nav aria-label="Pages de l’administration">
      {#each VOLUNTEER_TABS as tab (tab.id)}
        <button
          type="button"
          class="tab touch-target"
          class:current={admin.page === tab.id}
          onclick={() => void open(tab.id)}
        >
          {tab.label}
        </button>
      {/each}

      {#if !admin.expert}
        <button
          type="button"
          class="tab touch-target"
          class:current={locked}
          onclick={() => void open('hardware')}
        >
          Réglages avancés
        </button>
      {:else}
        {#each EXPERT_TABS as tab (tab.id)}
          <button
            type="button"
            class="tab touch-target"
            class:current={admin.page === tab.id}
            onclick={() => void open(tab.id)}
          >
            {tab.label}
          </button>
        {/each}
        <button type="button" class="tab touch-target" onclick={() => void admin.logout()}>
          Fermer la session
        </button>
      {/if}

      {#if onclose !== undefined}
        <button type="button" class="tab close touch-target" onclick={onclose}>
          Revenir à l’écran client
        </button>
      {/if}
    </nav>
  </header>

  {#if admin.notice !== ''}
    <p class="notice" data-notice>{admin.notice}</p>
  {/if}
  {#if admin.error !== ''}
    <p class="failure" data-failure>{admin.error}</p>
  {/if}

  {#if draft.pending !== null}
    <div class="pending" data-pending>
      <p>
        Configuration appliquée mais NON CONFIRMÉE : {draft.pending.changed_blocks.join(', ')}. Le
        poste reviendra tout seul à la version précédente dans
        {draft.pending.seconds_left} secondes si personne ne confirme.
      </p>
      <button
        type="button"
        class="tab touch-target"
        onclick={() => void draft.confirm()}
      >
        Tout fonctionne : confirmer
      </button>
    </div>
  {/if}

  <main class="body">
    {#if admin.health === null}
      <p class="waiting">Lecture de l’état du poste…</p>
    {:else if locked}
      <Login {admin} onopened={() => void open(admin.page)} />
    {:else if admin.page === 'dashboard'}
      <Dashboard health={admin.health} onshowrows={() => void open('catalog')} />
    {:else if admin.page === 'troubleshooting'}
      <Troubleshooting {admin} health={admin.health} />
    {:else if admin.page === 'hardware'}
      <Hardware {admin} {draft} health={admin.health} />
    {:else if admin.page === 'label'}
      <Label {admin} {draft} />
    {:else if admin.page === 'rules'}
      <Rules {draft} health={admin.health} />
    {:else if admin.page === 'catalog'}
      <Catalog {admin} health={admin.health} />
    {:else if admin.page === 'journal'}
      <Journal {admin} />
    {:else if admin.page === 'station'}
      <Station {admin} {draft} health={admin.health} />
    {/if}
  </main>

  {#if admin.expert && !locked && draft.config !== null}
    <footer>
      {#if draft.retired.length > 0}
        <p class="retired">
          Ce fichier porte des clés que ce binaire refuse : {draft.retired.join(', ')}.
          {#each draft.retired as key (key)}
            <button type="button" class="tab touch-target" onclick={() => draft.dropRetired(key)}>
              retirer {key}
            </button>
          {/each}
        </p>
      {/if}
      {#if draft.faults.length > 0}
        <ul class="faults" data-faults>
          {#each draft.faults as fault (fault.field)}
            <li>
              <code>{fault.field}</code>
              {fault.message}
              {#if fault.allowed !== undefined && fault.allowed.length > 0}
                — valeurs acceptées : {fault.allowed.join(', ')}
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      <button
        type="button"
        class="save touch-target"
        disabled={!draft.dirty || admin.busy}
        onclick={() => void save()}
      >
        {draft.dirty ? 'Enregistrer la configuration' : 'Aucune modification à enregistrer'}
      </button>
    </footer>
  {/if}
</div>

<style>
  /*
   * L'écran couvre la fenêtre : il est monté dans le document du client, au-dessus de la
   * grille, et un panneau qui laisserait voir la grille derrière lui inviterait à toucher
   * une tuile pendant un réglage.
   */
  .admin {
    position: fixed;
    inset: 0;
    z-index: 90;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    color: var(--ink);
    overflow: hidden;
  }

  header {
    flex: none;
    display: flex;
    flex-wrap: wrap;
    gap: 1rem;
    align-items: flex-start;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  h1 {
    margin: 0;
    font-size: 1.75rem;
  }

  .station,
  .build {
    margin: 0.125rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  nav {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
  }

  .tab {
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  /* L'onglet courant se signale par un liseré et non par une encre colorée (§14.2). */
  .tab.current {
    border-bottom: 0.25rem solid var(--focus);
  }

  .close {
    margin-left: 1rem;
  }

  .body {
    flex: 1;
    overflow-y: auto;
    padding: 1rem;
  }

  .waiting {
    margin: 0;
    font-size: 1.25rem;
    color: var(--ink-muted);
  }

  .notice,
  .failure,
  .pending {
    flex: none;
    margin: 0;
    padding: 0.75rem 1rem;
    font-size: 1.125rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  .notice {
    border-left: 0.5rem solid var(--ready);
  }

  .failure {
    border-left: 0.5rem solid var(--fault);
  }

  .pending {
    display: flex;
    flex-wrap: wrap;
    gap: 1rem;
    align-items: center;
    border-left: 0.5rem solid var(--warning);
  }

  .pending p {
    margin: 0;
    flex: 1 1 24rem;
  }

  footer {
    flex: none;
    padding: 0.75rem 1rem;
    background: var(--surface);
    border-top: 1px solid var(--border);
  }

  .retired {
    margin: 0 0 0.5rem;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .faults {
    margin: 0 0 0.5rem;
    padding-left: 1.25rem;
    font-size: 1.0625rem;
  }

  .save {
    padding: 0 1.5rem;
    min-height: 3.5rem;
    font-size: 1.25rem;
    font-weight: 700;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .save:disabled {
    opacity: 0.5;
  }
</style>
