<script lang="ts">
  import Act from './Act.svelte'
  import { AdminError } from '../lib/api'
  import * as api from '../lib/api'
  import type { PrinterDiagnosisDTO } from '../lib/dto'
  import { diagnosisReport, remedyLine, STATE_WORDS, type StepState } from '../lib/printer-report'

  /**
   * Le diagnostic pas à pas de l'imprimante, bloc « Imprimante » de la page Matériel
   * (§14.4) — cinq étapes, SANS mot de passe (décision 7), et un bouton « Copier le
   * rapport » pour le forum des coopératives (décision 4).
   *
   * Ce composant ne reçoit ni `admin` ni `draft` : les deux routes qu'il appelle sont
   * libres, et rien ici n'écrit la configuration. `onrun` le fait passer par le même
   * guichet d'un acte à la fois que le reste de la page Matériel — un seul port, un seul
   * geste en vol — sans que ce fichier ait à connaître `ActName`.
   */
  interface Props {
    /** Vrai pendant qu'un AUTRE acte de la page tourne : désarme « Lancer le diagnostic ». */
    disabled: boolean
    /** Vrai pendant que CE diagnostic tourne. */
    busy: boolean
    /** Passe le job au guichet d'un acte à la fois de la page Matériel. */
    onrun: (job: () => Promise<void>) => void
  }

  const { disabled, busy, onrun }: Props = $props()

  /** La borne de §14 Global Constraints : la même que `maxSeenRunes` côté Go. */
  const MAX_SEEN = 200

  let diagnosis = $state<PrinterDiagnosisDTO | null>(null)
  let error = $state('')
  let seen = $state('')
  let answering = $state(false)
  /** Vrai quand `navigator.clipboard` est absent ou a refusé : le repli visible prend le relais. */
  let copyFallback = $state(false)
  let copyNotice = $state('')
  let textareaEl: HTMLTextAreaElement | undefined = $state()

  const steps = $derived(diagnosis?.steps ?? [])
  /** La question n'apparaît QUE si l'étape 5 est encore sans réponse (décision 3). */
  const askStep5 = $derived(steps.length === 5 && steps[4]?.state === 'unanswered')

  /** Sélectionne le texte du repli dès qu'il apparaît : « Ctrl+C » doit avoir quelque chose sous la main. */
  $effect(() => {
    if (copyFallback && textareaEl !== undefined) {
      textareaEl.focus()
      textareaEl.select()
    }
  })

  /**
   * « Lancer le diagnostic » : les étapes 1 à 4, lues dans l'imprimante EN SERVICE.
   *
   * L'ancien parcours disparaît dès le clic, pas au retour : une relance imprime une
   * étiquette, donc dure plusieurs secondes, et une question restée visible pendant ce
   * temps recevrait une réponse que rien ne rattache plus au parcours affiché ensuite
   * (revue finale, T6 m2).
   */
  async function run(): Promise<void> {
    error = ''
    copyFallback = false
    copyNotice = ''
    diagnosis = null
    seen = ''
    try {
      diagnosis = await api.diagnosePrinter()
    } catch (err) {
      error = err instanceof AdminError ? err.message : 'Le diagnostic n’a pas pu être mené.'
    }
  }

  function launch(): void {
    onrun(run)
  }

  /**
   * « Oui » / « Non » : la seule mesure de l'étape 5 est celle du bénévole (décision 3).
   *
   * La réponse n'est greffée que sur le parcours auquel elle répondait : si une relance
   * l'a remplacé entre-temps, l'étape 5 rendue appartient à l'ancien, et la poser sur le
   * nouveau mélangerait deux parcours dans le rapport copié (revue finale, T6 m3).
   */
  async function answer(printed: boolean): Promise<void> {
    const asked = diagnosis
    if (asked === null || answering) return
    answering = true
    try {
      const firstFour = asked.steps.slice(0, 4).map((s) => s.state as StepState)
      const { step } = await api.answerPrinterDiagnosis({ steps: firstFour, printed, seen })
      if (diagnosis !== asked) return
      diagnosis = { target: asked.target, steps: [...asked.steps.slice(0, 4), step] }
    } catch (err) {
      error = err instanceof AdminError ? err.message : 'La réponse n’a pas pu être envoyée.'
    } finally {
      answering = false
    }
  }

  /**
   * « Copier le rapport (pour le forum) ».
   *
   * `navigator.clipboard` n'existe pas hors contexte sécurisé — ce qu'un téléphone qui
   * ouvre cette page en HTTP simple rencontre tout de suite (Review Focus). Le bouton ne
   * doit alors ni rester muet ni se désactiver : le repli est un texte déjà sélectionné,
   * avec la consigne de l'appuyer soi-même.
   */
  async function copyReport(): Promise<void> {
    if (diagnosis === null) return
    const text = diagnosisReport(diagnosis)
    copyNotice = ''
    if (typeof navigator.clipboard?.writeText !== 'function') {
      copyFallback = true
      return
    }
    try {
      await navigator.clipboard.writeText(text)
      copyFallback = false
      copyNotice = 'Rapport copié.'
    } catch {
      copyFallback = true
    }
  }
