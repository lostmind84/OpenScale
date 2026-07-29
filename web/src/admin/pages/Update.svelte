<script lang="ts" module>
  import type { UpdateStatus } from '../lib/dto'

  /**
   * Ce que chaque issue d'une bascule veut dire, en une phrase.
   *
   * Quatre phrases et non deux : « annulée, le poste fonctionne » n'appelle personne,
   * « le poste ne répond pas » demande quelqu'un tout de suite, et « rien n'a été
   * remplacé » veut dire qu'on peut recommencer sans risque. Les confondre ferait lire
   * au bénévole la moins utile des quatre.
   */
  const OUTCOMES: Record<UpdateStatus, string> = {
    succeeded: 'La dernière mise à jour a réussi.',
    'rolled-back':
      'La dernière mise à jour a échoué. La version précédente a été remise et le poste fonctionne.',
    'rolled-back-unhealthy':
      'La dernière mise à jour a échoué et le poste n’a pas redémarré. Appelez le support.',
    'not-started':
      'La dernière mise à jour n’a pas démarré : rien n’a été remplacé. Vous pouvez réessayer.',
  }

  /** À quelle cadence l'écran demande si le poste est revenu, en millisecondes. */
  const PROBE_PERIOD_MS = 2000

  /** Combien de temps il attend avant de renoncer, en millisecondes. */
  const PROBE_BUDGET_MS = 5 * 60 * 1000
</script>

<script lang="ts">
  import Act from '../components/Act.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { UpdateDTO } from '../lib/dto'
  import { frenchDateTime } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Mise à jour : installer la version publiée, depuis l'écran.
   *
   * Le geste que cette page remplace demandait une console en administrateur, et aucun
   * bénévole ne l'aurait fait. Ce qu'elle montre est donc réduit à ce qui sert : la
   * version qui tourne, celle qui existe, le dépôt suivi, et ce qui s'est passé la
   * dernière fois.
   *
   * **Les notes de version ne sont pas rendues.** Le corps d'une publication est du
   * Markdown venu du réseau : le rendre demanderait une bibliothèque de plus et ouvrirait
   * une injection dans l'écran d'administration, pour un gain nul. Un lien suffit.
   */
  interface Props {
    admin: Admin
  }

  const { admin }: Props = $props()

  /**
   * Ce que le service dit, ou null tant qu'on n'a pas lu.
   *
   * Le nom N'EST PAS `state` : une variable de ce nom rendrait `$state` ambigu — Svelte
   * lirait la rune comme l'abonnement au store `state` —, et le fichier ne compilerait
   * plus. `svelte-check` le dit, `vitest` ne le voit pas.
   */
  let current = $state<UpdateDTO | null>(null)

  /**
   * Ce qui a raté à la LECTURE, distinct de ce qui rate à un ACTE.
   *
   * Un seul champ pour les deux, c'est le défaut qui a fait échouer neuf boutons en
   * silence sur cet écran : le sondage remettait le message à vide plus vite qu'un
   * humain ne pouvait le lire.
   */
  let readFailure = $state('')

  /** L'acte en vol, pour désarmer le bouton qui travaille. */
  let working = $state('')

  /** Vrai pendant la bascule : le poste est en train de mourir et de revenir. */
  let switching = $state(false)

  /** Ce que l'attente du retour a donné, une fois qu'elle est finie. */
  let switchReport = $state('')

  const outcome = $derived(current?.outcome ?? null)
  const canInstall = $derived(current?.supported === true && current.available)
  /**
   * La version que le bouton installera.
   *
   * Extraite ici et non lue dans le gabarit : le rappel du bouton s'exécute plus tard,
   * et TypeScript ne peut pas savoir que `current` sera encore non nul à ce moment-là.
   * Une chaîne capturée dit exactement ce qui a été affiché — ce que le service exige.
   */
  const latest = $derived(current?.latest ?? '')

  $effect(() => {
    void read()
  })

  /**
   * Relit l'état.
   *
   * Un échec de lecture n'efface pas ce qu'on savait : un poste dont le service vient de
   * redémarrer doit continuer d'afficher ce qu'il affichait, avec la raison à côté.
   */
  async function read(): Promise<void> {
    try {
      current = await api.fetchUpdate()
      readFailure = ''
    } catch (failure) {
      readFailure = failure instanceof Error ? failure.message : 'Lecture impossible.'
    }
  }

  /** « Vérifier maintenant » : sonde le dépôt sans attendre le tour quotidien. */
  async function check(): Promise<void> {
    working = 'check'
    try {
      const fresh = await admin.protect(() => api.checkForUpdate())
      if (fresh !== null) {
        current = fresh
        readFailure = ''
      }
    } finally {
      working = ''
    }
  }

  /** Installe la version affichée, puis attend que le poste revienne. */
  async function install(version: string): Promise<void> {
    working = 'apply'
    switchReport = ''
    try {
      const started = await admin.protect(() => api.applyUpdate(version))
      if (started === null) return
      switching = true
      switchReport = (await waitForTheStationToComeBack())
        ? ''
        : 'Le poste n’a pas répondu dans les cinq minutes. Allez le voir.'
      await read()
    } finally {
      working = ''
      switching = false
    }
  }

  /**
   * Sonde le poste jusqu'à ce qu'il réponde de nouveau.
   *
   * UNE ERREUR RÉSEAU EST LE CAS NOMINAL ici, et c'est tout l'inverse du reste de cet
   * écran : le serveur meurt, et c'est le geste demandé qui le tue. La traiter comme un
   * échec afficherait une panne à l'instant précis où tout se passe comme prévu.
   *
   * @returns vrai quand le poste a répondu, faux quand le budget est épuisé.
   */
  async function waitForTheStationToComeBack(): Promise<boolean> {
    const deadline = Date.now() + PROBE_BUDGET_MS
    while (Date.now() < deadline) {
      await new Promise((resume) => setTimeout(resume, PROBE_PERIOD_MS))
      try {
        const answer = await fetch('/healthz', { cache: 'no-store' })
        if (answer.ok) return true
      } catch {
        // Le poste redémarre. C'est ce qu'on attend.
      }
    }
    return false
  }
