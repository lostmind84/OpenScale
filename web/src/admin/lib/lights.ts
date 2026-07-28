import type { HealthDTO } from './dto'
import { labelOf } from './fields'
import { frenchBytes, frenchDuration, frenchInteger } from './format'

/**
 * Les six feux du tableau de bord (§14.4) — et la règle qui les rend utiles.
 *
 * **Un feu qui n'est pas vert dit QUOI FAIRE.** « Imprimante en panne » n'apprend rien à
 * un bénévole debout devant un poste muet ; « regardez le capot et le rouleau, puis
 * touchez Tester l'imprimante » lui donne le geste suivant. Un test parcourt tous les
 * scénarios et refuse un verdict non vert sans consigne — la même règle que
 * `diag.Report.Validate()` applique aux quinze contrôles de `openscale doctor`, pour la
 * même raison : un échec sans remède apprend à ignorer l'écran.
 *
 * Les cinq niveaux distinguent « je ne peux pas le savoir d'ici » de « en panne », comme
 * `diag.Status` : un transport d'impression à sens unique qui ne dit rien n'est pas une
 * imprimante en panne (ADR-007), et le peindre en rouge laisserait tous les postes en
 * configuration par défaut rouges en permanence.
 */

/** Ce qu'un feu vaut. `off` est un feu ÉTEINT : le poste n'a pas cet organe. */
export type LightLevel = 'ok' | 'warn' | 'fault' | 'unknown' | 'off'

/** Les six organes surveillés, dans l'ordre où §14.4 les énumère. */
export type LightID = 'scale' | 'printer' | 'roll' | 'catalog' | 'disk' | 'journal'

/** Un feu, tel que la page le dessine. */
export interface Light {
  id: LightID
  /** Le mot du feu : « Balance », « Imprimante », « Rouleau »… */
  label: string
  level: LightLevel
  /** Ce qui est mesuré, en français, sans jargon et sans code. */
  value: string
  /** Le geste suivant. JAMAIS vide quand le niveau n'est ni `ok` ni `off`. */
  remedy: string
}

/**
 * Compose les six feux à partir du tableau de bord servi par le poste.
 *
 * @param health - la charge de `GET /admin/api/health`.
 * @returns six feux, toujours six, dans l'ordre de §14.4.
 */
export function lightsOf(health: HealthDTO): Light[] {
  return [
    scaleLight(health),
    printerLight(health),
    rollLight(health),
    catalogLight(health),
    diskLight(health),
    journalLight(health),
  ]
}

/**
 * La balance. Un poste déclaré sans balance ÉTEINT le feu au lieu de le laisser rouge :
 * c'est tout l'intérêt de la déclaration `scale.present` (§11.2, §14.4).
 */
function scaleLight(health: HealthDTO): Light {
  const scale = health.state.scale
  const cadence = frenchDuration(scale.median_ms)
  if (!health.scale_present) {
    return {
      id: 'scale',
      label: 'Balance',
      level: 'off',
      value: 'ce poste est déclaré sans balance',
      remedy: '',
    }
  }
  if (!scale.connected) {
    return {
      id: 'scale',
      label: 'Balance',
      level: 'fault',
      value: 'elle ne répond plus',
      remedy:
        'Vérifiez le câble et l’alimentation de la balance, puis touchez « Tester la ' +
        'balance ». En attendant, « Basculer en saisie manuelle » permet de continuer à servir.',
    }
  }
  if (scale.too_slow) {
    return {
      id: 'scale',
      label: 'Balance',
      level: 'warn',
      value: `une mesure toutes les ${cadence}, plus lent que la péremption`,
      remedy:
        'À cette cadence, un poids serait déclaré périmé avant l’arrivée de la mesure ' +
        'suivante. Vérifiez le câble et l’adaptateur USB, puis la cadence sur la ' +
        'page Matériel.',
    }
  }
  return {
    id: 'scale',
    label: 'Balance',
    level: 'ok',
    value: provisional(scale.provisional, `une mesure toutes les ${cadence}`),
    remedy: '',
  }
}

