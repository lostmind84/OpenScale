import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * Les neuf gros boutons du Dépannage sont-ils toujours GROS ?
 *
 * `tokens.test.ts` garantissait les 72 px de §14.2 sur chaque bouton de `src/**`, y
 * compris ceux de l'administration. ADR-033 l'a restreint à l'écran client : la règle
 * vient d'une contrainte physique — 20 mm sous un doigt — et une page de réglages à 45
 * champs conduite à la souris n'y est pas soumise.
 *
 * Cette page-ci, si. §14.4 la veut faite de « 9 gros boutons », et elle se touche debout
 * devant un poste en panne, parfois avec un sac dans l'autre main. Ce fichier est la
 * garantie qui remplace celle qui a été retirée : sans lui, personne ne remarquerait le
 * jour où ces boutons rétrécissent.
 */

const ADMIN = resolve(dirname(fileURLToPath(import.meta.url)), '../src/admin')

const bigButton = readFileSync(resolve(ADMIN, 'components/BigButton.svelte'), 'utf8')
const page = readFileSync(resolve(ADMIN, 'pages/Troubleshooting.svelte'), 'utf8')

/** Le gros bouton dont le libellé porte ce fragment, du `<BigButton` à son `/>`. */
function buttonSaying(fragment: string): string {
  const found = [...page.matchAll(/<BigButton\b[\s\S]*?\/>/gu)]
    .map((match) => match[0])
    .find((markup) => markup.includes(fragment))
  if (found === undefined) throw new Error(`aucun gros bouton « ${fragment} »`)
  return found
}

/** La famille que ce bouton déclare — « read » quand il n'en déclare aucune. */
function kindOf(markup: string): string {
  return /kind="(\w+)"/u.exec(markup)?.[1] ?? 'read'
}

describe('les gros boutons de §14.4', () => {
  it('déclarent la cible tactile, et une hauteur au-delà', () => {
    expect(bigButton).toContain('touch-target')
    const height = /min-height:\s*([\d.]+)rem/u.exec(bigButton)
    expect(height).not.toBeNull()
    // 6rem = 96 px, au-delà des 72 px de §14.2 : ces boutons se touchent sans lunettes.
    expect(Number(height?.[1]) * 16).toBeGreaterThanOrEqual(72)
  })

  it('sont le SEUL composant de la page à porter des boutons', () => {
    // Un <button> écrit à la main dans la page échapperait à la garantie ci-dessus.
    const handWritten = [...page.matchAll(/<button\b[^>]*>/gsu)].filter(
      (match) => !match[0].includes('touch-target'),
    )
    expect(handWritten.map((m) => m[0])).toEqual([])
  })
})

describe('ce que chaque bouton dit avant et pendant', () => {
  it('donne un état « en cours » à CHACUNE des neuf actions', () => {
    // Trois d'entre elles n'en avaient pas — les deux tests de matériel et l'import —
    // parce qu'elles passent par `admin.load`, qui ne lève pas `busy` contrairement à
    // `admin.run`. On appuyait, le port s'ouvrait pendant trois secondes, rien ne
    // bougeait, et on appuyait de nouveau.
    const wired = [...page.matchAll(/busy=\{working === '(\w+)'\}/gu)].map((m) => m[1])
    for (const action of ['scale', 'printer', 'label', 'reprint', 'reload', 'manual', 'roll']) {
      expect(wired, action).toContain(action)
    }
    expect(page).toContain("working = 'import'")
  })

  it('annonce AVANT le clic les deux actes qui demandent le mot de passe', () => {
    // Un bénévole qui n'a pas le mot de passe doit savoir lesquels lui sont accessibles
    // sans aller chercher quelqu'un (ADR-033).
    expect(page).toContain('protected')
    expect(bigButton).toContain('guarded')
  })

  it('fait passer les deux actes protégés par admin.protect, et eux seuls', () => {
    expect(page).toContain("guarded('manual'")
    expect(page).toContain('admin.protect(() => api.importCatalog(file))')
    // Les sept autres ne demandent rien : ADR-033 protège ce qui change ce que le poste
    // vend ou la façon dont il pèse, et tester une imprimante ne change ni l'un ni l'autre.
    for (const free of ['api.testScale', 'api.testPrinter', 'api.reprintLast', 'api.rollChanged']) {
      expect(page).toContain(free)
    }
  })

  it('annonce un import REFUSÉ comme refusé', () => {
    // « La veille l'appliquera dans la seconde » se lisait sous un import que le service
    // venait d'écarter : le bénévole repartait en croyant son catalogue à jour.
    expect(page).toContain('REFUSÉ')
    expect(page).toContain("record.result === 'rejected'")
  })
})

/**
 * La couleur d'un gros bouton dit CE QU'IL FAIT AU POSTE, et rien d'autre : neutre quand
 * il l'interroge, bleu quand il l'écrit, rouge quand rien ne le défait d'un clic. C'est
 * la seule information qu'un bénévole lit sans légende, et elle ne vaut que si elle est
 * exacte sur les neuf.
 */
describe('la couleur dit la nature de l’acte', () => {
  it.each([
    ['Tester la balance', 'read'],
    ['Tester l’imprimante', 'read'],
    ['Imprimer une étiquette de test', 'read'],
    ['Réimprimer la dernière', 'read'],
    ['Recharger le catalogue', 'write'],
    ['J’ai changé le rouleau', 'write'],
    ['imprimante du poste voisin', 'write'],
    ['Basculer en saisie manuelle', 'destructive'],
  ])('« %s » est un acte « %s »', (fragment, kind) => {
    expect(kindOf(buttonSaying(fragment))).toBe(kind)
  })

  it('donne à BigButton les deux fonds pleins, et une encre qui tient dessus', () => {
    expect(bigButton).toContain('background: var(--action)')
    expect(bigButton).toContain('background: var(--danger)')
    // L'explication passe en encre claire : --ink-muted disparaîtrait dans le fond plein.
    expect(bigButton).toMatch(/\.destructive \.hint[\s\S]*?color:\s*var\(--surface\)/u)
  })

  it('met le rouge sur la zone de dépôt sans en faire un bouton', () => {
    // C'est un `<label>` habillant un `<input type="file">` : en faire un bouton casserait
    // le sélecteur de fichier. Il prend le jeton, pas le composant.
    expect(page).toMatch(/\.choose\s*\{[^}]*background:\s*var\(--danger\)/u)
  })
})
