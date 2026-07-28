import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * La famille d'un bouton, et la pastille « clé », partout où un acte est protégé.
 *
 * `admin-troubleshooting.test.ts` tient déjà cette garantie sur les neuf gros boutons du
 * Dépannage. Elle manquait partout ailleurs, et quatre choses en avaient profité :
 *
 *  1. « Importer un fichier » du poste était PEINT EN ROUGE alors que sa route valide et
 *     n'applique rien, pendant que « Recopier », qui écrit vraiment dans le brouillon,
 *     était en bleu — les deux à l'envers l'un de l'autre. Un rouge non mérité use celui
 *     qui l'est, et cette page en porte un vrai sur la remise en service d'une version ;
 *  2. six boutons de la page Matériel demandaient le mot de passe SANS LE DIRE avant le
 *     clic, alors que les mêmes auto-tests le disaient sur la page Étiquette ;
 *  3. la mire d'alignement est neutre, contre la table du plan — décision prise et écrite
 *     dans le code, table du plan corrigée ;
 *  4. la zone de dépôt du catalogue portait la pastille sur la page Catalogue et PAS sur
 *     le Dépannage, alors que c'est le même acte et la même route gardée. Ce fichier est
 *     ce qui empêche les quatre de revenir au prochain passage.
 *
 * Le contrôle se fait sur le TEXTE SOURCE, comme celui du Dépannage : ce qui est vérifié
 * est la déclaration, et une déclaration absente est précisément le défaut.
 */

const ADMIN = resolve(dirname(fileURLToPath(import.meta.url)), '../src/admin')

const station = readFileSync(resolve(ADMIN, 'pages/Station.svelte'), 'utf8')
const hardware = readFileSync(resolve(ADMIN, 'pages/Hardware.svelte'), 'utf8')
const label = readFileSync(resolve(ADMIN, 'pages/Label.svelte'), 'utf8')
const catalog = readFileSync(resolve(ADMIN, 'pages/Catalog.svelte'), 'utf8')
const troubleshooting = readFileSync(resolve(ADMIN, 'pages/Troubleshooting.svelte'), 'utf8')

/** La pastille, telle que les trois pages qui la portent l'écrivent, au caractère près. */
const KEY_BADGE = '<span class="key" title="Demande le mot de passe">clé</span>'

/**
 * Le `<Act …/>` de cette page dont le balisage porte ce fragment.
 *
 * @param page - le texte source de la page.
 * @param fragment - de quoi le reconnaître : son libellé, ou son `act`.
 */
function actSaying(page: string, fragment: string): string {
  const found = [...page.matchAll(/<Act\b[\s\S]*?\/>/gu)]
    .map((match) => match[0])
    .find((markup) => markup.includes(fragment))
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} »`)
  return found
}

/** La famille que ce balisage déclare — « read » quand il n'en déclare aucune. */
function kindOf(markup: string): string {
  return /kind="(\w+)"/u.exec(markup)?.[1] ?? 'read'
}

/** Vrai quand ce balisage annonce la clé AVANT le clic. */
function guards(markup: string): boolean {
  return /\bprotected\b/u.test(markup)
}

describe('la zone d’import du poste ne peint pas en rouge ce qui n’applique rien', () => {
  it('n’emploie plus le jeton irréversible, nulle part sur la page', () => {
    // `POST /admin/api/config/import` valide et rend le diff : « it VALIDATES and returns
    // the diff, and applies nothing » (internal/web/config.go). Ce que la page dit quatre
    // lignes plus bas — « Recopier n'applique rien » — le disait déjà.
    expect(station).not.toContain('var(--danger)')
  })

  it('prend les jetons de la famille neutre, et sa densité de 44 px', () => {
    const chooser = /\.choose\s*\{[^}]*\}/u.exec(station)?.[0] ?? ''
    expect(chooser).toContain('background: var(--surface)')
    expect(chooser).toContain('color: var(--ink)')
    expect(chooser).toContain('min-height: 2.75rem')
    // Les 72 px appartiennent à la famille irréversible : dans `Act`, la famille porte la
    // hauteur autant que la couleur.
    expect(station).not.toMatch(/class="choose[^"]*touch-target/u)
  })

  it('garde la pastille « clé » : la route, elle, est bien protégée', () => {
    expect(station).toContain(KEY_BADGE)
  })

  it('laisse le bleu à ce qui écrit et le rouge à ce qui ne se défait pas', () => {
    // « Recopier » écrit dans le BROUILLON ; remettre une version en service change le
    // poste sur-le-champ et perd ce qui n'a pas été enregistré.
    expect(kindOf(actSaying(station, 'adoptLabel'))).toBe('write')
    expect(kindOf(actSaying(station, 'Remettre cette version en service'))).toBe('destructive')
    // Exporter ne fait que lire, quand bien même le poste demande le mot de passe.
    for (const exported of ['Exporter tout', 'Exporter sans le matériel']) {
      expect(kindOf(actSaying(station, exported)), exported).toBe('read')
      expect(guards(actSaying(station, exported)), exported).toBe(true)
    }
  })
})

describe('la page Matériel dit AVANT le clic ce qui demandera le mot de passe', () => {
  /**
   * Les six boutons dont l'acte traverse `admin.protect`, et la route gardée de chacun
   * (table `guarded` de `internal/web/server.go`). Les trois auto-tests sortent d'une même
   * boucle `{#each}`, donc d'un seul balisage.
   */
  it.each([
    ['act="detect"', 'POST /admin/api/scale/detect'],
    ['act="listen"', 'POST /admin/api/scale/capture'],
    ['Rechercher l’imprimante', 'POST /admin/api/printers/discover'],
    ['act={test.what}', 'POST /admin/api/printer/test'],
  ])('« %s » porte la clé — %s', (fragment) => {
    expect(guards(actSaying(hardware, fragment))).toBe(true)
  })

  it.each([
    ['Lister les ports', 'GET /admin/api/ports'],
    ['Lister les files', 'GET /admin/api/printers'],
  ])('« %s » ne la porte pas : sa route est ouverte — %s', (fragment) => {
    expect(guards(actSaying(hardware, fragment))).toBe(false)
  })

  it('fait passer par admin.protect exactement les actes qui portent la clé', () => {
    // La pastille est une PROMESSE : elle ne vaut que si l'acte la tient. Chacun de ces
    // quatre gestionnaires appelle `admin.protect`, qui ouvre le panneau et rejoue l'appel.
    for (const guarded of [
      'admin.protect(api.discoverPrinters)',
      'admin.protect(() => api.printerSelfTest(what))',
      'admin.protect(() => scan(list))',
      'admin.protect(() => api.captureFrames(listenOn, LISTEN_SECONDS))',
    ]) {
      expect(hardware, guarded).toContain(guarded)
    }
    // Et les deux énumérations passent par `admin.load`, qui ne demande rien.
    expect(hardware).toContain('admin.load(api.fetchPorts)')
    expect(hardware).toContain('admin.load(api.fetchPrinters)')
  })

  it('n’a aucune famille pleine : rien ici ne change ce que le poste vend', () => {
    expect(hardware).not.toContain('kind="write"')
    expect(hardware).not.toContain('kind="destructive"')
  })
})