</script>

<Panel title="Version de ce poste">
  {#if current === null}
    <p class="fact muted">{readFailure === '' ? 'Lecture…' : readFailure}</p>
  {:else}
    {#if readFailure !== ''}
      <p class="fault">{readFailure}</p>
    {/if}
    <dl>
      <dt>Version installée</dt>
      <dd>{current.running}</dd>
      <dt>Dépôt suivi</dt>
      <dd>{current.repository}</dd>
      {#if current.checked_at !== ''}
        <dt>Dernière vérification</dt>
        <dd>{frenchDateTime(current.checked_at)}</dd>
      {/if}
    </dl>

    {#if !current.supported}
      <p class="fact muted">
        La mise à jour depuis cet écran n’existe que sur les postes Windows. Sur les
        autres, elle se fait à la main — voir la notice d’installation.
      </p>
    {:else if current.available}
      <p class="fact">
        Version disponible : <strong>{current.latest}</strong>
        {#if current.published_at !== ''}, publiée le {frenchDateTime(current.published_at)}{/if}.
        {#if current.html_url !== ''}
          <a href={current.html_url} target="_blank" rel="noreferrer noopener">
            Voir les nouveautés
          </a>
        {/if}
      </p>
    {:else}
      <p class="fact muted">Ce poste est à jour.</p>
    {/if}
  {/if}
</Panel>

{#if current !== null && current.supported}
  <Panel title="Installer">
    <div class="row">
      <Act
        kind="read"
        label="Vérifier maintenant"
        protected
        act="check"
        busy={working === 'check'}
        disabled={switching}
        onrun={() => void check()}
      />
      {#if canInstall}
        <Act
          kind="destructive"
          label={`Installer la version ${latest}`}
          protected
          act="apply"
          busy={working === 'apply'}
          onrun={() => void install(latest)}
        />
      {/if}
    </div>

    {#if canInstall}
      <p class="fact muted">
        Le poste va s’arrêter environ une minute. L’écran client s’éteindra puis reviendra
        tout seul. Si la nouvelle version ne démarre pas, la précédente sera remise
        automatiquement — mais les données enregistrées, elles, ne reviendront pas en
        arrière.
      </p>
    {/if}

    {#if switching}
      <p class="fact">Mise à jour en cours. Le poste redémarre, ne le débranchez pas.</p>
    {/if}
    {#if switchReport !== ''}
      <p class="fault">{switchReport}</p>
    {/if}
  </Panel>
{/if}

{#if outcome !== null}
  <Panel title="Dernière tentative">
    <p class="fact" data-outcome={outcome.status}>{OUTCOMES[outcome.status]}</p>
    <dl>
      <dt>Depuis</dt>
      <dd>{outcome.from === '' ? '—' : outcome.from}</dd>
      <dt>Vers</dt>
      <dd>{outcome.to === '' ? '—' : outcome.to}</dd>
      {#if outcome.finished_at !== ''}
        <dt>Terminée le</dt>
        <dd>{frenchDateTime(outcome.finished_at)}</dd>
      {/if}
      {#if outcome.reason !== ''}
        <dt>Raison</dt>
        <dd>{outcome.reason}</dd>
      {/if}
    </dl>
  </Panel>
{/if}

<style>
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
  }
</style>
