import { describe, expect, it } from 'vitest'
import { expectedCatalogFile, sharesOneFile } from '../src/admin/lib/catalog-file'

/**
 * Le nom du fichier de catalogue qu'un poste attend.
 *
 * Il se dérive du modèle `catalog.options.file_name` et du numéro du poste, comme
 * `catalog.FileName` côté Go : l'écran qui dirait un autre nom que celui que le poste
 * cherche enverrait un producteur déposer un fichier que personne ne lira.
 */
describe('expectedCatalogFile', () => {
  it('retombe sur flv_<numéro>.csv quand le modèle est absent', () => {
    expect(expectedCatalogFile(undefined, 2)).toBe('flv_2.csv')
  })

  it('traite un modèle vide comme absent', () => {
    expect(expectedCatalogFile('', 4)).toBe('flv_4.csv')
  })

  it('garde un nom sans {n} tel quel', () => {
    expect(expectedCatalogFile('flv.csv', 1)).toBe('flv.csv')
  })

  it('remplace chaque {n} par le numéro du poste', () => {
    expect(expectedCatalogFile('p{n}_{n}.csv', 3)).toBe('p3_3.csv')
  })
})

describe('sharesOneFile', () => {
  it('est vrai quand le modèle n’a pas de {n}', () => {
    expect(sharesOneFile('flv.csv')).toBe(true)
  })

  it('est faux quand le modèle porte {n}', () => {
    expect(sharesOneFile('flv_{n}.csv')).toBe(false)
  })

  it('est faux sans modèle : le défaut porte le numéro du poste', () => {
    expect(sharesOneFile(undefined)).toBe(false)
    expect(sharesOneFile('')).toBe(false)
  })
})
