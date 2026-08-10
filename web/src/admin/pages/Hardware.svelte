<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import Act from '../components/Act.svelte'
  import Field from '../components/Field.svelte'
  import StandingHeader from '../components/StandingHeader.svelte'
  import * as api from '../lib/api'
  import { AdminError, isCredentialRefusal } from '../lib/api'
  import type { Draft } from '../lib/draft.svelte'
  import type { DetectionDTO, HealthDTO, PortDTO, PrinterDeviceDTO } from '../lib/dto'
  import { allowedFor, faultOf } from '../lib/faults'
  import { labelOf } from '../lib/fields'
  import { decodedOf, hexOf } from '../lib/frames'
  import { askLabel, detectLabel, framesCaption, type Listening } from '../lib/listening'
  import type { Admin } from '../lib/session.svelte'
  import { printerObservation, standingOfPrinter, standingOfScale } from '../lib/standing'
  import { census } from '../lib/tally'
  import {
    DEVICE_HINTS,
    DEVICE_KEYS,
    deviceKeyOf,
    reachElsewhere,
    transportChoices,
  } from '../lib/transports'

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
   * What the page is doing, or an empty string when it is doing nothing.
   *
   * ONE act at a time, and that is no display convenience. The port first: it is
   * exclusive, and two gestures opening it together rule each other out. Then the
   * password: `Admin.protect` has a single slot for the act waiting on the panel's
   * answer, and two protected acts started together would lose one of them.
   *
   * The name carries its suffix to leave `Act` to the component that draws the buttons.
   */
  type ActName = '' | 'ports' | 'printers' | 'discover' | 'detect' | 'listen' | SelfTest

  /**
   * Ce qu'un port a répondu au balayage — y compris quand il a REFUSÉ de s'ouvrir.
   *
   * Le rapport est gardé au-delà de sa phrase. Ce qu'il porte d'autre est ce qui permet
   * d'AGIR : le protocole que les trames ont nommé est exactement la valeur que
   * `scale.type` attend, et n'en garder que le message ne laissait à l'écran qu'une bonne
   * nouvelle sur laquelle on ne pouvait rien faire.
   */
  interface Verdict {
    port: string
    message: string
    /** Le driver qui a reconnu ce qui sortait du câble, ou une chaîne vide. */
    driver: string
    /** Combien de trames valides la fenêtre de détection a comptées. */
    validFrames: number
    refused: boolean
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
   * Les deux volets de réglages sont-ils ouverts ?
   *
   * La réponse appartient au BÉNÉVOLE, et elle vit ici plutôt que dans une expression du
   * gabarit. `open` est une PROPRIÉTÉ du DOM : `open={uneExpression}` ne compile pas comme
   * un attribut ordinaire mais en affectation directe, sans la mémoïsation qu'une écriture
   * d'attribut porte, et cette affectation est fondue dans l'effet de gabarit du fragment
   * — celui que le moindre changement du tableau de bord rejoue. Le volet se refermait donc
   * tout seul trois secondes après avoir été ouvert, sous les doigts de qui tapait un nom
   * de port dedans, et rien d'autre à l'écran ne trahissait le passage de l'effet.
   */
  let scaleSettingsOpen = $state(false)
  let printerSettingsOpen = $state(false)

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

  /** Les transports que CE POSTE porte, et pour chacun la clé où il fait écrire (§8.4). */
  const transports = $derived(health.printer_transports)
  const transport = $derived(draft.text('printer.options.transport'))
  /** Ce que la liste « Transport » propose, la valeur en cours COMPRISE. */
  const choices = $derived(transportChoices(transports, transport))
  /** La clé de `printer.options` que le transport CHOISI lit, et elle seule. */
  const deviceKey = $derived(deviceKeyOf(transports, transport))
  const devicePath = $derived('printer.options.' + deviceKey)
  /**
   * Les destinations que le transport choisi peut VRAIMENT ouvrir.
   *
   * Même règle que pour les auto-tests trente lignes plus bas (ADR-025) : une destination
   * qu'un clic écrirait dans une clé que le transport en service ne lit pas n'est pas un
   * choix. C'est ce clic-là qui mettait « 192.168.0.43:9100 » dans `printer.options.queue`
   * — un fichier que rien ne refuse et que le poste ne sait pas imprimer.
   */
  const reachable = $derived(printers.filter((device) => device.key === deviceKey))
  /** Combien de destinations sont écartées parce qu'un autre transport les lit. */
  const unreachable = $derived(printers.length - reachable.length)

  const scaleStanding = $derived(standingOfScale(health.scale_present, scale))
  const printerStanding = $derived(standingOfPrinter(printer))

  /**
   * Tout ce que les phrases du visualiseur lisent, rassemblé en un objet.
   *
   * Elles le prennent ENTIER plutôt que par morceaux : chacune d'elles doit pouvoir dire
   * « je ne sais pas encore », et une fonction à qui l'on cache la moitié de l'état le
   * devine au lieu de le lire.
   */
  const watch = $derived<Listening>({
    configRead,
    declaredWithoutScale,
    port,
    listed,
    portKnown: known(port),
    halt,
    acting,
    framesShown: shown.length,
    framesKept: FRAMES_KEPT,
  })

  const caption = $derived(framesCaption(watch))
  /** Ce que le bouton de capture propose, ou rien quand il n'y a rien à demander. */
  const ask = $derived(askLabel(watch))
  /** Ce que dit le bouton du balayage, et où il en est. */
  const detecting = $derived(detectLabel({ acting, listening, scanned, toScan }))
  /** Les réglages série s'ouvrent d'office quand un contrôle a refusé l'un d'eux. */
  const scaleRefused = $derived(
    faultOf(draft, 'scale.type') !== '' || faultOf(draft, 'scale.options.port') !== '',
  )
  const printerRefused = $derived(
    faultOf(draft, 'printer.type') !== '' ||
      faultOf(draft, 'printer.options.transport') !== '' ||
      // Les TROIS clés d'appareil, et pas seulement la file : un refus sur l'adresse
      // laissait le volet fermé sur le champ qu'il fallait corriger.
      DEVICE_KEYS.some((key) => faultOf(draft, 'printer.options.' + key) !== ''),
  )

  /**
   * Un refus de contrôle OUVRE le volet qui porte le champ fautif, et rien de plus.
   *
   * Ces deux effets n'écrivent que dans un sens : ils ouvrent, ils ne referment jamais. Un
   * volet ne se referme donc plus quand le refus disparaît — c'est une décision, pas un
   * oubli : on travaille dedans, et le replier à l'instant où l'enregistrement passe
   * emporterait le champ qu'on vient de corriger.
   *
   * Ils ne LISENT pas l'état qu'ils écrivent : une lecture ferait de l'effet sa propre
   * dépendance, et le volet se rouvrirait à chaque fois qu'un doigt le referme.
   */
  $effect(() => {
    if (scaleRefused) scaleSettingsOpen = true
  })

  $effect(() => {
    if (printerRefused) printerSettingsOpen = true
  })

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
        verdicts = [
          ...verdicts,
          {
            port: candidate.name,
            message: report.message,
            driver: report.driver,
            validFrames: report.valid_frames_count,
            refused: false,
          },
        ]
      } catch (failure) {
        if (isCredentialRefusal(failure)) throw failure
        verdicts = [
          ...verdicts,
          {
            port: candidate.name,
            message: sentenceOf(failure),
            driver: '',
            validFrames: 0,
            refused: true,
          },
        ]
      }
    }
  }

  /**
   * Ce port a-t-il fait reconnaître une balance ?
   *
   * Les deux témoins, et pas un seul : le rapport ne nomme un driver que lorsque des
   * trames l'ont désigné (`cmd/openscale/detect.go`), et un port qui a refusé de s'ouvrir
   * n'en porte aucun des deux. Mettre en service un protocole que rien sur le câble n'a
   * confirmé est exactement ce qu'un balayage sert à éviter.
   *
   * @param verdict - le verdict d'un port.
   */
  function recognised(verdict: Verdict): boolean {
    return verdict.driver !== '' && verdict.validFrames > 0
  }

  /**
   * Met en service la balance qu'un port vient de faire reconnaître.
   *
   * La détection SAIT quel protocole a répondu — le driver retenu est celui qui a reconnu
   * ce qui sortait du câble, jamais la première entrée d'un registre — et la page n'en
   * gardait que la phrase. Il restait alors trois gestes à faire de tête, dont deux sont
   * cachés : décocher « ce poste n'a pas de balance », déplier les réglages série et y
   * retaper ce protocole DE MÉMOIRE. C'était la seule raison pour laquelle un poste dont
   * la balance venait d'être détectée restait en configuration d'usine.
   *
   * Elle écrit dans le BROUILLON et n'applique rien : « Enregistrer » reste le seul geste
   * de cette page qui parte vers le poste.
   *
   * @param verdict - le verdict du port reconnu.
   */
  function useScale(verdict: Verdict): void {
    draft.set('scale.present', true)
    draft.set('scale.type', verdict.driver)
    draft.set('scale.options.port', verdict.port)
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
    const named = allowedFor(draft, path)
    return named.length > 0 ? named : detected.slice(0, ROWS_SHOWN)
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

  /** Les trois auto-tests de §8.6, et le nom français de chacun. */
  const SELF_TESTS: { what: SelfTest; name: string }[] = [
    { what: 'label', name: 'étiquette' },
    { what: 'alignment', name: 'alignement' },
    { what: 'ruler', name: 'réglette' },
  ]

  /**
   * Ceux que le driver EN SERVICE honore vraiment, et eux seuls.
   *
   * La page dessinait les trois quel que soit le driver. Sur un poste en `preview` — celui
   * sur lequel une configuration d'usine se replie (§11.3) — deux d'entre eux répondaient
   * « cet auto-test se lit sur une étiquette imprimée » APRÈS le clic, devant quelqu'un qui
   * cherchait déjà pourquoi rien ne sort. Un bouton dont la seule réponse possible est un
   * refus n'est pas un choix : le poste déclare ce qu'il honore, l'écran n'affiche que ça
   * (ADR-025).
   *
   * L'ordre reste celui de §8.6, jamais celui du service : c'est celui dans lequel le
   * document les présente, et celui qu'un bénévole a sous les yeux dans la documentation.
   */
  const offered = $derived(SELF_TESTS.filter((test) => health.printer_self_tests.includes(test.what)))
</script>

<div class="pages">
  <section class="panel">
    <StandingHeader title="Balance" name="scale" standing={scaleStanding} />
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
      <!--
        The key badge is DECLARED on the markup and never deduced: `Act` has no way of
        seeing which route a handler ends up calling, so a protected act whose markup stays
        silent asks for the password AFTER the click — the one moment ADR-033 wanted it not
        to surprise anybody. Six buttons of this page were in that case.

        The two exceptions are here on purpose: « Lister les ports » and « Lister les
        files » are the reads of the open table (`internal/web/server.go`), and they are the
        only acts of this page that carry no badge. Everything else goes through
        `admin.protect`, which is the same list, checked handler by handler.
      -->
      <Act
        label="Lister les ports"
        busy={acting === 'ports'}
        disabled={acting !== ''}
        onrun={() => void act('ports', loadPorts)}
      />
      <!--
        The only act of this page whose label SAYS HOW FAR IT HAS GOT: « port 2 sur 5 » on
        a scan that runs for a minute is worth more than « En cours… », so it carries its
        own progress and leaves `busy` aside.
      -->
      <Act
        act="detect"
        label={detecting}
        protected
        disabled={acting !== ''}
        onrun={() => void act('detect', detect)}
      />
    </div>

    {#if listed}
      <p class="count" data-ports-count>
        {ports.length === 0
          ? 'Aucun port série n’est visible depuis ce poste.'
          : census('port détecté', 'ports détectés', ports.length, ROWS_SHOWN)}
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
      <p class="count">
        {census('port interrogé', 'ports interrogés', verdicts.length, ROWS_SHOWN)}
      </p>
      <ul class="list" data-detections>
        {#each verdicts.slice(0, ROWS_SHOWN) as verdict (verdict.port)}
          <li data-verdict class:refused={verdict.refused}>
            <span class="what">{verdict.port}</span>
            <span class="detail">{verdict.message}</span>
            <!--
              Offert au SEUL port reconnu. Un geste inerte sous un port muet ferait
              chercher ce qui s'est cassé, là où son absence dit que ce port-là n'a pas de
              balance. Désarmé tant que la configuration n'est pas arrivée, comme les
              champs des volets : `draft.set` jette en silence dans un document absent.
            -->
            {#if recognised(verdict)}
              <button
                type="button"
                class="pick use"
                disabled={!configRead}
                onclick={() => useScale(verdict)}
              >
                Utiliser cette balance
              </button>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    <!--
      Les réglages, repliés — et DÉSARMÉS tant que la configuration n'est pas arrivée :
      `draft.set` jette en silence ce qu'on écrit dans un document qui n'existe pas encore.

      `bind:` n'est pas un raffinement de style, c'est ce qui empêche le volet de se
      refermer sous le doigt : `open={uneExpression}` compile en écriture de propriété DOM
      non mémoïsée, logée dans l'effet de gabarit du fragment, que le remplacement du
      tableau de bord rejoue toutes les trois secondes. Lié, l'état vit dans son propre
      effet, et l'événement `toggle` y réinjecte ce que le doigt a fait. Le banc
      `web/test/details-open.test.ts` refuse maintenant l'autre écriture partout dans `src`.
    -->
    <details class="folded" bind:open={scaleSettingsOpen}>
      <summary>Réglages série de la balance</summary>
      <div class="folded-body">
        <Field
          label="Protocole"
          path="scale.type"
          value={draft.text('scale.type')}
          hint="Les valeurs acceptées apparaissent ici si l’enregistrement est refusé."
          fault={faultOf(draft, 'scale.type')}
          allowed={allowedFor(draft, 'scale.type')}
          disabled={!configRead}
          onchange={(value) => draft.set('scale.type', value)}
        />
        <Field
          label="Port série"
          path="scale.options.port"
          value={draft.text('scale.options.port')}
          hint="Choisissez-le dans la liste détectée ci-dessus plutôt que de le taper : l’écoute permanente ne suit que des ports détectés."
          fault={faultOf(draft, 'scale.options.port')}
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
      <p class="frames-head" data-caption>{caption}</p>
      {#if halt !== '' || ask !== ''}
        <p class="interrupted" data-interrupted>
          {halt}
          {#if ask !== ''}
            <Act
              act="listen"
              label={ask}
              protected
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
    <StandingHeader title="Imprimante" name="printer" standing={printerStanding} />
    <p class="fact">{printerStanding.detail}</p>
    <p class="note">{printerObservation(printer)}</p>

    <div class="actions">
      <!--
        `disabled` carries « an act is running somewhere on the page », `busy` carries
        « it is THIS one »: the first disarms them all, the second is the only one left
        fully legible.
      -->
      <Act
        label="Lister les files"
        busy={acting === 'printers'}
        disabled={acting !== ''}
        onrun={() => void act('printers', loadPrinters)}
      />
      <Act
        label="Rechercher l’imprimante"
        protected
        busy={acting === 'discover'}
        disabled={acting !== ''}
        onrun={() => void act('discover', discover)}
      />
      <!--
        The self-tests THIS driver honours, side by side: the label keeps WHICH ONE is
        working. Reduced all of them to « En cours… », nothing on screen would say any
        more which one is putting out a label.
      -->
      {#each offered as test (test.what)}
        <Act
          act={test.what}
          label={`Auto-test : ${test.name}${acting === test.what ? ' — en cours…' : ''}`}
          protected
          disabled={acting !== ''}
          onrun={() => void act(test.what, () => selfTest(test.what))}
        />
      {/each}
    </div>

    <!--
      Un bouton qui manque doit se lire comme une déclaration et non comme une panne. Sans
      cette ligne, un exploitant qui connaît les trois auto-tests de §8.6 et n'en voit
      qu'un cherche ce qui s'est cassé — ce qui est exactement le temps que la déclaration
      fait gagner.
    -->
    {#if offered.length < SELF_TESTS.length}
      <p class="note" data-self-tests-note>
        {offered.length === 0
          ? 'Le driver d’impression en service n’imprime aucun auto-test.'
          : 'Les autres auto-tests ne sont pas proposés : le driver d’impression en service ne les imprime pas.'}
      </p>
    {/if}

    {#if reachable.length > 0}
      <p class="count" data-printers-count>
        {census('destination', 'destinations', reachable.length, ROWS_SHOWN)}
      </p>
      <ul class="list">
        {#each reachable.slice(0, ROWS_SHOWN) as device (device.name)}
          <li>
            <!--
              Le clic écrit dans la clé que la DESTINATION déclare, jamais dans une clé que
              l'écran aurait choisie : les deux routes servent la même liste ici, et
              « 192.168.0.43:9100 » ne ressemble pas moins à un nom de file que
              « SATO WS408_2 ».
            -->
            <button type="button" class="pick" onclick={() => draft.set(devicePath, device.name)}>
              {device.name}
            </button>
            <span class="detail">
              {device.detail}{device.default ? ' — file par défaut du système' : ''}
            </span>
          </li>
        {/each}
      </ul>
    {/if}

    <!--
      Ce que le transport choisi ne peut pas ouvrir se dit, au lieu de disparaître. Une
      liste qui rétrécit en silence après « Rechercher l'imprimante » se lit comme une
      recherche qui n'a rien trouvé.
    -->
    {#if unreachable > 0}
      <p class="note" data-unreachable>{reachElsewhere(printers, transports, deviceKey)}</p>
    {/if}

    <!-- Même règle que le volet de la balance : l'ouverture est LIÉE, jamais poussée. -->
    <details class="folded" bind:open={printerSettingsOpen}>
      <summary>Réglages de l’imprimante</summary>
      <div class="folded-body">
        <Field
          label="Driver"
          path="printer.type"
          value={draft.text('printer.type')}
          hint="Gardez le driver raster : c’est celui que les postes en service utilisent."
          fault={faultOf(draft, 'printer.type')}
          allowed={allowedFor(draft, 'printer.type')}
          disabled={!configRead}
          onchange={(value) => draft.set('printer.type', value)}
        />
        <!--
          Le transport se CHOISIT, et le champ d'en dessous suit. C'était un champ de texte
          libre au-dessus d'un champ câblé sur `printer.options.queue` quoi qu'on tape : un
          poste réglé sur `tcp` enregistrait l'adresse de son imprimante dans la clé de la
          file Windows. Rien ne le refusait — aucun contrôle ne lie une clé à un transport —
          et le poste n'imprimait pas.

          La liste est vide sur un binaire qui ne déclare aucun transport ; `Field` retombe
          alors sur la saisie libre, ce qui vaut mieux qu'une liste sans une valeur dedans.
        -->
        <Field
          label="Transport"
          path="printer.options.transport"
          value={transport}
          hint="Local par défaut : une file Windows ou un nœud d’impression de ce poste."
          fault={faultOf(draft, 'printer.options.transport')}
          allowed={allowedFor(draft, 'printer.options.transport')}
          {choices}
          disabled={!configRead}
          onchange={(value) => draft.set('printer.options.transport', value)}
        />
        <!--
          UN champ, dont la clé est celle que le transport choisi lit. Les deux autres ne
          sont pas effacées pour autant : revenir à un transport déjà réglé ne doit pas
          coûter la saisie, et une clé qu'aucun transport ne lit est légitimement vide
          (§8.4).
        -->
        <Field
          label={labelOf(devicePath)}
          path={devicePath}
          value={draft.text(devicePath)}
          hint={DEVICE_HINTS[deviceKey] ?? ''}
          fault={faultOf(draft, devicePath)}
          allowed={suggestions(
            devicePath,
            reachable.map((device) => device.name),
          )}
          disabled={!configRead}
          onchange={(value) => draft.set(devicePath, value)}
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
    /* `:not(:disabled)` comme dans `app.css` : une commande désarmée qui s'éclaire sous la
       souris se lit comme une commande qu'un clic ferait marcher. */
    .pick:hover:not(:disabled),
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

  /*
   * Le geste qui met une balance reconnue en service : il écrit dans le brouillon comme
   * les noms de port juste au-dessus, mais il porte une PHRASE et non une valeur — le
   * soulignement qui fait lire « COM8 » comme une chose à choisir ferait lire celle-ci
   * comme un lien.
   */
  .use {
    text-decoration: none;
    background: var(--surface);
    border: 1px solid var(--border);
  }

  .use:disabled {
    opacity: 0.5;
    cursor: default;
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
   * Le visualiseur de trames : borné et défilant.
   *
   * Sa sélection ne se déclare plus ici : `app.css` la rend à toute l'administration, qui
   * est un document quand l'écran client n'en est pas un. Ces lignes-là sont ce qu'un
   * support demande au téléphone, et elles ont été la première à en avoir besoin.
   */
  .frames {
    margin-top: 1rem;
    max-height: 18rem;
    overflow-y: auto;
    background: var(--waiting-wash);
    border: 1px solid var(--border);
    border-radius: var(--radius);
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
