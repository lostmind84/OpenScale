import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Catalog from '../src/admin/pages/Catalog.svelte'
import { Draft } from '../src/admin/lib/draft.svelte'
import type { HealthDTO } from '../src/admin/lib/dto'
import { Admin } from '../src/admin/lib/session.svelte'
import { nominalHealth } from './fixtures/health'

/**
 * Le panneau qui dit où le poste va chercher son catalogue.
 *
 * Ce qu'il tient, et qui n'allait pas de soi :
 *
 *  1. le répertoire ne se montre QUE sur une source locale — un serveur WebDAV n'en a
 *     pas, et un champ vide sous une adresse est une invitation à le remplir ;
 *  2. le dépôt d'un CSV depuis l'écran DISPARAÎT sur WebDAV : le poste n'a plus de
 *     fichier local où l'écrire, et c'est le seul recours du jour de mise en service ;
 *  3. le mot de passe est en écriture seule : laissé vide, il ne bouge pas ;
 *  4. changer de source EFFACE les réglages de l'autre. Le poste refuse la présence même
 *     d'un compte sur un répertoire local, et celle d'un répertoire sur un serveur : sans
 *     ce ménage, le seul geste que ce panneau existe pour offrir revenait en trois refus
 *     portant sur des champs que personne n'avait remplis.
 */

let host: HTMLElement
let component: unknown

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  vi.stubGlobal('fetch', fakeFetch)
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
  vi.unstubAllGlobals()
})

/**
 * Les lectures que la page lance au montage, et rien d'autre.
 *
 * Elles ne servent aucun test de ce fichier : ce qui les intéresse est ce que le panneau
 * de source affiche du BROUILLON. Sans elles, la page part sur le réseau du poste de
 * travail à chaque montage.
 */
function fakeFetch(input: RequestInfo | URL): Promise<Response> {
  const url = String(input)
  if (url.startsWith('/admin/api/imports')) return json({ imports: [], findings: [] })
  if (url === '/api/v1/catalog') return json({ products: [], categories: [], tiers: [] })
  return json({ done: true, message: 'Fait.' })
}

/** Une réponse JSON, comme le service en écrit. */
function json(body: unknown, status = 200): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

/** Ce qu'un test peut demander au panneau une fois la page montée. */
interface Page {
  draft: Draft
  /** Le champ d'une clé, ou null quand la source affichée ne le montre pas. */
  field: (path: string) => HTMLInputElement | null
  /** Le texte de la page entière, apostrophes ramenées à une seule forme. */
  text: () => string
  /** Tape dans le champ d'une clé, exactement comme un exploitant le fait. */
  type: (path: string, value: string) => void
  /** Choisit une source, exactement comme un exploitant le fait. */
  choose: (type: string) => void
  /** Vrai quand le bouton radio de cette source est celui qui est coché. */
  chosen: (type: string) => boolean
}

/**
 * Monte la page Catalogue sur un brouillon portant ces valeurs.
 *
 * @param values - les chemins pointés du document et ce qu'ils valent.
 * @param station - le numéro du poste, dont le nom du fichier attendu dépend.
 */
function renderCatalog(values: Record<string, unknown> = {}, station = 2): Page {
  const admin = new Admin()
  const draft = new Draft(admin)
  draft.config = {}
  for (const [path, value] of Object.entries(values)) draft.set(path, value)
  draft.dirty = false

  const health: HealthDTO = { ...nominalHealth(), station }
  component = mount(Catalog, { target: host, props: { admin, draft, health } })
  flushSync()

  return {
    draft,
    field: (path) => host.querySelector<HTMLInputElement>('#' + fieldID(path)),
    text: () => collapse(host.textContent ?? ''),
    type: (path, value) => {
      const field = host.querySelector<HTMLInputElement>('#' + fieldID(path))
      if (field === null) throw new Error(`aucun champ ${path}`)
      field.value = value
      field.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()
    },
    choose: (type) => {
      const radio = sourceRadio(type)
      radio.checked = true
      radio.dispatchEvent(new Event('change', { bubbles: true }))
      flushSync()
    },
    chosen: (type) => sourceRadio(type).checked,
  }
}

/** L'identifiant que `Field` dérive du chemin d'une clé. */
function fieldID(path: string): string {
  return 'field-' + path.replace(/\./gu, '-')
}

