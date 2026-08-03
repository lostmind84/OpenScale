<script lang="ts" module>
  /** À quelle cadence l'écran demande si le poste est revenu, en millisecondes. */
  const PROBE_PERIOD_MS = 2000

  /** Combien de temps il attend avant de renoncer, en millisecondes. */
  const PROBE_BUDGET_MS = 5 * 60 * 1000

  /**
   * Sonde le poste jusqu'à ce qu'il réponde de nouveau.
   *
   * UNE ERREUR RÉSEAU EST LE CAS NOMINAL ici, et c'est l'inverse du reste de l'écran : le
   * serveur meurt, et c'est le geste demandé qui le tue. La traiter comme une panne
   * afficherait une erreur à l'instant précis où tout se passe comme prévu.
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

<script lang="ts">
  import Act from './Act.svelte'
  import Panel from './Panel.svelte'
  import * as api from '../lib/api'
  import { isCredentialRefusal } from '../lib/api'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La rubrique Maintenance de la page Poste : les trois gestes de reprise.
   *
   * Ils sont rangés par brutalité croissante, et c'est la seule chose à lire dans leur
   * ordre : relire le fichier ne coupe rien, redémarrer le poste coupe quelques secondes,
   * redémarrer l'ordinateur coupe une minute et rien ne le défait — d'où le rouge et les
   * trente secondes d'annulation.
   *
   * **Ce n'est pas le bouton qu'ADR-027 a supprimé.** L'ADR refuse un redémarrage EXIGÉ
   * PAR UN RÉGLAGE, et aucun bloc de configuration n'en exige : le rechargement à chaud
   * les couvre tous. Ces trois gestes-là sont des réparations, pour un poste sous kiosque
   * où aucune console n'est atteignable.
   */
  interface Props {
    admin: Admin
  }

  const { admin }: Props = $props()

  /** L'acte en vol, pour désarmer le bouton qui travaille. */
  let working = $state('')

  /** Ce que le dernier acte a répondu, phrase du service comprise. */
  let said = $state('')

  /** Les fautes d'un 422, TOUTES : une seule à la fois est un écran qu'on abandonne. */
  let faults = $state<{ field: string; message: string }[]>([])

  /** Vrai pendant le redémarrage du poste : il meurt et revient. */
  let restarting = $state(false)

  /** L'échéance du redémarrage de l'ordinateur, ou 0 quand rien n'est armé. */
  let rebootAt = $state(0)

  /** Ce qu'il reste à l'échéance, en secondes, redessiné chaque seconde. */
  let secondsLeft = $state(0)

  /** Vrai quand le bouton rouge a été touché une fois et attend sa confirmation. */
  let rebootAsked = $state(false)

  const rebootArmed = $derived(rebootAt > 0)

  $effect(() => {
    if (!rebootArmed) return
    const tick = setInterval(() => {
      secondsLeft = Math.max(0, Math.round((rebootAt - Date.now()) / 1000))
    }, 1000)
    return () => clearInterval(tick)
  })

  /**
   * Relit config.json et met en service ce qui s'y trouve.
   *
   * Le refus d'AUTHENTIFICATION remonte jusqu'à `Admin.protect`, qui demande le mot de
   * passe puis rejoue ; tout le reste — un 422 et ses fautes en tête — s'affiche ici,
   * parce qu'il se règle en corrigeant le fichier et non en s'authentifiant.
   */
  async function reloadConfig(): Promise<void> {
    working = 'reload-config'
    said = ''
    faults = []
    try {
      const served = await admin.protect(async () => {
        try {
          return await api.reloadConfigFromDisk()
        } catch (failure) {
          if (isCredentialRefusal(failure)) throw failure
          admin.report(failure)
          faults = admin.lastFaults
          return null
        }
      })
      if (served === null) return
      said = `Le fichier est en service. Empreinte ${served.config_fingerprint}.`
    } finally {
      working = ''
    }
  }

  /** Demande au poste de redémarrer, puis attend qu'il réponde de nouveau. */
  async function restartStation(): Promise<void> {
    working = 'restart'
    said = ''
    faults = []
    try {
      const started = await admin.protect(() => api.restartStation())
      if (started === null) return
      said = started.message
      restarting = true
      said = (await waitForTheStationToComeBack())
        ? 'Le poste est revenu.'
        : 'Le poste n’a pas répondu dans les cinq minutes. Allez le voir.'
    } finally {
      working = ''
      restarting = false
    }
  }

  /** Arme le redémarrage de l'ordinateur, après une confirmation. */
  async function askForReboot(): Promise<void> {
    if (!rebootAsked) {
      rebootAsked = true
      return
    }
    working = 'reboot'
    said = ''
    faults = []
    try {
      const armed = await admin.protect(() => api.armReboot())
      if (armed === null) return
      rebootAt = Date.parse(armed.at)
      secondsLeft = armed.seconds_left
    } finally {
      working = ''
      rebootAsked = false
    }
  }

  /** Annule le redémarrage armé. */
  async function cancelReboot(): Promise<void> {
    working = 'cancel-reboot'
    try {
      const cancelled = await admin.protect(() => api.cancelReboot())
      if (cancelled === null) return
      rebootAt = 0
      said = cancelled.message
    } finally {
      working = ''
    }
  }