/** L'imprimante, telle que le superviseur l'a vue en dernier (§13.1, goroutine 6). */
function printerLight(health: HealthDTO): Light {
  const printer = health.state.printer
  switch (printer.health) {
    case 'faulted':
      return {
        id: 'printer',
        label: 'Imprimante',
        level: 'fault',
        value: printer.detail === '' ? 'elle ne peut pas imprimer' : printer.detail,
        remedy: printerFaultRemedy(health),
      }
    case 'consumable':
      return {
        id: 'printer',
        label: 'Imprimante',
        level: 'warn',
        value: 'elle imprime, mais le rouleau arrive en fin de vie',
        remedy:
          'Changez le rouleau quand vous passez derrière le comptoir, puis touchez ' +
          '« J’ai changé le rouleau ». Le poste continue de servir en attendant.',
      }
    case 'ready':
      return {
        id: 'printer',
        label: 'Imprimante',
        level: 'ok',
        value: 'elle répond et n’a rien à signaler',
        remedy: '',
      }
    default:
      return {
        id: 'printer',
        label: 'Imprimante',
        level: 'unknown',
        value: 'elle prend les étiquettes et ne dit rien en retour',
        remedy:
          'C’est la réponse normale d’une file Windows en RAW ou d’un fichier de ' +
          'périphérique, pas une panne. Pour savoir si elle imprime, touchez ' +
          '« Imprimer une étiquette de test ».',
      }
  }
}

/** La consigne d'une imprimante en panne, qui nomme le secours QUAND il existe. */
function printerFaultRemedy(health: HealthDTO): string {
  const base =
    'Regardez le capot, le rouleau et le câble, puis touchez « Tester l’imprimante ».'
  if (health.printing?.fallback_available === true) {
    return (
      base +
      ' Le poste peut servir en attendant : « Imprimer sur l’imprimante du poste voisin ».'
    )
  }
  return base
}

/**
 * Le rouleau. Le compteur est FAIT pour être faux — rien sur une imprimante thermique ne
 * dit qu'on a changé le papier — donc le feu répète la phrase du compteur et ne recalcule
 * aucun seuil : c'est `printing.RollCounter` qui sait quand « environ 100 restantes »
 * devient « probablement fini » (§8.5).
 */
function rollLight(health: HealthDTO): Light {
  const roll = health.roll
  if (roll === null) {
    return {
      id: 'roll',
      label: 'Rouleau',
      level: 'unknown',
      value: 'aucun compteur d’étiquettes sur ce poste',
      remedy:
        'Sans imprimante construite, il n’y a pas de rouleau à compter : vérifiez ' +
        `le « ${labelOf('printer.type')} » et ses réglages sur la page Matériel.`,
    }
  }
  if (!roll.known) {
    return {
      id: 'roll',
      label: 'Rouleau',
      level: 'unknown',
      value: roll.message,
      remedy:
        'Touchez « J’ai changé le rouleau » en mettant un rouleau neuf : c’est le seul ' +
        'geste qui dise quelque chose de vrai du papier.',
    }
  }
  if (roll.level === 'warn') {
    return {
      id: 'roll',
      label: 'Rouleau',
      level: 'warn',
      value: roll.message,
      remedy: 'Changez le rouleau, puis touchez « J’ai changé le rouleau ».',
    }
  }
  return { id: 'roll', label: 'Rouleau', level: 'ok', value: roll.message, remedy: '' }
}

/**
 * Le catalogue. Un catalogue vide est un poste qui ne peut rien vendre : la consigne
 * NOMME le fichier attendu et le répertoire surveillé, comme l'écran client le fait
 * (§14.3, §14.4) — « déposez le fichier » est inapplicable sans le chemin.
 */
function catalogLight(health: HealthDTO): Light {
  const watched = health.catalog_source?.label ?? ''
  if (health.state.catalog_count === 0) {
    return {
      id: 'catalog',
      label: 'Catalogue',
      level: 'fault',
      value: 'aucun produit dans la grille',
      remedy: emptyCatalogRemedy(watched),
    }
  }
  const last = health.catalog
  if (last !== null && last.result !== 'applied' && last.result !== 'unchanged') {
    return {
      id: 'catalog',
      label: 'Catalogue',
      level: 'warn',
      value: `dernier fichier refusé : ${last.reason === '' ? last.result : last.reason}`,
      remedy:
        'La grille tourne toujours sur le catalogue précédent. Corrigez le fichier dans ' +
        'Odoo, ou touchez « Oublier la quarantaine » puis redéposez-le.',
    }
  }
  return {
    id: 'catalog',
    label: 'Catalogue',
    level: 'ok',
    value: `${frenchInteger(health.state.catalog_count)} produits dans la grille`,
    remedy: '',
  }
}