/** Le bouton radio d'une source. */
function sourceRadio(type: string): HTMLInputElement {
  const found = host.querySelector<HTMLInputElement>(
    `input[name="catalog-source"][value="${type}"]`,
  )
  if (found === null) throw new Error(`aucun choix de source « ${type} »`)
  return found
}

/** Réduit les blancs et ramène toute apostrophe à une seule forme. */
function collapse(text: string): string {
  return text.replace(/\s+/gu, ' ').replace(/[’´`]/gu, "'").trim()
}

describe('le choix de la source', () => {
  it('montre le répertoire sur une source locale, et pas l’adresse', () => {
    const page = renderCatalog({ 'catalog.type': 'local_drop' })
    expect(page.field('catalog.options.directory')).not.toBeNull()
    expect(page.field('catalog.options.url')).toBeNull()
  })

  it('montre l’adresse et le compte sur WebDAV, et pas le répertoire', () => {
    const page = renderCatalog({ 'catalog.type': 'webdav' })
    expect(page.field('catalog.options.url')).not.toBeNull()
    expect(page.field('catalog.options.username')).not.toBeNull()
    expect(page.field('catalog.options.directory')).toBeNull()
  })

  it('coche la source du document, et non un défaut d’écran', () => {
    const page = renderCatalog({ 'catalog.type': 'webdav' })
    expect(page.chosen('webdav')).toBe(true)
    expect(page.chosen('local_drop')).toBe(false)
  })

  it('dit que le dépôt d’un CSV n’existe plus sur WebDAV, et le dit là seulement', () => {
    expect(renderCatalog({ 'catalog.type': 'webdav' }).text()).toContain(
      "le dépôt d'un fichier CSV depuis cet écran n'est plus possible",
    )
    expect(host.querySelector('[data-webdav-warning]')).not.toBeNull()

    unmount(component as Parameters<typeof unmount>[0])
    component = undefined
    renderCatalog({ 'catalog.type': 'local_drop' })
    expect(host.querySelector('[data-webdav-warning]')).toBeNull()
  })

  it('nomme le fichier attendu, dérivé du numéro du poste', () => {
    expect(renderCatalog({ 'catalog.type': 'local_drop' }, 2).text()).toContain('flv_2.csv')
  })

  it('annonce le répertoire du poste quand le champ est vide', () => {
    const page = renderCatalog({ 'catalog.type': 'local_drop', 'catalog.options.directory': '' })
    expect(page.text()).toContain('Laissez vide pour le répertoire du poste')
  })

  it('écrit le répertoire dans le brouillon, sans toucher au reste', () => {
    const page = renderCatalog({ 'catalog.type': 'local_drop' })
    page.type('catalog.options.directory', 'D:\\partage\\odoo')
    expect(page.draft.text('catalog.options.directory')).toBe('D:\\partage\\odoo')
    expect(page.draft.text('catalog.type')).toBe('local_drop')
  })

  it('laisse le mot de passe vide et le dit', () => {
    const page = renderCatalog({ 'catalog.type': 'webdav' })
    expect(page.field('catalog.options.password')?.getAttribute('type')).toBe('password')
    expect(page.text()).toContain("Laissez vide : le mot de passe actuel est conservé")
  })
})

describe('changer de source fait le ménage', () => {
  it('passer au répertoire local efface le compte et le mot de passe', () => {
    const page = renderCatalog({
      'catalog.type': 'webdav',
      'catalog.options.url': 'https://dav.example.org/',
      'catalog.options.username': 'balance',
      'catalog.options.password': '',
      'catalog.options.poll_interval_s': 5,
    })

    page.choose('local_drop')

    const options = page.draft.value('catalog.options') as Record<string, unknown>
    expect(Object.keys(options)).not.toContain('username')
    expect(Object.keys(options)).not.toContain('password')
    expect(Object.keys(options)).not.toContain('url')
    // Ce qui vaut pour les deux sources RESTE : la cadence de veille n'a rien à voir
    // avec l'endroit où le fichier est lu.
    expect(options['poll_interval_s']).toBe(5)
  })

  it('passer au serveur efface le répertoire surveillé', () => {
    const page = renderCatalog({
      'catalog.type': 'local_drop',
      'catalog.options.directory': 'D:\\partage\\odoo',
    })

    page.choose('webdav')

    const options = page.draft.value('catalog.options') as Record<string, unknown>
    expect(page.draft.text('catalog.type')).toBe('webdav')
    expect(Object.keys(options)).not.toContain('directory')
  })
})
