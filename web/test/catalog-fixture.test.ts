import { describe, expect, it } from 'vitest'
import { qualifyExport, readExport, type QualifiedRow } from './fixtures/odoo'

/**
 * Le harnais de fixture reproduit-il l'inventaire MESURÉ des deux exports réels ?
 *
 * Ce test ne vérifie pas le front : il vérifie que le catalogue servi aux autres
 * tests est bien celui du fichier authentique. Sans lui, « la grille rend 331
 * produits » ne prouverait que la capacité du harnais à en fabriquer 331.
 *
 * Les chiffres viennent de §10.2 et §10.3, mesurés sur les fichiers eux-mêmes.
 */

/** Compte les lignes d'une qualification donnée. */
function count(rows: QualifiedRow[], qualification: QualifiedRow['qualification']): number {
  return rows.filter((r) => r.qualification === qualification).length
}

describe('flv.csv — la pièce de référence du 24/07/2026', () => {
  const rows = qualifyExport(readExport('flv.csv'))

  it('lit les 355 lignes annoncées', () => {
    expect(rows).toHaveLength(355)
  })

  it('rend l’inventaire 355 / 331 / 8 / 16 / 1 de §10.3', () => {
    expect(count(rows, 'weighable')).toBe(331)
    expect(count(rows, 'not_weighable')).toBe(8)
    expect(count(rows, 'anomaly')).toBe(16)
    expect(rows.filter((r) => r.finding === 'UNIT_MISMATCH')).toHaveLength(1)
  })

  it('nomme les 8 non-pesables : 7 préemballés et 1 code interne 0490', () => {
    const reasons = rows
      .filter((r) => r.qualification === 'not_weighable')
      .map((r) => r.reason)
      .sort()
    expect(reasons.filter((r) => r === 'PREPACKAGED_PRODUCT')).toHaveLength(7)
    expect(reasons.filter((r) => r === 'INTERNAL_CODE_NOT_WEIGHABLE')).toHaveLength(1)
  })

  it('range les 16 anomalies sous « zone de réservation occupée », et pas ailleurs', () => {
    const anomalies = rows.filter((r) => r.qualification === 'anomaly')
    expect(anomalies.every((r) => r.reason === 'RESERVED_ZONE_NOT_EMPTY')).toBe(true)
    // Le contre-exemple nommé par §6.2 (T32) : il doit être dans le lot.
    expect(anomalies.some((r) => r.product.name.includes('TOMME DE SAVOIE'))).toBe(true)
  })

  it('signale l’unité divergente de CAROTTE BOTTE SAF, et la laisse pesable', () => {
    const divergent = rows.filter((r) => r.finding === 'UNIT_MISMATCH')
    expect(divergent[0]?.product.name).toBe('CAROTTE BOTTE SAF')
    expect(divergent[0]?.qualification).toBe('weighable')
  })

  it('répartit les lignes reçues en A = 140, V = 118, L = 68, F = 29', () => {
    const perLetter = new Map<string, number>()
    for (const r of rows) perLetter.set(r.letter, (perLetter.get(r.letter) ?? 0) + 1)
    expect(perLetter.get('A')).toBe(140)
    expect(perLetter.get('V')).toBe(118)
    expect(perLetter.get('L')).toBe(68)
    expect(perLetter.get('F')).toBe(29)
  })

  it('trouve 181 images sur les 355 lignes reçues, donc 174 lignes sans photo', () => {
    expect(rows.filter((r) => r.hasImage)).toHaveLength(181)
    expect(rows.filter((r) => !r.hasImage)).toHaveLength(174)
  })

  it('ÉCART DOCUMENTAIRE : les tuiles se partagent 177 / 154, et non 181 / 174', () => {
    // §14.4 écrit « 331 pesables ← dans la grille (181 avec photo, 174 sans) » et
    // §16.2 (12 bis) « 331 tuiles dont 174 sans photo ». Or 181 + 174 = 355 : ce
    // sont les totaux sur les lignes REÇUES, pas sur les tuiles. Quatre des 181
    // images appartiennent à des lignes qui n'ont pas de tuile.
    const weighable = rows.filter((r) => r.qualification === 'weighable')
    expect(weighable.filter((r) => r.hasImage)).toHaveLength(177)
    expect(weighable.filter((r) => !r.hasImage)).toHaveLength(154)
    expect(rows.filter((r) => r.qualification !== 'weighable' && r.hasImage)).toHaveLength(4)
  })

  it('reconnaît le format aux octets d’en-tête : 171 JPEG et 10 PNG', () => {
    // §16.1 nomme ces deux chiffres, dont « les 10 PNG que l'ancienne application
    // nommait .jpg ». Les retrouver prouve que le décodage base64 et la détection
    // du format sont justes, donc que le partage 177 / 154 ci-dessus l'est aussi.
    const extensions = rows
      .filter((r) => r.hasImage)
      .map((r) => r.product.image_url.split('.')[1])
    expect(extensions.filter((e) => e === 'jpg')).toHaveLength(171)
    expect(extensions.filter((e) => e === 'png')).toHaveLength(10)
  })

  it('tient le mode depuis le préfixe : 316 au poids, 15 à l’unité', () => {
    const weighable = rows.filter((r) => r.qualification === 'weighable')
    expect(weighable.filter((r) => r.product.mode === 'by_weight')).toHaveLength(316)
    expect(weighable.filter((r) => r.product.mode === 'by_unit')).toHaveLength(15)
  })

  it('laisse les 2 produits « Litre(s) » au poids, sans signalement', () => {
    const litres = rows.filter((r) => r.product.price_suffix === ' € le litre')
    expect(litres).toHaveLength(9)
    const weighed = litres.filter((r) => r.qualification === 'weighable')
    expect(weighed).toHaveLength(2)
    expect(weighed.every((r) => r.product.mode === 'by_weight' && r.finding === '')).toBe(true)
    expect(weighed.map((r) => r.product.name).sort()).toEqual([
      'SAVON LIQUIDE LAVANDE 20KG',
      'SHAMPOING CHEVEUX NORMAUX',
    ])
  })

  it('parse les prix en entier : « 7.89 » vaut 789 centimes', () => {
    const lentils = rows.find((r) => r.product.name === 'LENTILLES VERTES ♥ *')
    expect(lentils?.product.unit_price_cents).toBe(789)
    expect(rows.every((r) => Number.isInteger(r.product.unit_price_cents))).toBe(true)
  })
})

