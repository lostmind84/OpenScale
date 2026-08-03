import { frenchInteger } from './format'

/**
 * What the Matériel page says about a serial port it is holding — or not holding.
 *
 * Everything here exists because under Windows a serial port is EXCLUSIVE and the service
 * exposes no stream of frames but a bounded capture that HOLDS the port for three
 * seconds. The permanent listening, the scan, and the one button that appears when
 * neither can run are three readings of that same fact, so they are worded in one place:
 * two of them saying different things about the same port is how a screen sends somebody
 * looking for a fault that is not there.
 *
 * Nothing is asserted that has not been checked — « je ne sais pas encore » while the
 * configuration has not arrived — which is why every sentence takes the whole state
 * rather than guessing at part of it.
 */

/** Everything the sentences of the frame viewer are drawn from. */
export interface Listening {
  /** Whether the configuration has arrived. Before that, nothing is known. */
  configRead: boolean
  /** Whether the station is declared with no scale at all. */
  declaredWithoutScale: boolean
  /** The port the configuration names, empty when it names none. */
  port: string
  /** Whether the port enumeration has answered, even with zero port. */
  listed: boolean
  /** Whether that port is one the station REALLY enumerated. */
  portKnown: boolean
  /** What stopped the listening on that port, empty while it runs. */
  halt: string
  /** The act in flight, or an empty string. */
  acting: string
  /** How many frames the viewer is showing. */
  framesShown: number
  /** How many it keeps at most. */
  framesKept: number
}

/** Where the scan has got to. */
export interface Scan {
  /** The act in flight, or an empty string. */
  acting: string
  /** True while a listening round is in flight: the port is HELD. */
  listening: boolean
  /** How many ports the scan has opened, and how many it has to open. */
  scanned: number
  toScan: number
}

/**
 * La légende du visualiseur : ce qui est écouté, puis ce qui a été entendu.
 *
 * @param state - ce que la page sait du port et de ce qu'elle en a reçu.
 */
export function framesCaption(state: Listening): string {
  const heard = captionOfHeard(state)
  return heard === '' ? captionOfListening(state) : `${captionOfListening(state)} ${heard}`
}

/**
 * Ce que la page fait du port, en une phrase — et « je ne sais pas encore » quand c'est
 * la vérité.
 *
 * Tant que la configuration n'est pas arrivée, le port vaut la chaîne vide parce que la
 * clé est ABSENTE, et non parce que personne ne l'a renseignée. La page affirmait
 * « Aucun port n'est indiqué » à trois centimètres de « cette page ne déclare rien de ce
 * poste ».
 *
 * @param state - ce que la page sait du port.
 */
function captionOfListening(state: Listening): string {
  if (!state.configRead) {
    return 'Lecture de la configuration en cours : le port à écouter n’est pas encore connu.'
  }
  if (state.declaredWithoutScale) {
    return 'Ce poste est déclaré sans balance : aucun port n’est écouté.'
  }
  if (state.port === '') {
    return 'Aucun port n’est indiqué : choisissez-en un dans la liste ci-dessus pour écouter les trames.'
  }
  if (!state.listed) {
    return state.acting === 'ports'
      ? `Énumération des ports en cours : l’écoute de ${state.port} démarre dès qu’il est vu.`
      : `Les ports de ce poste n’ont pas été énumérés : « Lister les ports » dira si ${state.port} existe.`
  }
  if (!state.portKnown) {
    return `${state.port} n’est pas visible depuis ce poste : rien n’est écouté en continu.`
  }
  if (state.halt !== '') return `L’écoute de ${state.port} est arrêtée.`
  if (state.acting !== '') {
    return `L’écoute de ${state.port} est suspendue le temps de l’acte en cours.`
  }
  return `Écoute de ${state.port}.`
}

/**
 * Ce que le visualiseur montre, accordé à ce qu'il y a vraiment dedans.
 *
 * @param state - combien de trames sont affichées, et combien sont gardées au plus.
 */
function captionOfHeard(state: Listening): string {
  if (state.framesShown === 0) return 'Aucune trame reçue pour l’instant.'
  // Une balance qui n'émet qu'au posé de sac rend UNE trame par manche : « les 1
  // dernières trames » est le cas normal de la mise en service, pas un cas limite.
  if (state.framesShown === 1) {
    return `Une seule trame reçue — ${frenchInteger(state.framesKept)} au plus sont gardées.`
  }
  return (
    `Les ${frenchInteger(state.framesShown)} dernières trames — ` +
    `${frenchInteger(state.framesKept)} au plus, la plus récente en bas.`
  )
}

/**
 * Ce que le bouton de capture propose, ou rien quand la boucle tourne toute seule.
 *
 * Deux situations le font apparaître, et une seule phrase ne couvre pas les deux : le
 * poste a REFUSÉ la dernière manche — il faut insister, avec le mot de passe s'il le
 * faut — ou le port n'est pas énuméré, et l'écoute permanente ne s'en saisira jamais.
 *
 * @param state - ce que la page sait du port.
 */
export function askLabel(state: Listening): string {
  if (!state.configRead || state.declaredWithoutScale || state.port === '') return ''
  if (state.halt !== '') return 'Reprendre l’écoute'
  if (state.listed && !state.portKnown) return 'Écouter ce port une fois'
  return ''
}

/**
 * Ce que dit le bouton du balayage, et où il en est (« port 2 sur 5 »).
 *
 * It is the one act of the page whose label SAYS HOW FAR IT HAS GOT: « port 2 sur 5 » on
 * a scan that runs for a minute is worth more than « En cours… ».
 *
 * @param scan - où en est le balayage.
 */
export function detectLabel(scan: Scan): string {
  if (scan.acting !== 'detect') return 'Détecter automatiquement'
  if (scan.listening) return 'Détection : le port se libère…'
  if (scan.toScan === 0) return 'Détection : énumération des ports…'
  return `Détection : port ${frenchInteger(scan.scanned)} sur ${frenchInteger(scan.toScan)}…`
}