</script>

<Panel title="Maintenance">
  <p class="fact muted">
    Trois gestes de reprise, du plus doux au plus brutal. Le premier ne coupe rien.
  </p>

  <dl>
    <dt>Relire le fichier de configuration</dt>
    <dd>
      Met en service <code>config.json</code> tel qu’il est sur le disque, sans arrêter le
      poste. À utiliser après une modification faite à la main dans le fichier.
      <Act
        kind="write"
        act="reload-config"
        label="Relire le fichier"
        protected
        busy={working === 'reload-config'}
        onrun={reloadConfig}
      />
    </dd>

    <dt>Redémarrer le poste</dt>
    <dd>
      Arrête l’application et la relance. La pesée est interrompue quelques secondes ;
      l’écran client revient tout seul.
      <Act
        kind="write"
        act="restart"
        label="Redémarrer le poste"
        protected
        busy={working === 'restart' || restarting}
        onrun={restartStation}
      />
    </dd>

    <dt>Redémarrer l’ordinateur</dt>
    <dd>
      Redémarre la machine entière. Comptez une minute avant que l’écran revienne.
      {#if rebootArmed}
        <p class="fact" data-countdown>
          L’ordinateur redémarre dans {secondsLeft} seconde{secondsLeft > 1 ? 's' : ''}.
        </p>
        <Act
          kind="write"
          act="cancel-reboot"
          label="Annuler"
          busy={working === 'cancel-reboot'}
          onrun={cancelReboot}
        />
      {:else}
        {#if rebootAsked}
          <p class="fact" data-confirm>
            Touchez de nouveau pour confirmer. Rien ne défait un redémarrage une fois
            l’ordinateur parti.
          </p>
        {/if}
        <Act
          kind="destructive"
          act="reboot"
          label={rebootAsked ? 'Confirmer le redémarrage' : 'Redémarrer l’ordinateur'}
          protected
          busy={working === 'reboot'}
          onrun={askForReboot}
        />
      {/if}
    </dd>
  </dl>

  <!-- Le refus lui-même n'est PAS affiché ici : App.svelte en fait déjà une bannière,
       et deux exemplaires de la même phrase sur un écran se lisent comme deux pannes. Ce
       qui est propre à cette rubrique — les fautes d'un fichier refusé, une par ligne —
       n'existe nulle part ailleurs et s'affiche donc ici. -->
  {#if said !== ''}
    <p class="fact" data-said>{said}</p>
  {/if}
  {#if faults.length > 0}
    <ul class="faults">
      {#each faults as fault (fault.field)}
        <li><code>{fault.field}</code> — {fault.message}</li>
      {/each}
    </ul>
  {/if}
</Panel>

<style>
  dd {
    margin: 0 0 1.25rem;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.5rem;
  }

  /* La même forme que les fautes de la page Poste : une bande d'avertissement, jamais du
     texte rouge — §14.2 inventorie les couleurs de TEXTE et leur contraste, et une
     seconde façon d'afficher le même objet se lirait comme un autre objet. */
  .faults {
    margin-top: 0.75rem;
    padding: 0.5rem 1rem 0.5rem 2rem;
    border-left: 0.375rem solid var(--warning);
    border-radius: var(--radius);
    background: var(--warning-wash);
  }

  .faults li {
    margin-bottom: 0.25rem;
  }
</style>
