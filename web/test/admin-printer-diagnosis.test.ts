import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PrinterDiagnosis from '../src/admin/components/PrinterDiagnosis.svelte'
import { diagnosisReport } from '../src/admin/lib/printer-report'
import type { PrinterDiagnosisDTO } from '../src/admin/lib/dto'

/**
 * Le diagnostic pas à pas de l'imprimante, bloc « Imprimante » de la page Matériel
 * (§14.4) — monté seul, hors de `Hardware.svelte` : les deux routes qu'il appelle sont
 * libres (décision 7), et rien ici n'a l'usage du reste de la page.
 *
 * Ce que ce fichier tient :
 *
 *  - « Lancer le diagnostic » rend les cinq étapes, l'observé à chasse fixe ;
 *  - une étape « unknown » (le silence de l'étape 3, décision 2) ne porte pas la
 *    classe d'échec ;
 *  - la question de l'étape 5 n'apparaît que si elle est encore « sans réponse » ;
 *  - « Non » envoie exactement les quatre premiers états et le texte libre, et
 *    remplace l'étape 5 par la réponse du service ;
 *  - « Copier le rapport » est actif avant toute réponse à l'étape 5 ;
 *  - `navigator.clipboard` simulé reçoit le texte de `diagnosisReport` ; absent, un
 *    repli visible montre le même texte, déjà sélectionné (Review Focus) ;
 *  - une relance efface l'ancien parcours, et une réponse arrivée après une relance
 *    n'est pas greffée sur le nouveau (revue finale, T6 m2 + m3) ;
 *  - « À faire » ne précède que le remède d'une étape en échec (T6 m4) ;
 *  - le lien « Documentation » s'ouvre dans un nouvel onglet (T6 m5).
 */

/** Le parcours que « Lancer le diagnostic » rend : rien à l'étape 3 (silence, décision 2). */
const DIAGNOSIS: PrinterDiagnosisDTO = {
  target: "nœud d'impression /dev/usb/lp0",
  steps: [
    {
      number: 1,
      title: 'Le périphérique existe',
      state: 'pass',
      observed: '« /dev/usb/lp0 » existe.',
      remedy: '',
      doc: '',
    },
    {
      number: 2,
      title: 'Le poste a le droit d’y écrire',
      state: 'pass',
      observed: '',
      remedy: '',
      doc: '',
    },
    {
      number: 3,
      title: "L'imprimante répond quand on lui demande son état",
      state: 'unknown',
      observed: 'aucune réponse à la sonde de statut en 500 ms.',
      remedy:
        "Ce n'est pas une panne en soi : certains réglages de l'interface ne renvoient pas d'état. On continue.",
      doc: '',
    },
    {
      number: 4,
      title: 'Une étiquette de test est envoyée en entier',
      state: 'pass',
      observed: '16 214 octets.',
      remedy: '',
      doc: '',
    },
    {
      number: 5,
      title: 'L’étiquette est-elle sortie ?',
      state: 'unanswered',
      observed: '',
      remedy: '',
      doc: '',
    },
  ],
}

let host: HTMLElement
let component: unknown
/** Tous les appels passés, dans l'ordre : « méthode route ». */
let calls: { method: string; route: string; body: unknown }[] = []
/** Ce que la prochaine route de diagnostic rend : un test de relance le change. */
let nextDiagnosis: PrinterDiagnosisDTO = DIAGNOSIS
/** Posées par un test, ces barrières retiennent la réponse de leur route jusqu'à `release`. */
let diagnosisGate: Promise<void> | null = null
let answerGate: Promise<void> | null = null

/** Une barrière, et la fonction qui la lève. */
function gate(): { held: Promise<void>; release: () => void } {
  let release = (): void => {}
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  return { held, release }
}

