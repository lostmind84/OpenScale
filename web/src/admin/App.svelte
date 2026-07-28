<script lang="ts">
  import { onMount } from 'svelte'
  import { Draft } from './lib/draft.svelte'
  import { Admin, needsPassword, type PageID } from './lib/session.svelte'
  import Act from './components/Act.svelte'
  import PasswordPanel from './components/PasswordPanel.svelte'
  import Catalog from './pages/Catalog.svelte'
  import Dashboard from './pages/Dashboard.svelte'
  import Hardware from './pages/Hardware.svelte'
  import Journal from './pages/Journal.svelte'
  import Label from './pages/Label.svelte'
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

  /**
   * Les neuf pages, en DEUX GROUPES et sans porte entre eux (ADR-033).
   *
   * « Réglages avancés » n'est plus un onglet qui demande un mot de passe : les six pages
   * s'ouvrent, on y voit les prix, les ports et les garde-fous, et le mot de passe est
   * demandé au moment où l'on ENREGISTRE. La hiérarchie de §14.4 reste — ce qu'un
   * bénévole touche tous les jours d'un côté, les réglages de l'autre — mais elle est
   * dite par un intertitre plutôt que par une serrure.
   */
  const GROUPS: { title: string; pages: { id: PageID; label: string }[] }[] = [
    {
      title: 'Au quotidien',
      pages: [
        { id: 'dashboard', label: 'Tableau de bord' },
        { id: 'troubleshooting', label: 'Dépannage' },
      ],
    },
    {
      title: 'Réglages',
      pages: [
        { id: 'hardware', label: 'Matériel' },
        { id: 'label', label: 'Étiquette' },
        { id: 'rules', label: 'Règles' },
        { id: 'catalog', label: 'Catalogue' },
        { id: 'journal', label: 'Journal' },
        { id: 'station', label: 'Poste' },
      ],
    },
  ]

  /** Le titre de la page ouverte, pour l'en-tête du corps. */
  const heading = $derived(
    GROUPS.flatMap((group) => group.pages).find((page) => page.id === admin.page)?.label ?? '',
  )

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
   * Ouvre une page, et charge la configuration la première fois qu'une page en a besoin.
   *
   * Le document n'est PAS lu au montage : le tableau de bord n'en a pas besoin, et une
   * lecture inutile au démarrage retarde ce qu'un bénévole est venu voir. Il n'y a plus
   * de mot de passe à demander pour la lire (ADR-033).
   */
  async function open(page: PageID): Promise<void> {
    admin.open(page)
    if (needsPassword(page) && draft.config === null) await draft.load()
  }

  /**
   * Enregistre le brouillon : les cinq étapes de §11.4, leurs refus — et le mot de passe.
   *
   * C'est ici que la protection d'ADR-033 se joue : l'acte part, et s'il revient en 401
   * ou en 409, le panneau demande de quoi s'authentifier PUIS L'ACTE EST REJOUÉ. Personne
   * ne perd sa saisie.
   */
  async function save(): Promise<void> {
    const done = await admin.protect(() => draft.save())
    if (done === true) admin.notice = 'La configuration est enregistrée et appliquée.'
  }
</script>