describe('flv_1.csv — l’export du 05/01/2022', () => {
  const rows = qualifyExport(readExport('flv_1.csv'))

  it('rend l’inventaire 153 / 107 / 39 / 7 / 5 de §10.3', () => {
    expect(rows).toHaveLength(153)
    expect(count(rows, 'weighable')).toBe(107)
    expect(count(rows, 'not_weighable')).toBe(39)
    expect(count(rows, 'anomaly')).toBe(7)
    expect(rows.filter((r) => r.finding === 'UNIT_MISMATCH')).toHaveLength(5)
  })

  it('n’a aucune image, sur 153 lignes sur 153', () => {
    expect(rows.filter((r) => r.hasImage)).toHaveLength(0)
  })

  it('compte 9 produits sans code-barres et 7 clés de contrôle fausses', () => {
    const reasons = rows.map((r) => r.reason)
    expect(reasons.filter((r) => r === 'NO_BARCODE')).toHaveLength(9)
    expect(reasons.filter((r) => r === 'INVALID_BARCODE')).toHaveLength(7)
  })

  it('inverse la répartition de 2026 : L = 84, V = 58, F = 10, A = 1', () => {
    const perLetter = new Map<string, number>()
    for (const r of rows) perLetter.set(r.letter, (perLetter.get(r.letter) ?? 0) + 1)
    expect(perLetter.get('L')).toBe(84)
    expect(perLetter.get('V')).toBe(58)
    expect(perLetter.get('F')).toBe(10)
    expect(perLetter.get('A')).toBe(1)
  })
})
