import type { PrinterDTO, ScaleDTO } from '../../lib/dto'
import { frenchDateTime, frenchDuration, frenchInteger } from './format'
import type { LightLevel } from './lights'

/**
 * What the station OBSERVES of a piece of hardware, in one word and one sentence.
 *
 * The rule of the Matériel page in one line: this comes from what the station sees, never
 * from what somebody declared about it. And it is always FRENCH — `printer.health` is one
 * of four English tokens, which the page used to show a volunteer as they came.
 */
export interface Standing {
  level: LightLevel
  /** Le mot français que lit un bénévole. Jamais un jeton du service. */
  word: string
  detail: string
}

/**
 * L'en-tête d'état de la balance : ce que le poste OBSERVE, jamais ce qu'on déclare.
 *
 * @param present - le poste déclare-t-il une balance ?
 * @param scale - ce que le poste sait de sa balance sans la lui demander.
 */
export function standingOfScale(present: boolean, scale: ScaleDTO): Standing {
  if (!present) {
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
        cadenceOf(scale) +
        ' À cette cadence, un poids serait déclaré périmé avant l’arrivée de la mesure suivante.',
    }
  }
  return { level: 'ok', word: 'Connectée', detail: 'Elle répond. ' + cadenceOf(scale) }
}

/**
 * La cadence OBSERVÉE, et rien quand aucun intervalle n'a encore été mesuré.
 *
 * @param scale - ce que le poste sait de sa balance.
 */
export function cadenceOf(scale: ScaleDTO): string {
  if (scale.observations_count === 0) {
    return 'Aucun intervalle n’a encore été mesuré : la cadence sera connue dès les premières trames.'
  }
  const measured = `Une mesure toutes les ${frenchDuration(scale.median_ms)} sur ${frenchInteger(
    scale.observations_count,
  )} intervalles`
  return measured + (scale.provisional ? ', cadence encore provisoire.' : '.')
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

/**
 * L'en-tête d'état de l'imprimante, EN FRANÇAIS.
 *
 * `printer.health` vaut `ready`, `consumable`, `faulted` ou `unknown` : quatre jetons
 * anglais que la page affichait tels quels à un bénévole. Un jeton que cette table ne
 * connaît pas ne passe pas non plus — il devient « État inconnu », ce qui est la vérité.
 *
 * @param printer - la dernière chose que le superviseur a vue de l'imprimante.
 */
export function standingOfPrinter(printer: PrinterDTO): Standing {
  const said = PRINTER_STANDINGS[printer.health] ?? {
    level: 'unknown' as LightLevel,
    word: 'État inconnu',
    detail: 'Le poste a répondu un état que cet écran ne sait pas nommer.',
  }
  return { ...said, detail: printer.detail === '' ? said.detail : printer.detail }
}

/**
 * Ce que l'imprimante a dit, et QUAND elle l'a dit.
 *
 * @param printer - la dernière chose que le superviseur a vue de l'imprimante.
 */
export function printerObservation(printer: PrinterDTO): string {
  const when =
    printer.observed_at === ''
      ? 'Jamais observée depuis le démarrage'
      : `Observée le ${frenchDateTime(printer.observed_at)}`
  const pending = printer.pending_jobs_count
  return `${when}, ${frenchInteger(pending)} ${pending > 1 ? 'travaux' : 'travail'} en attente.`
}