describe('les auto-tests d’impression ont UNE couleur, quelle que soit la page', () => {
  it('garde la mire d’alignement neutre, et protégée', () => {
    // Décision prise contre la table du plan, qui la donnait « destructive » : imprimer
    // coûte une étiquette, mais ne laisse pas le poste dans un état d'où l'on ne revient
    // pas. La raison est écrite dans `Label.svelte`, et le plan a été aligné dessus.
    const test = actSaying(label, 'act={test.what}')
    expect(kindOf(test)).toBe('read')
    expect(guards(test)).toBe(true)
    expect(label).toContain('Imprimer la mire d’alignement')
  })

  it('donne la même famille aux auto-tests de la page Matériel', () => {
    // Un même acte — `POST /admin/api/printer/test` — ne peut pas porter deux couleurs
    // selon l'écran par lequel on l'atteint.
    expect(kindOf(actSaying(hardware, 'act={test.what}'))).toBe(
      kindOf(actSaying(label, 'act={test.what}')),
    )
  })

  it('dit ce que l’impression coûte, en toutes lettres', () => {
    // Ce que la couleur ne porte pas, la phrase le porte.
    expect(label).toContain('Chaque appui sort une étiquette pour de bon')
  })
})

/**
 * La zone de dépôt d'un CSV est le MÊME acte sur les deux pages qui la portent.
 *
 * Elle n'est pas un `<Act>` et ne peut pas le devenir — c'est un `<label>` habillant un
 * `<input type="file">`, et en faire un bouton casserait le sélecteur de fichier —, donc
 * rien de ce que le composant garantit ne s'applique à elle : chaque page écrit la
 * pastille à la main, ou l'oublie. Le Dépannage l'avait oubliée, sur un balisage par
 * ailleurs identique à celui de la page Catalogue.
 */
describe('la zone de dépôt du catalogue annonce la clé sur les DEUX pages', () => {
  it.each([
    ['la page Catalogue', catalog],
    ['le Dépannage', troubleshooting],
  ])('%s porte la pastille, écrite pareil', (_where, page) => {
    expect(page).toContain(KEY_BADGE)
  })

  it.each([
    ['la page Catalogue', catalog],
    ['le Dépannage', troubleshooting],
  ])('%s tient la promesse : l’acte passe par admin.protect', (_where, page) => {
    // La pastille est une PROMESSE. `POST /admin/api/catalog/import` est dans la table
    // `guarded` de `internal/web/server.go` ; l'annoncer sur un écran et pas sur l'autre
    // laissait croire, depuis le Dépannage, que le dépôt ne demandait rien.
    expect(page).toContain('admin.protect(() => api.importCatalog(file))')
  })
})
