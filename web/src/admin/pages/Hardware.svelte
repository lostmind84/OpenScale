<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import Act from '../components/Act.svelte'
  import Field from '../components/Field.svelte'
  import * as api from '../lib/api'
  import { AdminError } from '../lib/api'
  import type { Draft } from '../lib/draft.svelte'
  import type { DetectionDTO, HealthDTO, PortDTO, PrinterDeviceDTO } from '../lib/dto'
  import { frenchDateTime, frenchDuration, frenchInteger } from '../lib/format'
  import type { LightLevel } from '../lib/lights'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Matériel de §14.4 — celle devant laquelle on est assis le jour de la mise en
   * service, un câble dans une main et le téléphone dans l'autre.
   *
   * Ce qu'elle change par rapport à un écran de réglages ordinaire tient en une phrase :
   * **c'est la détection qui répond à « y a-t-il une balance ? », pas l'exploitant.**
   * « Détecter automatiquement » ouvre chaque port, applique les parseurs et annonce
   * « COM8 : 12 trames valides, GRAM XFOC » — un exploitant n'a pas à savoir ce qu'est un
   * port série pour répondre à cette question.
   *
   * D'où sa forme : **deux panneaux à en-tête d'état**, ce que le poste OBSERVE au-dessus
   * et ce qu'on lui DÉCLARE en dessous, replié. Les vingt dernières trames brutes vivent
   * en permanence sous la balance — §14.4 les veut « toujours actif : ce n'est plus un
   * réglage » — parce qu'un poste muet et un poste qui parle sans être compris demandent
   * deux gestes opposés, et que rien d'autre à l'écran ne les distingue.
   *
   * Cette écoute permanente est encadrée par une chose que le matériel impose et qu'aucune
   * bonne intention ne contourne : **sous Windows un port série est EXCLUSIF**, et le
   * service n'expose pas de flux de trames mais une capture bornée qui TIENT le port trois
   * secondes. Trois règles en découlent, et elles se lisent dans {@link canListenOn} :
   *
   *  1. on n'écoute qu'un port que le poste a VRAIMENT énuméré — jamais le « C » qu'une
   *     saisie traverse en route vers « COM3 », qui ouvrirait un port inexistant ;
   *  2. un seul acte à la fois : l'écoute rend le port AVANT qu'un balayage ne parte, sans
   *     quoi la détection échoue sur le seul port où il y avait quelque chose à trouver ;
   *  3. tout ce que l'écran affirme de l'écoute est un état qu'il a vérifié — « je ne sais
   *     pas encore » tant que la configuration n'est pas arrivée.
   */
  interface Props {
    admin: Admin
    draft: Draft
    health: HealthDTO
  }

  const { admin, draft, health }: Props = $props()

  /** Combien de trames le visualiseur garde : les vingt dernières (§14.4). */
  const FRAMES_KEPT = 20
  /** La fenêtre d'une manche d'écoute, en secondes — celle du service (§14.4). */
  const LISTEN_SECONDS = 3
  /** Le souffle entre deux manches : le port se referme avant de se rouvrir. */
  const PAUSE_MS = 250
  /** À quelle cadence on regarde si la manche en cours a rendu le port. */
  const POLL_MS = 50
  /**
   * Combien de temps un acte attend le port au plus.
   *
   * Une manche, son souffle, et une marge : au-delà, le service ne rend plus la main et
   * désarmer la page pour toujours serait pire que de tenter l'ouverture.
   */
  const SILENCE_WAIT_MS = LISTEN_SECONDS * 1000 + PAUSE_MS + 1000
  /** Combien de lignes une liste montre au plus. Son total est annoncé à côté. */
  const ROWS_SHOWN = 12

  /** Les trois auto-tests d'impression de §8.6, par le jeton que la route attend. */
  type SelfTest = 'label' | 'alignment' | 'ruler'

  /**
   * Ce que la page est en train de faire, ou une chaîne vide quand elle ne fait rien.
   *
   * UN SEUL acte à la fois, et ce n'est pas une commodité d'affichage. D'abord le port :
   * il est exclusif, et deux gestes qui l'ouvrent ensemble s'excluent l'un l'autre.
   * Ensuite le mot de passe : `Admin.protect` n'a qu'une seule place pour l'acte qui
   * attend la réponse du panneau, et deux actes protégés lancés ensemble en perdraient un.
   *
   * Le nom porte son suffixe pour laisser `Act` au composant qui dessine les boutons.
   */
  type ActName = '' | 'ports' | 'printers' | 'discover' | 'detect' | 'listen' | SelfTest

  /** Ce qu'un port a répondu au balayage — y compris quand il a REFUSÉ de s'ouvrir. */
  interface Verdict {
    port: string
    message: string
    refused: boolean
  }

  /** L'en-tête d'état d'un panneau : un point, un mot, et la phrase qui va avec. */
  interface Standing {
    level: LightLevel
    /** Le mot français que lit un bénévole. Jamais un jeton du service. */
    word: string
    detail: string
  }

  /** Pourquoi l'écoute d'un port s'est arrêtée. Le port est nommé : la phrase le cite. */
  interface Halt {
    port: string
    reason: string
  }

  let ports = $state<PortDTO[]>([])
  let printers = $state<PrinterDeviceDTO[]>([])
  let verdicts = $state<Verdict[]>([])
  let frames = $state<string[]>([])
  /** Le port dont viennent les trames affichées : elles ne survivent pas à un changement. */
  let framesPort = $state('')
  /** Vrai une fois que l'énumération des ports a répondu, même avec zéro port. */
  let listed = $state(false)
  /** L'acte en vol, ou une chaîne vide. Les commandes se désarment tant qu'il dure. */
  let acting = $state<ActName>('')
  /** Combien de ports le balayage a ouverts, et combien il doit en ouvrir. */
  let scanned = $state(0)
  let toScan = $state(0)
  /** Vrai pendant qu'une manche d'écoute est en vol : le port est TENU. */
  let listening = $state(false)
  /** Ce qui a arrêté l'écoute, et sur quel port. Null tant qu'elle tourne. */
  let stopped = $state<Halt | null>(null)

  /**
   * Vrai quand la page est démontée.
   *
   * Ce n'est PAS un `$state` : rien à l'écran n'en dépend, et la boucle d'écoute doit
   * pouvoir le lire après que le composant a disparu.
   */
  let closed = false

  const scale = $derived(health.state.scale)
  const printer = $derived(health.state.printer)
  /**
   * La configuration est-elle arrivée ?
   *
   * Tant qu'elle ne l'est pas, `draft.flag('scale.present')` vaut FAUX — la valeur par
   * défaut d'une clé absente — et la page affichait « ce poste n'a pas de balance » coché
   * sur un poste qui en a une. Une page de réglages ne devine pas : elle attend.
   */
  const configRead = $derived(draft.config !== null)
  const declaredWithoutScale = $derived(configRead && !draft.flag('scale.present'))
  const port = $derived(draft.text('scale.options.port'))

  /** Ce qui a arrêté l'écoute du port ACTUEL, en français. Vide quand rien ne l'arrête. */
  const halt = $derived(stopped !== null && stopped.port === port ? stopped.reason : '')
  /** Les trames affichées : celles du port écouté, et d'aucun autre. */
  const shown = $derived(framesPort === port ? frames : [])

  const scaleStanding = $derived(standingOfScale())
  const printerStanding = $derived(standingOfPrinter())
  const framesCaption = $derived(captionOfFrames())
  /** Ce que le bouton de capture propose, ou rien quand il n'y a rien à demander. */
  const askLabel = $derived(labelOfAsk())
  /** Les réglages série s'ouvrent d'office quand un contrôle a refusé l'un d'eux. */
  const scaleRefused = $derived(
    faultOf('scale.type') !== '' || faultOf('scale.options.port') !== '',
  )
  const printerRefused = $derived(
    faultOf('printer.type') !== '' ||
      faultOf('printer.options.transport') !== '' ||
      faultOf('printer.options.queue') !== '',
  )

  onDestroy(() => {
    closed = true
  })

  onMount(() => {
    // L'énumération n'OUVRE aucun port : elle lit ce que la plateforme déclare. Elle est
    // faite d'office parce que l'écoute permanente n'écoute que des ports ÉNUMÉRÉS —
    // sans cette lecture, elle ne démarrerait jamais.
    void act('ports', loadPorts)
  })

  /**
   * L'écoute permanente des trames (§14.4).
   *
   * Elle démarre d'elle-même dès que {@link canListenOn} est vrai et se relance manche
   * après manche : ce n'est pas un bouton, c'est l'état normal de la page. `listening` est
   * lu ici pour qu'une boucle qui s'arrête — le port a changé, un acte est passé — en
   * fasse repartir une autre quand la voie est de nouveau libre.
   */
  $effect(() => {
    if (listening || !canListenOn(port)) return
    void listen(port)
  })

  /**
   * Peut-on écouter ce port, maintenant, honnêtement ?
   *
   * Chacune de ces conditions a coûté un défaut. `known` en particulier : `Field` émet à
   * chaque caractère, et sans elle un « C » tapé en route vers « COM3 » ouvrait un port
   * qui n'a jamais existé, dont le refus figeait l'écoute pour de bon.
   *
   * @param candidate - le port qu'on voudrait écouter.
   */
  function canListenOn(candidate: string): boolean {
    return (
      !closed &&
      configRead &&
      !declaredWithoutScale &&
      candidate !== '' &&
      candidate === port &&
      known(candidate) &&
      halt === '' &&
      acting === ''
    )
  }

  /** Vrai quand ce nom est celui d'un port que le poste a VRAIMENT énuméré. */
  function known(name: string): boolean {
    return ports.some((candidate) => candidate.name === name)
  }

  /**
   * Écoute un port, manche après manche, tant que la voie reste libre.
   *
   * Un refus ARRÊTE la boucle au lieu de la faire tourner dans le vide : sur un poste en
   * service, le port série est déjà tenu par la boucle de production — il est EXCLUSIF
   * sous Windows — et le service répond alors par une phrase qu'il faut lire une fois, pas
   * quatre fois par seconde. La route de capture est protégée (`internal/web/server.go`) :
   * son 401 arrive ici comme les autres refus, et c'est « Reprendre l'écoute » qui demande
   * le mot de passe, pas l'ouverture de la page (ADR-033).
   *
   * @param listenOn - le port à écouter.
   */
  async function listen(listenOn: string): Promise<void> {
    listening = true
    try {
      while (canListenOn(listenOn)) {
        try {
          keep(listenOn, await api.captureFrames(listenOn, LISTEN_SECONDS))
        } catch (failure) {
          stopped = { port: listenOn, reason: sentenceOf(failure) }
          return
        }
        await pause()
      }
    } finally {
      listening = false
    }
  }

  /**
   * « Reprendre l'écoute » : le geste qui, LUI, est un acte protégé (ADR-033).
   *
   * La boucle ci-dessus appelle la route en direct et se tait si le poste la refuse.
   * Faire surgir le panneau de mot de passe à la seule OUVERTURE de la page remettrait la
   * porte qu'ADR-033 vient d'enlever ; le demander quand l'exploitant INSISTE est
   * exactement ce que cet ADR décrit. C'est aussi la seule façon d'écouter un port que la
   * plateforme n'énumère pas — un fichier de périphérique saisi à la main.
   */
  async function listenOnce(): Promise<void> {
    const listenOn = port
    const heard = await admin.protect(() => api.captureFrames(listenOn, LISTEN_SECONDS))
    if (heard === null) return
    keep(listenOn, heard)
    // L'effet voit l'arrêt levé et relance la boucle, si le port est énuméré.
    stopped = null
  }

  /**
   * Garde les vingt dernières trames, et rien de plus (§14.4).
   *
   * Le port est retenu avec elles : une légende qui nomme COM3 au-dessus de trames venues
   * de COM8 est un mensonge que rien à l'écran ne rattrape.
   *
   * @param listenOn - le port qui les a rendues.
   * @param batch - ce que la manche a entendu.
   */
  function keep(listenOn: string, batch: string[]): void {
    if (closed || batch.length === 0) return
    const kept = listenOn === framesPort ? frames : []
    framesPort = listenOn
    frames = [...kept, ...batch].slice(-FRAMES_KEPT)
  }

  /**
   * Le souffle entre deux manches, ou la cadence à laquelle on guette le port.
   *
   * @param ms - combien de temps attendre.
   */
  function pause(ms = PAUSE_MS): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms))
  }

  /**
   * Attend que la manche d'écoute en cours RENDE LE PORT.
   *
   * `acting` est déjà posé quand on arrive ici : la boucle sort d'elle-même à la fin de sa
   * manche. Ce qui est attendu, c'est la capture EN VOL — trois secondes pendant lesquelles
   * le port est tenu et pendant lesquelles « Détecter automatiquement » ne trouverait sur
   * ce port qu'un « il est déjà utilisé », sur le seul port où il y avait quelque chose à
   * trouver. L'attente est BORNÉE : un service qui ne rendrait jamais la main ne doit pas
   * désarmer la page pour toujours.
   */
  async function silence(): Promise<void> {
    const until = Date.now() + SILENCE_WAIT_MS
    while (listening && Date.now() < until) await pause(POLL_MS)
  }

  /**
   * Passe un acte de cette page : un seul à la fois, l'écoute suspendue le temps qu'il dure.
   *
   * @param what - l'acte, tel que les libellés et les boutons le nomment.
   * @param action - ce qu'il faut faire une fois le port rendu.
   */
  async function act(what: ActName, action: () => Promise<void>): Promise<void> {
    if (acting !== '') return
    acting = what
    try {
      await silence()
      await action()
    } finally {
      acting = ''
    }
  }

  /** Énumère les ports série, avec la description USB qui nomme un câble visible. */
  async function loadPorts(): Promise<void> {
    const found = await admin.load(api.fetchPorts)
    if (found === null) return
    ports = found
    listed = true
  }

  /** « Lister les files » d'impression que la plateforme connaît. */
  async function loadPrinters(): Promise<void> {
    printers = (await admin.load(api.fetchPrinters)) ?? []
  }

  /**
   * « Rechercher l'imprimante » : le balayage réseau, qui prend des secondes.
   *
   * `POST /admin/api/printers/discover` est PROTÉGÉE (`internal/web/server.go`). Elle
   * passait par `admin.load`, qui avale le 401 : il n'en restait qu'un bandeau « cette
   * adresse demande une session ouverte », sans aucune porte pour la régler.
   */
  async function discover(): Promise<void> {
    const found = await admin.protect(api.discoverPrinters)
    if (found !== null) printers = found
  }

  /**
   * Un des trois auto-tests d'impression (§8.6).
   *
   * Protégé lui aussi, et chaque appui sort une étiquette POUR DE BON : c'est ce qui rend
   * l'état « en cours » de {@link act} nécessaire plutôt que décoratif.
   *
   * @param what - lequel des trois.
   */
  async function selfTest(what: SelfTest): Promise<void> {
    const done = await admin.protect(() => api.printerSelfTest(what))
    if (done === null) return
    admin.notice = done.message
    await admin.refresh()
  }

  /**
   * « Détecter automatiquement » : chaque port ouvert trois secondes, les parseurs
   * appliqués, et le verdict annoncé port par port.
   *
   * C'est un acte PROTÉGÉ : il ouvre un port série exclusif sur ce poste. Le panneau de
   * mot de passe s'ouvre s'il le faut, et `admin.protect` rejoue **tout le balayage** —
   * d'où la remise à zéro des verdicts au début de {@link scan}, qui rend le rejeu propre.
   */
  async function detect(): Promise<void> {
    scanned = 0
    toScan = 0
    if (!listed) await loadPorts()
    const list = ports
    if (list.length === 0) {
      verdicts = []
      return
    }
    await admin.protect(() => scan(list))
  }

  /**
   * Le balayage lui-même : un port après l'autre, et CHACUN laisse une ligne.
   *
   * Un port qui refuse de s'ouvrir — « il est déjà utilisé » — est le cas le plus fréquent
   * sur un poste en service, et c'était exactement celui que la page avalait : le port
   * disparaissait de la liste des verdicts, et l'exploitant en concluait qu'il n'existait
   * pas. Seuls les refus d'AUTHENTIFICATION remontent, parce qu'eux se règlent en
   * s'authentifiant et que `admin.protect` sait le faire.
   *
   * @param list - les ports à ouvrir, dans l'ordre où ils ont été énumérés.
   */
  async function scan(list: PortDTO[]): Promise<void> {
    verdicts = []
    toScan = list.length
    for (const [index, candidate] of list.entries()) {
      scanned = index + 1
      try {
        const report: DetectionDTO = await api.detectScale(candidate.name)
        verdicts = [...verdicts, { port: candidate.name, message: report.message, refused: false }]
      } catch (failure) {
        if (isCredentialRefusal(failure)) throw failure
        verdicts = [
          ...verdicts,
          { port: candidate.name, message: sentenceOf(failure), refused: true },
        ]
      }
    }
  }

  /** Ce que dit le bouton du balayage, et où il en est (« port 2 sur 5 »). */
  function detectLabel(): string {
    if (acting !== 'detect') return 'Détecter automatiquement'
    if (listening) return 'Détection : le port se libère…'
    if (toScan === 0) return 'Détection : énumération des ports…'
    return `Détection : port ${frenchInteger(scanned)} sur ${frenchInteger(toScan)}…`
  }

  /** L'en-tête d'état de la balance : ce que le poste OBSERVE, jamais ce qu'on déclare. */
  function standingOfScale(): Standing {
    if (!health.scale_present) {
      return {
        level: 'off',
        word: 'Sans balance',
        detail:
          'Ce poste est déclaré sans balance : le feu est éteint et le poids se saisit à la main.',
      }
    }
    if (!scale.connected) {
      return {
        level: 'fault',
        word: 'Sans réponse',
        detail:
          'Elle ne répond plus. Vérifiez le câble et l’alimentation, puis « Tester la ' +
          'balance » sur la page Dépannage.',
      }
    }
    if (scale.too_slow) {
      return {
        level: 'warn',
        word: 'Trop lente',
        detail:
          cadence() +
          ' À cette cadence, un poids serait déclaré périmé avant l’arrivée de la mesure suivante.',
      }
    }
    return { level: 'ok', word: 'Connectée', detail: 'Elle répond. ' + cadence() }
  }

  /** La cadence OBSERVÉE, et rien quand aucun intervalle n'a encore été mesuré. */
  function cadence(): string {
    if (scale.observations_count === 0) {
      return 'Aucun intervalle n’a encore été mesuré : la cadence sera connue dès les premières trames.'
    }
    const measured = `Une mesure toutes les ${frenchDuration(scale.median_ms)} sur ${frenchInteger(
      scale.observations_count,
    )} intervalles`
    return measured + (scale.provisional ? ', cadence encore provisoire.' : '.')
  }

  /**
   * L'en-tête d'état de l'imprimante, EN FRANÇAIS.
   *
   * `printer.health` vaut `ready`, `consumable`, `faulted` ou `unknown` : quatre jetons
   * anglais que la page affichait tels quels à un bénévole. Un jeton que cette table ne
   * connaît pas ne passe pas non plus — il devient « État inconnu », ce qui est la vérité.
   */
  function standingOfPrinter(): Standing {
    const said = PRINTER_STANDINGS[printer.health] ?? {
      level: 'unknown' as LightLevel,
      word: 'État inconnu',
      detail: 'Le poste a répondu un état que cet écran ne sait pas nommer.',
    }
    return { ...said, detail: printer.detail === '' ? said.detail : printer.detail }
  }

  /** Les quatre états que le superviseur d'impression publie (§13.1), et leurs mots. */
  const PRINTER_STANDINGS: Record<string, Standing> = {
    ready: {
      level: 'ok',
      word: 'Prête',
      detail: 'Elle répond et n’a rien à signaler.',
    },
    consumable: {
      level: 'warn',
      word: 'Rouleau en fin de vie',
      detail: 'Elle imprime, mais le rouleau arrive en fin de vie.',
    },
    faulted: {
      level: 'fault',
      word: 'En panne',
      detail: 'Elle ne peut pas imprimer.',
    },
    unknown: {
      level: 'unknown',
      word: 'Silencieuse',
      detail:
        'Elle prend les étiquettes et ne dit rien en retour : c’est la réponse normale ' +
        'd’une file Windows en RAW ou d’un fichier de périphérique, pas une panne.',
    },
  }

  /** Ce que l'imprimante a dit, et QUAND elle l'a dit. */
  function printerObservation(): string {
    const when =
      printer.observed_at === ''
        ? 'Jamais observée depuis le démarrage'
        : `Observée le ${frenchDateTime(printer.observed_at)}`
    const pending = printer.pending_jobs_count
    return `${when}, ${frenchInteger(pending)} ${pending > 1 ? 'travaux' : 'travail'} en attente.`
  }

  /** La légende du visualiseur : ce qui est écouté, puis ce qui a été entendu. */
  function captionOfFrames(): string {
    const heard = captionOfHeard()
    return heard === '' ? captionOfListening() : `${captionOfListening()} ${heard}`
  }

  /**
   * Ce que la page fait du port, en une phrase — et « je ne sais pas encore » quand c'est
   * la vérité.
   *
   * Tant que la configuration n'est pas arrivée, `port` vaut la chaîne vide parce que la
   * clé est ABSENTE, et non parce que personne ne l'a renseignée. La page affirmait
   * « Aucun port n'est indiqué » à trois centimètres de « cette page ne déclare rien de ce
   * poste ».
   */
  function captionOfListening(): string {
    if (!configRead) {
      return 'Lecture de la configuration en cours : le port à écouter n’est pas encore connu.'
    }
    if (declaredWithoutScale) return 'Ce poste est déclaré sans balance : aucun port n’est écouté.'
    if (port === '') {
      return 'Aucun port n’est indiqué : choisissez-en un dans la liste ci-dessus pour écouter les trames.'
    }
    if (!listed) {
      return acting === 'ports'
        ? `Énumération des ports en cours : l’écoute de ${port} démarre dès qu’il est vu.`
        : `Les ports de ce poste n’ont pas été énumérés : « Lister les ports » dira si ${port} existe.`
    }
    if (!known(port)) {
      return `${port} n’est pas visible depuis ce poste : rien n’est écouté en continu.`
    }
    if (halt !== '') return `L’écoute de ${port} est arrêtée.`
    if (acting !== '') return `L’écoute de ${port} est suspendue le temps de l’acte en cours.`
    return `Écoute de ${port}.`
  }

  /** Ce que le visualiseur montre, accordé à ce qu'il y a vraiment dedans. */
  function captionOfHeard(): string {
    if (shown.length === 0) return 'Aucune trame reçue pour l’instant.'
    // Une balance qui n'émet qu'au posé de sac rend UNE trame par manche : « les 1
    // dernières trames » est le cas normal de la mise en service, pas un cas limite.
    if (shown.length === 1) {
      return `Une seule trame reçue — ${frenchInteger(FRAMES_KEPT)} au plus sont gardées.`
    }
    return (
      `Les ${frenchInteger(shown.length)} dernières trames — ` +
      `${frenchInteger(FRAMES_KEPT)} au plus, la plus récente en bas.`
    )
  }

  /**
   * Ce que le bouton de capture propose, ou rien quand la boucle tourne toute seule.
   *
   * Deux situations le font apparaître, et une seule phrase ne couvre pas les deux : le
   * poste a REFUSÉ la dernière manche — il faut insister, avec le mot de passe s'il le
   * faut — ou le port n'est pas énuméré, et l'écoute permanente ne s'en saisira jamais.
   */
  function labelOfAsk(): string {
    if (!configRead || declaredWithoutScale || port === '') return ''
    if (halt !== '') return 'Reprendre l’écoute'
    if (listed && !known(port)) return 'Écouter ce port une fois'
    return ''
  }

  /**
   * Le total d'une liste, et son plafond quand elle en a un.
   *
   * Aucune liste de cette page n'est servie entière sans le dire : un poste peut porter
   * trente files d'impression — PDF, OneNote, télécopie — et une liste tronquée en
   * silence est une liste qui ment.
   *
   * @param singular - le nom au singulier, accord compris.
   * @param plural - le même au pluriel.
   * @param total - combien il y en a vraiment.
   */
  function census(singular: string, plural: string, total: number): string {
    const head = `${frenchInteger(total)} ${total > 1 ? plural : singular}`
    if (total <= ROWS_SHOWN) return head + '.'
    // « lignes » et non le nom compté : l'accord reste juste quel que soit ce qu'on liste.
    return `${head} — seules les ${frenchInteger(ROWS_SHOWN)} premières lignes sont affichées.`
  }

  /**
   * Ce qu'un champ propose en autocomplétion, PLAFONNÉ.
   *
   * `Field` imprime cette liste en toutes lettres — « Valeurs acceptées : … » — dès qu'un
   * contrôle de §11.3 refuse la clé. Les noms DÉTECTÉS y arrivaient entiers, les trente
   * files d'impression comprises, alors que les listes de la page sont plafonnées douze
   * lignes plus haut. Ce qu'un contrôle a NOMMÉ passe entier, lui : c'est sa réponse, elle
   * énumère des valeurs acceptables et en cacher une renverrait chercher ailleurs.
   *
   * @param path - le chemin de la clé.
   * @param detected - les noms que le poste a détectés, quand il en a détecté.
   */
  function suggestions(path: string, detected: string[]): string[] {
    const named = allowedFor(path)
    return named.length > 0 ? named : detected.slice(0, ROWS_SHOWN)
  }

  /** Les octets d'une trame, en hexadécimal : ce qu'un support demande d'abord. */
  function hexOf(frame: string): string {
    return [...new TextEncoder().encode(frame)]
      .map((byte) => byte.toString(16).toUpperCase().padStart(2, '0'))
      .join(' ')
  }

  /** Les caractères de commande qu'une trame de balance porte (§9.1), par leur nom. */
  const CONTROL_NAMES: Record<number, string> = {
    0: 'NUL',
    2: 'STX',
    3: 'ETX',
    4: 'EOT',
    5: 'ENQ',
    6: 'ACK',
    9: 'TAB',
    10: 'LF',
    13: 'CR',
    21: 'NAK',
    27: 'ESC',
    127: 'DEL',
  }

  /**
   * La trame DÉCODÉE : les mêmes octets tels qu'un humain les lit.
   *
   * C'est le « dump hexa + ASCII » de `openscale capture` (§15.1) porté à l'écran. Les
   * caractères de commande sont NOMMÉS plutôt que rendus : c'est le STX manquant ou le CR
   * en trop qui explique un poste muet, et un caractère invisible ne se lit pas au
   * téléphone.
   *
   * @param frame - une trame brute, telle que le poste l'a lue sur le câble.
   */
  function decodedOf(frame: string): string {
    let read = ''
    for (const character of frame) {
      const code = character.codePointAt(0) ?? 0
      if (code >= 32 && code !== 127) {
        read += character
        continue
      }
      read += `⟨${CONTROL_NAMES[code] ?? code.toString(16).toUpperCase().padStart(2, '0')}⟩`
    }
    return read
  }

  /** Le message du contrôle qui a refusé cette clé, quand il y en a un (§11.3). */
  function faultOf(path: string): string {
    return draft.faults.find((fault) => fault.field === path)?.message ?? ''
  }

  /** Les valeurs qu'un contrôle a nommées comme acceptables. */
  function allowedFor(path: string): string[] {
    return draft.faults.find((fault) => fault.field === path)?.allowed ?? []
  }

  /**
   * La phrase d'un refus : celle du service quand il en a écrit une.
   *
   * Recopiée de `session.svelte.ts`, qui ne l'exporte pas : ce qui arrive ici sont des
   * refus PORT PAR PORT, que la page affiche elle-même au lieu de les remonter au bandeau
   * — sans quoi le dernier port effacerait le verdict de tous les autres.
   */
  function sentenceOf(failure: unknown): string {
    if (failure instanceof AdminError) return failure.message
    if (failure instanceof Error) return 'Le poste n’a pas répondu : ' + failure.message
    return 'Le poste n’a pas répondu.'
  }

  /** Vrai quand un refus se règle en s'authentifiant : il REMONTE jusqu'à `protect`. */
  function isCredentialRefusal(failure: unknown): boolean {
    return failure instanceof AdminError && (failure.status === 401 || failure.status === 409)
  }

  /** Les trois auto-tests, et le nom français de chacun. */
  const SELF_TESTS: { what: SelfTest; name: string }[] = [
    { what: 'label', name: 'étiquette' },
    { what: 'alignment', name: 'alignement' },
    { what: 'ruler', name: 'réglette' },
  ]