</script>

<div class="diagnosis" data-diagnosis>
  <h3>Matériel › Imprimante › Diagnostic</h3>

  <div class="actions">
    <Act act="diagnosis" label="Lancer le diagnostic" {busy} {disabled} onrun={launch} />
    <Act
      act="copy-report"
      label="Copier le rapport (pour le forum)"
      disabled={diagnosis === null}
      onrun={() => void copyReport()}
    />
  </div>

  {#if error !== ''}
    <p class="note" data-diagnosis-error>{error}</p>
  {/if}

  {#if copyNotice !== ''}
    <p class="note" data-copy-notice>{copyNotice}</p>
  {/if}

  {#if diagnosis !== null}
    <ol class="steps" data-diagnosis-steps>
      {#each steps as step (step.number)}
        <li class:fail={step.state === 'fail'} data-state={step.state}>
          <p class="step-title">
            {step.number}. {step.title} : {STATE_WORDS[step.state as StepState] ?? step.state}
          </p>
          {#if step.observed !== ''}
            <p class="observed" data-observed>{step.observed}</p>
          {/if}
          <!--
            « À faire » sur une étape en échec SEULEMENT : le remède d'un silence (étape 3,
            inconnu) dit qu'on continue, et le présenter comme une action en ferait une
            panne à corriger (décision 2).
          -->
          {#if step.remedy !== ''}
            <p class="remedy">{remedyLine(step.state, step.remedy)}</p>
          {/if}
          <!-- Un nouvel onglet : revenir ici sinon coûterait une nouvelle étiquette imprimée. -->
          {#if step.doc !== ''}
            <p class="doc">
              <a href={step.doc} target="_blank" rel="noreferrer noopener">Documentation</a>
            </p>
          {/if}
        </li>
      {/each}
    </ol>

    {#if askStep5}
      <div class="ask" data-ask>
        <p class="question">L’étiquette est-elle sortie ?</p>
        <div class="actions">
          <Act
            label="Oui"
            disabled={answering || busy || disabled}
            onrun={() => void answer(true)}
          />
          <Act
            label="Non"
            disabled={answering || busy || disabled}
            onrun={() => void answer(false)}
          />
        </div>
        <label class="seen">
          Ce que vous voyez sur l’imprimante
          <input type="text" maxlength={MAX_SEEN} bind:value={seen} />
        </label>
      </div>
    {/if}
  {/if}

  {#if copyFallback}
    <div class="copy-fallback" data-copy-fallback>
      <p class="note">
        Le presse-papiers n’est pas accessible depuis cette page. Sélectionnez le texte ci-dessous
        et faites Ctrl+C.
      </p>
      <textarea readonly bind:this={textareaEl} data-copy-text
        >{diagnosisReport(diagnosis ?? { target: '', steps: [] })}</textarea
      >
    </div>
  {/if}
</div>

<style>
  .diagnosis {
    margin-top: 1rem;
    padding-top: 1rem;
    border-top: 1px solid var(--border-soft);
  }

  h3 {
    margin: 0 0 0.5rem;
    font-size: 1.0625rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }

  .note {
    margin: 0.5rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .steps {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0.75rem 0 0;
    padding: 0;
    list-style: none;
  }

  .steps li {
    padding: 0.5rem 0.75rem;
    border-left: 0.25rem solid var(--border);
  }

  /* Un silence (unknown) ou une question encore ouverte (unanswered) ne sont PAS des
     échecs (décision 2, décision 3) : cette classe ne se pose que sur « fail ». */
  .steps li.fail {
    border-left: 0.25rem solid var(--fault);
    background: var(--fault-wash);
  }

  .step-title {
    margin: 0;
    font-weight: 700;
  }

  /* Ce que le poste a vu, à chasse fixe : un nombre d'octets ou un message technique brut
     s'y lit sans que la graisse variable n'en déforme la largeur. */
  .observed {
    margin: 0.25rem 0 0;
    font-family: 'Courier New', Courier, monospace;
    color: var(--ink-muted);
  }

  .remedy,
  .doc {
    margin: 0.25rem 0 0;
  }

  .ask {
    margin-top: 0.75rem;
    padding: 0.75rem;
    background: var(--bg);
    border-radius: var(--radius);
  }

  .question {
    margin: 0 0 0.5rem;
    font-weight: 700;
  }

  .seen {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    margin-top: 0.75rem;
    font-size: 1rem;
  }

  .seen input {
    height: 2.75rem;
    padding: 0 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .copy-fallback {
    margin-top: 0.75rem;
  }

  .copy-fallback textarea {
    width: 100%;
    min-height: 10rem;
    margin-top: 0.5rem;
    padding: 0.75rem;
    font-family: 'Courier New', Courier, monospace;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
</style>
