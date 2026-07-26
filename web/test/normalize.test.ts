import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { normalize } from '../src/lib/normalize'

/**
 * La normalisation du navigateur donne-t-elle EXACTEMENT ce que donne
 * `domain.Normalize` côté Go ?
 *
 * La fixture `web/testdata/normalization.json` est consommée par les deux tests.
 * Si les deux implémentations divergent, l'un des deux rougit (§14.3, §16.1).
 */

/** Forme des paires de la fixture partagée. */
interface Fixture {
  pairs: { input: string; want: string }[]
}

const here = dirname(fileURLToPath(import.meta.url))
const fixture = JSON.parse(
  readFileSync(resolve(here, '../testdata/normalization.json'), 'utf8'),
) as Fixture

describe('normalize — la fixture partagée avec le test Go', () => {
  it('porte les 121 paires annoncées par SUIVI.md', () => {
    expect(fixture.pairs).toHaveLength(121)
  })

  it.each(fixture.pairs)('« $input » → « $want »', ({ input, want }) => {
    expect(normalize(input)).toBe(want)
  })

  // Un tableur ou un poste macOS produit du NFD sans prévenir : les quatre formes
  // doivent donner le même résultat, comme du côté Go.
  it.each(['NFC', 'NFD', 'NFKC', 'NFKD'] as const)('donne le même résultat en %s', (form) => {
    for (const { input, want } of fixture.pairs) {
      expect(normalize(input.normalize(form))).toBe(want)
    }
  })

  it('est idempotente : normalize(normalize(x)) == normalize(x)', () => {
    for (const { input } of fixture.pairs) {
      const once = normalize(input)
      expect(normalize(once)).toBe(once)
    }
  })
})

describe('normalize — les pièges nommés par le document', () => {
  it('replie « ß » par le bas, là où toUpperCase() donnerait « SS »', () => {
    // C'est le piège explicite de §14.3 : 'ß'.toUpperCase() vaut « SS » en
    // JavaScript et « ß » en Go. Le pliage vers le bas n'a aucune expansion.
    expect(normalize('ß')).toBe('ss')
    expect('ß'.toUpperCase()).toBe('SS')
  })

  it('cherche la ligature « Œ » par « OE », qu’Unicode ne décompose pas', () => {
    expect('Œ'.normalize('NFD')).toBe('Œ')
    expect('Œ'.normalize('NFKD')).toBe('Œ')
    expect(normalize('Œuf chocolat lait cœur lacté 2 kg')).toBe('oeuf chocolat lait coeur lacte 2 kg')
  })

  it('ignore le cœur U+2665, présent dans 127 des 355 noms réels', () => {
    expect(normalize('♥ LENTILLES VERTES 10Kg')).toBe('lentilles vertes 10kg')
    expect(normalize('♥AA-TOMME DE SAVOIE -MV')).toBe('aa tomme de savoie mv')
  })

  it('replie « Σ » en « σ » même en fin de mot, comme unicode.ToLower en Go', () => {
    // toLowerCase() appliqué au MOT donne le sigma final « ς », que Go ne produit
    // jamais : unicode.ToLower rend « σ » sans regarder le contexte. Replier point
    // de code par point de code supprime le contexte, donc la divergence.
    expect('ΟΔΟΣ'.toLowerCase()).toBe('οδος')
    expect(normalize('ΟΔΟΣ')).toBe('οδοσ')
  })

  it('ne dilate aucune lettre : « İ » perd son point avant d’être repliée', () => {
    // 'İ'.toLowerCase() vaut « i + U+0307 » en JavaScript et « i » en Go. La
    // décomposition NFD retire la marque combinante AVANT le pliage, donc les deux
    // implémentations rendent « i ».
    expect('İ'.toLowerCase()).toHaveLength(2)
    expect(normalize('İ')).toBe('i')
  })
})