</script>

<div class="pages">
  <section class="panel">
    <header class="head">
      <h2>Balance</h2>
      <span class="standing" data-standing="scale" data-level={scaleStanding.level}>
        <span class="dot"></span>{scaleStanding.word}
      </span>
    </header>
    <p class="fact">{scaleStanding.detail}</p>
    <p class="note">
      L’état et la cadence viennent de ce que le poste observe vraiment, jamais d’un réglage.
    </p>

    {#if configRead}
      <label class="check">
        <input
          type="checkbox"
          checked={declaredWithoutScale}
          onchange={(event) => draft.set('scale.present', !event.currentTarget.checked)}
        />
        <span>
          Ce poste n’a pas de balance
          <small>Le feu s’ÉTEINT au lieu de rester rouge, et la saisie manuelle devient le mode normal.</small>
        </span>
      </label>
    {:else}
      <p class="waiting" data-reading>
        Lecture de la configuration en cours… tant qu’elle n’est pas arrivée, cette page ne
        déclare rien de ce poste.
      </p>
    {/if}

    <div class="actions">
      <Act
        label="Lister les ports"
        busy={acting === 'ports'}
        disabled={acting !== ''}
        onrun={() => void act('ports', loadPorts)}
      />
      <!--
        Le seul acte de cette page dont le libellé DIT OÙ IL EN EST : « port 2 sur 5 » sur
        un balayage qui dure une minute vaut mieux que « En cours… », donc il porte son
        avancement lui-même et laisse `busy` de côté.
      -->
      <Act
        act="detect"
        label={detectLabel()}
        disabled={acting !== ''}
        onrun={() => void act('detect', detect)}
      />
    </div>

    {#if listed}
      <p class="count" data-ports-count>
        {ports.length === 0
          ? 'Aucun port série n’est visible depuis ce poste.'
          : census('port détecté', 'ports détectés', ports.length)}
      </p>
      {#if ports.length > 0}
        <ul class="list">
          {#each ports.slice(0, ROWS_SHOWN) as candidate (candidate.name)}
            <li>
              <button
                type="button"
                class="pick"
                onclick={() => draft.set('scale.options.port', candidate.name)}
              >
                {candidate.name}
              </button>
              <span class="detail">
                {candidate.description === '' ? 'aucune description USB' : candidate.description}
                {candidate.vid === '' ? '' : ` — VID ${candidate.vid} PID ${candidate.pid}`}
              </span>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}

    {#if verdicts.length > 0}
      <p class="count">{census('port interrogé', 'ports interrogés', verdicts.length)}</p>
      <ul class="list" data-detections>
        {#each verdicts.slice(0, ROWS_SHOWN) as verdict (verdict.port)}
          <li data-verdict class:refused={verdict.refused}>
            <span class="what">{verdict.port}</span>
            <span class="detail">{verdict.message}</span>
          </li>
        {/each}
      </ul>
    {/if}

    <!--
      Les réglages, repliés — et DÉSARMÉS tant que la configuration n'est pas arrivée :
      `draft.set` jette en silence ce qu'on écrit dans un document qui n'existe pas encore.
    -->
    <details class="folded" open={scaleRefused}>
      <summary>Réglages série de la balance</summary>
      <div class="folded-body">
        <Field
          label="Protocole"
          path="scale.type"
          value={draft.text('scale.type')}
          hint="Les valeurs acceptées apparaissent ici si l’enregistrement est refusé."
          fault={faultOf('scale.type')}
          allowed={allowedFor('scale.type')}
          disabled={!configRead}
          onchange={(value) => draft.set('scale.type', value)}
        />
        <Field
          label="Port série"
          path="scale.options.port"
          value={draft.text('scale.options.port')}
          hint="Choisissez-le dans la liste détectée ci-dessus plutôt que de le taper : l’écoute permanente ne suit que des ports détectés."
          fault={faultOf('scale.options.port')}
          allowed={suggestions(
            'scale.options.port',
            ports.map((candidate) => candidate.name),
          )}
          disabled={!configRead}
          onchange={(value) => draft.set('scale.options.port', value)}
        />
      </div>
    </details>

    <!--
      Le visualiseur des vingt dernières trames. Il n'a pas de bouton tant que la boucle
      tourne — §14.4 le veut « toujours actif : ce n'est plus un réglage » — et il en
      montre un exactement quand elle ne peut pas tourner, en disant pourquoi.
    -->
    <div class="frames" data-frames>
      <p class="frames-head" data-caption>{framesCaption}</p>
      {#if halt !== '' || askLabel !== ''}
        <p class="interrupted" data-interrupted>
          {halt}
          {#if askLabel !== ''}
            <Act
              act="listen"
              label={askLabel}
              busy={acting === 'listen'}
              disabled={acting !== ''}
              onrun={() => void act('listen', listenOnce)}
            />
          {/if}
        </p>
      {/if}
      {#if shown.length > 0}
        <ol class="frame-rows">
          {#each shown as frame, index (index)}
            <li data-frame>
              <code class="hex" data-hex>{hexOf(frame)}</code>
              <code class="decoded" data-decoded>{decodedOf(frame)}</code>
            </li>
          {/each}
        </ol>
      {/if}
    </div>
  </section>

  <section class="panel">
    <header class="head">
      <h2>Imprimante</h2>
      <span class="standing" data-standing="printer" data-level={printerStanding.level}>
        <span class="dot"></span>{printerStanding.word}
      </span>
    </header>
    <p class="fact">{printerStanding.detail}</p>
    <p class="note">{printerObservation()}</p>

    <div class="actions">
      <!--
        `disabled` porte « un acte tourne quelque part sur la page », `busy` porte
        « c'est CELUI-CI » : le premier désarme, le second est le seul à rester
        pleinement lisible.
      -->
      <Act
        label="Lister les files"
        busy={acting === 'printers'}
        disabled={acting !== ''}
        onrun={() => void act('printers', loadPrinters)}
      />
      <Act
        label="Rechercher l’imprimante"
        busy={acting === 'discover'}
        disabled={acting !== ''}
        onrun={() => void act('discover', discover)}
      />
      <!--
        Trois auto-tests côte à côte : le libellé garde LEQUEL travaille. Réduits tous les
        trois à « En cours… », rien à l'écran ne dirait plus lequel sort une étiquette.
      -->
      {#each SELF_TESTS as test (test.what)}
        <Act
          act={test.what}
          label={`Auto-test : ${test.name}${acting === test.what ? ' — en cours…' : ''}`}
          disabled={acting !== ''}
          onrun={() => void act(test.what, () => selfTest(test.what))}
        />
      {/each}
    </div>

    {#if printers.length > 0}
      <p class="count" data-printers-count>
        {census('file d’impression', 'files d’impression', printers.length)}
      </p>
      <ul class="list">
        {#each printers.slice(0, ROWS_SHOWN) as device (device.name)}
          <li>
            <button
              type="button"
              class="pick"
              onclick={() => draft.set('printer.options.queue', device.name)}
            >
              {device.name}
            </button>
            <span class="detail">
              {device.detail}{device.default ? ' — file par défaut du système' : ''}
            </span>
          </li>
        {/each}
      </ul>
    {/if}

    <details class="folded" open={printerRefused}>
      <summary>Réglages de l’imprimante</summary>
      <div class="folded-body">
        <Field
          label="Driver"
          path="printer.type"
          value={draft.text('printer.type')}
          hint="Gardez le driver raster : c’est celui que les postes en service utilisent."
          fault={faultOf('printer.type')}
          allowed={allowedFor('printer.type')}
          disabled={!configRead}
          onchange={(value) => draft.set('printer.type', value)}
        />
        <Field
          label="Transport"
          path="printer.options.transport"
          value={draft.text('printer.options.transport')}
          hint="Local par défaut : une file Windows ou un fichier de périphérique de ce poste."
          fault={faultOf('printer.options.transport')}
          allowed={allowedFor('printer.options.transport')}
          disabled={!configRead}
          onchange={(value) => draft.set('printer.options.transport', value)}
        />
        <Field
          label="File ou chemin"
          path="printer.options.queue"
          value={draft.text('printer.options.queue')}
          hint="Choisissez-la dans la liste ci-dessus : une file mal orthographiée ne s’imprime pas."
          fault={faultOf('printer.options.queue')}
          allowed={suggestions(
            'printer.options.queue',
            printers.map((device) => device.name),
          )}
          disabled={!configRead}
          onchange={(value) => draft.set('printer.options.queue', value)}
        />
      </div>
    </details>
  </section>
</div>

<style>
  /*
   * Deux panneaux, et la même règle dans les deux : ce que le poste OBSERVE en haut, ce
   * qu'on lui DÉCLARE en bas et replié. La page présentait neuf puis onze commandes à
   * plat, où rien ne disait laquelle décrit le monde et laquelle le change.
   *
   * La densité est celle de l'administration (ADR-033) : 44 px sur les commandes de
   * formulaire, et AUCUNE cible de 72 px ici — rien sur cette page ne change ce que le
   * poste vend ni la façon dont il pèse. Un auto-test mal touché coûte une étiquette ;
   * « Basculer en saisie manuelle », qui est de l'autre côté de cette frontière, garde
   * ses 72 px sur la page Dépannage.
   */
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .panel {
    padding: 1rem 1.25rem 1.25rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-1);
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    justify-content: space-between;
  }

  h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 700;
  }

  /*
   * L'en-tête d'état : un point de couleur et un MOT FRANÇAIS.
   *
   * La couleur porte le sens et les lettres le répètent — §14.2 interdit de confier une
   * information à la seule teinte, et --warning comme --fault n'atteignent pas les 7:1
   * exigés d'un texte : ils sont donc un point et un lavis, jamais de l'encre.
   */
  .standing {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    height: 2rem;
    padding: 0 0.75rem;
    border-radius: var(--radius-pill);
    background: var(--waiting-wash);
    font-size: 1rem;
    font-weight: 700;
  }

  .standing[data-level='ok'] {
    background: var(--ready-wash);
  }

  .standing[data-level='warn'] {
    background: var(--warning-wash);
  }

  .standing[data-level='fault'] {
    background: var(--fault-wash);
  }

  .dot {
    width: 0.625rem;
    height: 0.625rem;
    border-radius: var(--radius-pill);
    background: var(--waiting);
  }

  .standing[data-level='ok'] .dot {
    background: var(--ready);
  }

  .standing[data-level='warn'] .dot {
    background: var(--warning);
  }

  .standing[data-level='fault'] .dot {
    background: var(--fault);
  }

  .fact {
    margin: 0.75rem 0 0;
    font-size: 1.125rem;
  }

  .note,
  .count,
  .waiting {
    margin: 0.25rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .check {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    /* 44 px : la densité des commandes de formulaire de l'administration (ADR-033). */
    min-height: 2.75rem;
    margin-top: 0.5rem;
    font-size: 1.0625rem;
  }

  .check input {
    width: 1.5rem;
    height: 1.5rem;
    flex: 0 0 auto;
  }

  .check small {
    display: block;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin: 0.75rem 0 0;
  }

  /* Ce qu'une souris attend, et qu'un doigt n'a jamais demandé (app.css). */
  @media (hover: hover) {
    .pick:hover,
    summary:hover {
      background: var(--bg);
    }
  }

  .list {
    margin: 0.25rem 0 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
  }

  .list li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    padding: 0.125rem 0;
    border-top: 1px solid var(--border-soft);
    font-size: 1.0625rem;
  }

  /* Un port qui a REFUSÉ de s'ouvrir : la ligne existe, et elle se voit. */
  .list li.refused {
    padding-left: 0.5rem;
    border-left: 0.25rem solid var(--fault);
    background: var(--fault-wash);
  }

  .pick {
    height: 2.75rem;
    padding: 0 0.75rem;
    font-weight: 700;
    text-decoration: underline;
    border-radius: var(--radius-sm);
    transition: background-color var(--tap) var(--ease);
  }

  .what {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }

  /*
   * Les réglages, repliés.
   *
   * `summary` n'est pas un bouton : le retour d'appui de `app.css` ne l'atteint pas, et
   * une commande qui ne bouge pas sous le doigt fait lire la page comme une image. Ce que
   * `app.css` ne porte pas non plus jusqu'ici, c'est l'EXEMPTION — sa règle de
   * `prefers-reduced-motion` vise `button:active` — et un retour d'appui qui reste sous
   * « animations réduites » est précisément ce que ce réglage demande de retirer.
   */
  .folded {
    margin-top: 1rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  summary {
    display: flex;
    align-items: center;
    height: 2.75rem;
    padding: 0 0.75rem;
    font-size: 1.0625rem;
    font-weight: 700;
    border-radius: var(--radius);
    cursor: pointer;
    transition:
      background-color var(--tap) var(--ease),
      transform var(--tap) var(--ease);
  }

  summary:active {
    transform: scale(0.975);
  }

  @media (prefers-reduced-motion: reduce) {
    summary:active {
      transform: none;
    }
  }

  /*
   * Le visualiseur de trames : borné, défilant, et SÉLECTIONNABLE.
   *
   * Le poste interdit la sélection partout ailleurs — ce n'est pas un document (app.css)
   * — mais ces lignes-là sont ce qu'un support demande au téléphone, et les recopier à la
   * main est le meilleur moyen d'appeler pour rien.
   */
  .frames {
    margin-top: 1rem;
    max-height: 18rem;
    overflow-y: auto;
    background: var(--waiting-wash);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    user-select: text;
  }

  .frames-head {
    position: sticky;
    top: 0;
    margin: 0;
    padding: 0.5rem 0.75rem;
    background: var(--surface);
    border-radius: var(--radius) var(--radius) 0 0;
    box-shadow: var(--shadow-2);
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .interrupted {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    margin: 0;
    padding: 0.5rem 0.75rem;
    background: var(--warning-wash);
    border-left: 0.375rem solid var(--warning);
    font-size: 1rem;
  }

  .frame-rows {
    margin: 0;
    padding: 0.5rem 0.75rem;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .frame-rows li {
    display: grid;
    /* L'hexa d'abord, la lecture ensuite : c'est l'ordre dans lequel on les compare. */
    grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
    gap: 0.75rem;
    font-size: 0.9375rem;
  }

  /* Une trame longue défile DANS sa colonne : le corps de la page, jamais. */
  .hex,
  .decoded {
    overflow-x: auto;
    white-space: pre;
  }

  .decoded {
    color: var(--ink-muted);
  }
</style>