<div class="admin" data-admin>
  <nav class="rail" aria-label="Pages de l’administration">
    <h1>Administration</h1>

    {#each GROUPS as group (group.title)}
      <p class="group">{group.title}</p>
      {#each group.pages as page (page.id)}
        <button
          type="button"
          class="entry"
          class:current={admin.page === page.id}
          aria-current={admin.page === page.id ? 'page' : undefined}
          onclick={() => void open(page.id)}
        >
          {page.label}
        </button>
      {/each}
    {/each}

    <div class="foot">
      {#if admin.health !== null}
        <p class="station">{title}</p>
        <p class="build">
          {admin.health.coop} · version {admin.health.version}
        </p>
        <p class="build">configuration {admin.health.config_fingerprint}</p>
      {/if}
      {#if onclose !== undefined}
        <button type="button" class="back" onclick={onclose}>← Revenir à l’écran client</button>
      {/if}
    </div>
  </nav>

  <div class="body">
    {#if admin.notice !== ''}
      <p class="banner notice" data-notice>{admin.notice}</p>
    {/if}
    {#if admin.linkError !== ''}
      <p class="banner failure" data-link-failure>{admin.linkError}</p>
    {/if}
    {#if admin.actionError !== ''}
      <p class="banner failure" data-failure>{admin.actionError}</p>
    {/if}

    {#if draft.pending !== null}
      <div class="banner pending" data-pending>
        <p>
          Configuration appliquée mais NON CONFIRMÉE : {draft.pending.changed_blocks.join(', ')}. Le
          poste reviendra tout seul à la version précédente dans
          {draft.pending.seconds_left} secondes si personne ne confirme.
        </p>
        <Act
          kind="write"
          label="Tout fonctionne : confirmer"
          onrun={() => void admin.protect(() => draft.confirm())}
        />
      </div>
    {/if}

    <main class="page">
      <h2 class="page-title">{heading}</h2>
      {#if admin.health === null}
        <p class="waiting">Lecture de l’état du poste…</p>
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

    <!--
      La barre d'enregistrement, ancrée en bas du corps et non de la fenêtre : elle
      n'apparaît que sur les pages qui éditent un document, et elle porte les refus
      champ par champ que §11.3 remonte TOUS d'un coup.
    -->
    {#if needsPassword(admin.page) && draft.config !== null}
      <footer class="save-bar">
        {#if draft.retired.length > 0}
          <p class="retired">
            Ce fichier porte des clés que ce binaire refuse : {draft.retired.join(', ')}.
            {#each draft.retired as key (key)}
              <Act kind="write" label={`retirer ${key}`} onrun={() => draft.dropRetired(key)} />
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
        <Act
          kind="write"
          label={draft.dirty ? 'Enregistrer la configuration' : 'Aucune modification à enregistrer'}
          disabled={!draft.dirty || admin.busy}
          onrun={() => void save()}
        />
      </footer>
    {/if}
  </div>

  {#if admin.pending !== null}
    <PasswordPanel {admin} />
  {/if}
</div>


<style>
  /*
   * L'écran couvre la fenêtre : il est monté dans le document du client, au-dessus de la
   * grille, et un panneau qui laisserait voir la grille derrière lui inviterait à toucher
   * une tuile pendant un réglage.
   *
   * Un RAIL à gauche et un corps à droite (ADR-033). Les neuf pages ne tenaient plus dans
   * une rangée d'onglets une fois la porte des « Réglages avancés » supprimée, et la
   * rangée passait à deux lignes dès qu'une session s'ouvrait — la barre bougeait sous le
   * doigt au moment précis où l'on venait de s'authentifier.
   */
  .admin {
    position: fixed;
    inset: 0;
    z-index: 90;
    display: grid;
    grid-template-columns: 16rem 1fr;
    background: var(--bg);
    color: var(--ink);
    overflow: hidden;
  }

  .rail {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    padding: 1rem 0.75rem;
    overflow-y: auto;
    background: var(--surface);
    border-right: 1px solid var(--border);
  }

  h1 {
    margin: 0 0.5rem 1rem;
    font-size: 1.25rem;
    letter-spacing: -0.01em;
  }

  /* L'intertitre dit la hiérarchie de §14.4 que la serrure disait avant lui. */
  .group {
    margin: 1rem 0.5rem 0.375rem;
    color: var(--ink-muted);
    font-size: 0.8125rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .entry {
    padding: 0 0.75rem;
    height: 2.5rem;
    display: flex;
    align-items: center;
    text-align: left;
    font-size: 1.0625rem;
    border-radius: var(--radius-sm);
    color: var(--ink-muted);
    transition:
      background-color var(--tap) var(--ease),
      color var(--tap) var(--ease);
  }

  @media (hover: hover) {
    .entry:hover {
      background: var(--bg);
      color: var(--ink);
    }
  }

  .entry.current {
    background: var(--bg);
    color: var(--ink);
    font-weight: 700;
    box-shadow: inset 0.1875rem 0 0 0 var(--focus);
  }

  .foot {
    margin-top: auto;
    padding: 1rem 0.5rem 0;
    border-top: 1px solid var(--border);
  }

  .station {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
  }

  .build {
    margin: 0.125rem 0 0;
    font-size: 0.8125rem;
    color: var(--ink-muted);
  }

  .back {
    margin-top: 0.75rem;
    padding: 0 0.5rem;
    height: 2.25rem;
    font-size: 0.9375rem;
    color: var(--ink-muted);
    border-radius: var(--radius-sm);
  }

  @media (hover: hover) {
    .back:hover {
      background: var(--bg);
      color: var(--ink);
    }
  }

  .body {
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow: hidden;
  }

  /*
   * La colonne de lecture. C'était le défaut de mise en page le plus visible : les
   * paragraphes du tableau de bord couraient sur 1 800 px, d'un bord à l'autre d'un écran
   * de 24 pouces. Les tableaux larges — journal, historique d'imports, diff — sortent de
   * cette borne dans leur propre conteneur défilant, jamais en poussant le corps.
   */
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 1.5rem 2rem 3rem;
  }

  .page :global(> *) {
    max-width: 68rem;
  }

  .page-title {
    margin: 0 0 1.25rem;
    font-size: 1.75rem;
    letter-spacing: -0.01em;
  }

  .waiting {
    margin: 0;
    font-size: 1.125rem;
    color: var(--ink-muted);
  }

  .banner {
    flex: none;
    margin: 0;
    padding: 0.75rem 2rem;
    font-size: 1.0625rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  .notice {
    border-left: 0.375rem solid var(--ready);
    background: var(--ready-wash);
  }

  .failure {
    border-left: 0.375rem solid var(--fault);
    background: var(--fault-wash);
  }

  .pending {
    display: flex;
    flex-wrap: wrap;
    gap: 1rem;
    align-items: center;
    border-left: 0.375rem solid var(--warning);
    background: var(--warning-wash);
  }

  .pending p {
    margin: 0;
    flex: 1 1 24rem;
  }

  .save-bar {
    flex: none;
    padding: 0.75rem 2rem;
    background: var(--surface);
    border-top: 1px solid var(--border);
    box-shadow: var(--shadow-1);
  }

  .retired {
    margin: 0 0 0.5rem;
    font-size: 0.9375rem;
    color: var(--ink-muted);
  }

  .faults {
    margin: 0 0 0.5rem;
    padding-left: 1.25rem;
    font-size: 1rem;
  }
</style>
