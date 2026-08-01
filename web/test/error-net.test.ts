import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installErrorNet } from '../src/lib/error-net'

/**
 * Le filet ERR-UI-01, et la règle que le poste pilote a imposée le 01/08/2026 :
 * **un avertissement du navigateur n'est pas une exception.**
 *
 * Mesuré sur DESKTOP-EKE0I2T. Un bénévole règle la grille sur dix colonnes depuis
 * l'écran d'administration, enregistre, et l'écran client tombe sur « Une erreur est
 * survenue » toutes les 5,12 s — la cadence de `RELOAD_AFTER_S`. 43 entrées au journal
 * technique, toutes identiques :
 *
 *     ERR-UI-01  detail : ResizeObserver loop completed with undelivered notifications.
 *
 * Ce message n'est le signe d'aucun bug de cet écran : c'est l'avertissement qu'un
 * navigateur émet quand un `ResizeObserver` n'a pas convergé dans la frame. Mais il
 * arrive par un événement `error` sur `window`, que le filet attrapait sans le lire —
 * donc voile, rechargement, remesure, même avertissement. La boucle ne s'arrêtait pas.
 *
 * Le prix n'était pas dix colonnes, qui se reprennent : c'est que N'IMPORTE QUEL hoquet
 * de mise en page — un écran rebranché, une rotation — blanchit un poste en libre-service
 * et le fait recharger sans fin, devant un client.
 *
 * **Aucun test ne pouvait le voir**, et c'est la seconde leçon : `test/setup.ts` remplace
 * `ResizeObserver` par une classe qui n'observe rien, et jsdom ne fait pas de mise en
 * page. L'événement réel n'existe nulle part dans cette suite. Ce banc ne l'attend donc
 * pas d'une mesure : il le POSE, tel que le navigateur l'écrit.
 */

/** L'avertissement relevé sur le poste pilote, au caractère près. */
const LAYOUT_NOTICE = 'ResizeObserver loop completed with undelivered notifications.'

/** Les deux formes qu'il prend, telles que les navigateurs les écrivent. */
const BROWSER_NOTICES = [
  LAYOUT_NOTICE,
  // La forme antérieure, encore émise par des navigateurs en service.
  'ResizeObserver loop limit exceeded',
]

let uninstall: () => void
let fetched: ReturnType<typeof vi.fn>

beforeEach(() => {
  vi.useFakeTimers()
  fetched = vi.fn(() => Promise.resolve(new Response(null, { status: 202 })))
  vi.stubGlobal('fetch', fetched)
  document.body.replaceChildren()
  uninstall = installErrorNet()
})

afterEach(() => {
  uninstall()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

/** Pose l'événement `error` que le navigateur poserait, avec ou sans exception derrière. */
function raise(message: string, error?: Error): void {
  window.dispatchEvent(new ErrorEvent('error', { message, error }))
}

/** Le voile, ou `null` tant que l'écran n'a pas été remplacé. */
function veil(): Element | null {
  return document.querySelector('.fatal')
}

/** Le premier signalement parti au journal technique — l'absence est une faute du banc. */
function firstReport(): { route: string; body: string } {
  const call = fetched.mock.calls[0]
  if (call === undefined) throw new Error('aucun signalement n’est parti')
  return { route: String(call[0]), body: String((call[1] as RequestInit | undefined)?.body) }
}

describe('le filet ERR-UI-01 lit ce qu’il attrape', () => {
  it.each(BROWSER_NOTICES)('ne lève pas le voile sur « %s »', (notice) => {
    raise(notice)

    expect(veil()).toBeNull()
  })

  it.each(BROWSER_NOTICES)('ne programme aucun rechargement sur « %s »', (notice) => {
    raise(notice)

    // Zéro et non « pas encore » : c'est le rechargement qui refaisait la mise en page,
    // donc le même avertissement, donc la boucle. Un seul minuteur armé la referme.
    expect(vi.getTimerCount()).toBe(0)
  })

  it('garde la trace de l’avertissement au journal technique', () => {
    // Le taire entièrement aurait coûté le diagnostic : c'est cette ligne-là, et elle
    // seule, qui a nommé le défaut. Un écran qui ne converge pas reste un symptôme.
    raise(LAYOUT_NOTICE)

    expect(fetched).toHaveBeenCalledTimes(1)
    expect(firstReport().body).toContain('ResizeObserver')
  })

  it('l’envoie par la route des avertissements, PAS par celle des exceptions', () => {
    // La route décide du niveau et du code côté poste : ERR-UI-02 en « warn » ici,
    // ERR-UI-01 en « error » là-bas. Les confondre remettrait « Erreur JavaScript dans
    // l'écran client » au journal d'un poste qui sert ses clients sans broncher.
    raise(LAYOUT_NOTICE)

    expect(firstReport().route).toBe('/api/v1/ui/layout-notice')
  })

  it('ne signale l’avertissement qu’une fois par chargement', () => {
    // Un `ResizeObserver` qui boucle le dit à chaque frame. Sans ce garde-fou, le filet
    // remplacerait une boucle de rechargements par une boucle d'écritures au journal.
    for (let i = 0; i < 20; i += 1) raise(LAYOUT_NOTICE)

    expect(fetched).toHaveBeenCalledTimes(1)
  })
})

describe('le filet ERR-UI-01 attrape toujours ce qui compte', () => {
  it('lève le voile sur une vraie exception', () => {
    raise('TypeError: products.filter is not a function', new TypeError('products.filter'))

    expect(veil()).not.toBeNull()
    expect(veil()?.querySelector('h1')?.textContent).toBe('Une erreur est survenue')
    expect(veil()?.querySelector('.code')?.textContent).toBe('ERR-UI-01')
  })

  it('programme le rechargement qui répare l’écran', () => {
    raise('TypeError: products.filter is not a function', new TypeError('products.filter'))

    expect(vi.getTimerCount()).toBe(1)
  })

  it('signale l’exception au journal technique', () => {
    raise('TypeError: products.filter is not a function', new TypeError('products.filter'))

    expect(fetched).toHaveBeenCalledTimes(1)
    expect(firstReport().route).toBe('/api/v1/ui/error')
    expect(firstReport().body).toContain('products.filter')
  })

  it('n’attrape pas deux fois : le premier voile tient jusqu’au rechargement', () => {
    raise('TypeError: le premier', new TypeError('le premier'))
    raise('TypeError: le second', new TypeError('le second'))

    expect(document.querySelectorAll('.fatal')).toHaveLength(1)
    expect(fetched).toHaveBeenCalledTimes(1)
  })

  it('attrape une promesse rejetée', () => {
    window.dispatchEvent(
      new PromiseRejectionEvent('unhandledrejection', {
        promise: Promise.reject(new Error('boum')).catch(() => undefined) as Promise<never>,
        reason: new Error('boum'),
      }),
    )

    expect(veil()).not.toBeNull()
  })

  it('une exception dont le message NOMME ResizeObserver reste une exception', () => {
    // Le filtre porte sur l'avertissement de mise en page, pas sur le mot. Une pile
    // d'appel accompagne l'exception et jamais l'avertissement : c'est ce qui les sépare.
    raise(
      'TypeError: ResizeObserver is not a constructor',
      new TypeError('ResizeObserver is not a constructor'),
    )

    expect(veil()).not.toBeNull()
  })
})