/** La consigne d'un catalogue vide, avec le chemin surveillé en clair quand on l'a. */
function emptyCatalogRemedy(watched: string): string {
  const where = watched === '' ? '' : ` Ce poste surveille : ${watched}.`
  return (
    'Déposez le fichier du catalogue là où le poste le guette, ou utilisez « Importer un ' +
    'catalogue » ci-contre pour le glisser depuis une clé USB.' +
    where
  )
}

/**
 * Le disque. Zéro octet libre est rouge, sous le seuil déclaré est orange — la même règle
 * que le 5ᵉ contrôle de `openscale doctor` (§15.4), et le seuil s'affiche À CÔTÉ de la
 * mesure pour qu'un réglage sans rapport avec la réalité se voie du premier coup d'œil
 * (§10.4).
 */
function diskLight(health: HealthDTO): Light {
  const disk = health.disk
  if (disk === null) {
    return {
      id: 'disk',
      label: 'Disque',
      level: 'unknown',
      value: 'la place libre n’a pas pu être mesurée',
      remedy:
        'Téléchargez le fichier de diagnostic : il porte ce que le poste a pu lire du ' +
        'volume, et c’est la pièce qu’un support demandera.',
    }
  }
  const free = frenchBytes(disk.free_bytes)
  const threshold = `seuil ${frenchInteger(disk.alert_mb)} Mo`
  if (disk.free_bytes === 0) {
    return {
      id: 'disk',
      label: 'Disque',
      level: 'fault',
      value: `plus un octet libre sur ${disk.path}`,
      remedy:
        'Le journal ne peut plus rien écrire, alors que les étiquettes continuent de ' +
        'sortir. Faites de la place sur le disque, puis rechargez cette page.',
    }
  }
  if (disk.alert_mb > 0 && disk.free_bytes < disk.alert_mb * 1_000_000) {
    return {
      id: 'disk',
      label: 'Disque',
      level: 'warn',
      value: `${free} libres (${threshold})`,
      remedy:
        'Faites de la place avant que le journal ne puisse plus écrire : archives de ' +
        'catalogues, anciennes étiquettes, corbeille du poste.',
    }
  }
  return {
    id: 'disk',
    label: 'Disque',
    level: 'ok',
    value: `${free} libres (${threshold})`,
    remedy: '',
  }
}

/**
 * Le journal. Le compteur de pesées non journalisées est le SEUL feu rouge d'ADR-013 : le
 * journal se dégrade, le service jamais — donc l'étiquette est sortie, la vente a eu lieu,
 * et c'est la trace qui manque.
 */
function journalLight(health: HealthDTO): Light {
  const unlogged = health.counters.unlogged_weighings_count
  if (unlogged > 0) {
    return {
      id: 'journal',
      label: 'Journal',
      level: 'fault',
      value: `${frenchInteger(unlogged)} pesées imprimées mais non enregistrées`,
      remedy:
        'Les étiquettes sortent, les ventes sont bonnes, c’est la trace qui manque. ' +
        'Téléchargez le fichier de diagnostic et prévenez le support : ne redémarrez rien.',
    }
  }
  if (health.counters.journal_rows_count < 0) {
    return {
      id: 'journal',
      label: 'Journal',
      level: 'unknown',
      value: 'ce poste n’a pas de journal ouvert',
      remedy:
        'Le poste pèse et imprime quand même. Téléchargez le fichier de ' +
        'diagnostic : il dit pourquoi la base n’a pas pu être ouverte.',
    }
  }
  return {
    id: 'journal',
    label: 'Journal',
    level: 'ok',
    value: `${frenchInteger(health.counters.journal_rows_count)} pesées enregistrées`,
    remedy: '',
  }
}

/** Ajoute la mention « cadence provisoire » tant que huit intervalles ne sont pas vus. */
function provisional(isProvisional: boolean, sentence: string): string {
  return isProvisional ? sentence + ' (cadence encore provisoire)' : sentence
}