beforeEach(() => {
  calls = []
  nextDiagnosis = DIAGNOSIS
  diagnosisGate = null
  answerGate = null
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

async function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const route = String(input)
  const method = init?.method ?? 'GET'
  const body = JSON.parse(String(init?.body ?? '{}')) as { seen?: string }
  calls.push({ method, route, body })
  if (route === '/admin/api/printer/diagnosis' && method === 'POST') {
    const answer = nextDiagnosis
    if (diagnosisGate !== null) await diagnosisGate
    return json(answer)
  }
  if (route === '/admin/api/printer/diagnosis/answer' && method === 'POST') {
    if (answerGate !== null) await answerGate
    return json({
      step: {
        number: 5,
        title: 'L’étiquette est-elle sortie ?',
        state: 'fail',
        observed: body.seen,
        remedy: "L'imprimante a tout reçu mais n'a pas fini le travail.",
        doc: 'https://lostmind84.github.io/OpenScale/imprimante/#etape-5',
      },
    })
  }
  return json({})
}

function json(body: unknown): Promise<Response> {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
}

/** Monte le composant seul : un seul acte à la fois n'a pas de sens ici, `onrun` lance tout de suite. */
function open(): void {
  component = mount(PrinterDiagnosis, {
    target: host,
    props: {
      disabled: false,
      busy: false,
      onrun: (job: () => Promise<void>) => void job(),
    },
  })
  flushSync()
}

function button(fragment: string): HTMLButtonElement {
  const found = [...host.querySelectorAll('button')].find((candidate) =>
    (candidate.textContent ?? '').includes(fragment),
  )
  if (found === undefined) throw new Error(`aucun bouton « ${fragment} » à l'écran`)
  return found
}

async function waitFor(what: () => boolean): Promise<void> {
  await vi.waitUntil(
    () => {
      flushSync()
      return what()
    },
    { timeout: 2000, interval: 5 },
  )
}

async function launch(): Promise<void> {
  open()
  button('Lancer le diagnostic').click()
  await waitFor(() => host.querySelectorAll('[data-diagnosis-steps] li').length === 5)
}

describe('le diagnostic pas à pas de l’imprimante', () => {
  it('rend les cinq étapes, l’observé à chasse fixe', async () => {
    await launch()
    const steps = host.querySelectorAll('[data-diagnosis-steps] li')
    expect(steps.length).toBe(5)
    expect(host.textContent).toContain("L'imprimante répond quand on lui demande son état")
    const observed = host.querySelector<HTMLElement>('[data-state="unknown"] [data-observed]')
    expect(observed).not.toBeNull()
    expect(observed?.textContent).toContain('aucune réponse à la sonde de statut en 500 ms.')
    expect(getComputedStyle(observed as HTMLElement).fontFamily).toMatch(/monospace/u)
  })

  it('ne classe jamais le silence de l’étape 3 comme un échec', async () => {
    await launch()
    const step3 = host.querySelector('[data-state="unknown"]')
    expect(step3).not.toBeNull()
    expect(step3?.classList.contains('fail')).toBe(false)
  })

  it('ne pose la question que si l’étape 5 est encore sans réponse', async () => {
    await launch()
    expect(host.querySelector('[data-ask]')).not.toBeNull()
    expect(host.textContent).toContain('L’étiquette est-elle sortie ?')
  })

  it('« Non » envoie les quatre premiers états et le texte libre, et remplace l’étape 5', async () => {
    await launch()
    const seenField = host.querySelector<HTMLInputElement>('[data-ask] input[type="text"]')
    if (seenField === null) throw new Error('aucun champ de texte à l’étape 5')
    seenField.value = 'le voyant 1 clignote'
    seenField.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()

    button('Non').click()
    await waitFor(() => calls.some((call) => call.route === '/admin/api/printer/diagnosis/answer'))

    const answerCall = calls.find((call) => call.route === '/admin/api/printer/diagnosis/answer')
    expect(answerCall?.body).toEqual({
      steps: ['pass', 'pass', 'unknown', 'pass'],
      printed: false,
      seen: 'le voyant 1 clignote',
    })

    await waitFor(() => host.querySelector('[data-state="fail"]') !== null)
    expect(host.querySelector('[data-ask]')).toBeNull()
  })

  it('garde « Copier le rapport » actif avant toute réponse à l’étape 5', async () => {
    await launch()
    expect(button('Copier le rapport').disabled).toBe(false)
  })

  it('appelle navigator.clipboard.writeText une seule fois, avec le texte du rapport', async () => {
    await launch()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    button('Copier le rapport').click()
    await waitFor(() => writeText.mock.calls.length > 0)

    expect(writeText).toHaveBeenCalledTimes(1)
    expect(writeText).toHaveBeenCalledWith(diagnosisReport(DIAGNOSIS))
  })

  it('sans navigator.clipboard, montre un repli sélectionné avec la consigne Ctrl+C', async () => {
    await launch()
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })

    button('Copier le rapport').click()
    await waitFor(() => host.querySelector('[data-copy-fallback]') !== null)

    expect(host.textContent).toContain('Ctrl+C')
    const area = host.querySelector<HTMLTextAreaElement>('[data-copy-text]')
    expect(area).not.toBeNull()
    expect(area?.value).toBe(diagnosisReport(DIAGNOSIS))
    expect(area?.selectionStart).toBe(0)
    expect(area?.selectionEnd).toBe((area?.value ?? '').length)
  })

  it('une relance efface l’ancien parcours : plus de question à laquelle répondre', async () => {
    await launch()
    expect(host.querySelector('[data-ask]')).not.toBeNull()

    const { held, release } = gate()
    diagnosisGate = held
    button('Lancer le diagnostic').click()
    flushSync()

    // Pendant que l'étiquette de la relance s'imprime, rien de l'ancien parcours ne
    // reste cliquable.
    expect(host.querySelector('[data-ask]')).toBeNull()
    expect(host.querySelector('[data-diagnosis-steps]')).toBeNull()

    release()
    await waitFor(() => host.querySelector('[data-ask]') !== null)
  })

  it('une réponse revenue après une relance n’est pas greffée sur le nouveau parcours', async () => {
    await launch()

    const { held, release } = gate()
    answerGate = held
    button('Non').click()
    await waitFor(() => calls.some((call) => call.route === '/admin/api/printer/diagnosis/answer'))

    nextDiagnosis = { ...DIAGNOSIS, target: 'file Windows « SATO WS408_2 »' }
    button('Lancer le diagnostic').click()
    await waitFor(
      () => (host.textContent ?? '').length > 0 && host.querySelector('[data-ask]') !== null,
    )

    release()
    await new Promise((resolve) => setTimeout(resolve, 20))
    flushSync()

    // L'étape 5 du nouveau parcours attend toujours sa propre réponse.
    expect(host.querySelector('[data-state="fail"]')).toBeNull()
    expect(host.querySelector('[data-ask]')).not.toBeNull()
  })

  it('« À faire » ne précède que le remède d’une étape en échec', async () => {
    await launch()
    const step3 = host.querySelector('[data-state="unknown"] .remedy')
    expect(step3?.textContent).toContain("certains réglages de l'interface")
    expect(step3?.textContent).not.toContain('À faire')

    button('Non').click()
    await waitFor(() => host.querySelector('[data-state="fail"]') !== null)
    expect(host.querySelector('[data-state="fail"] .remedy')?.textContent).toContain('À faire :')
  })

  it('ouvre la documentation dans un nouvel onglet : le parcours reste à l’écran', async () => {
    await launch()
    button('Non').click()
    await waitFor(() => host.querySelector('[data-state="fail"] a') !== null)
    const link = host.querySelector<HTMLAnchorElement>('[data-state="fail"] a')
    expect(link?.target).toBe('_blank')
    expect(link?.rel).toBe('noreferrer noopener')
  })
})
