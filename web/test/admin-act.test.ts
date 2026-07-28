import { flushSync, mount, unmount, type ComponentProps } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Act from '../src/admin/components/Act.svelte'

/**
 * Le bouton de l'administration, et les trois choses qu'il portait à la main dans
 * chacune des quatre pages qui le redéfinissaient : sa famille, sa pastille et son
 * « En cours… ».
 */

let host: HTMLElement
let component: unknown

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  if (component !== undefined) unmount(component as Parameters<typeof unmount>[0])
  component = undefined
  host.remove()
})

/** Monte un acte et rend le bouton tel que le DOM le porte. */
function render(props: ComponentProps<typeof Act>): HTMLButtonElement {
  component = mount(Act, { target: host, props })
  flushSync()
  const button = host.querySelector('button')
  if (button === null) throw new Error('un acte doit se rendre en <button> : rien n’a été trouvé')
  return button
}

describe('la famille dit la nature de l’acte', () => {
  it('est neutre par défaut : lire ou tester ne change rien', () => {
    expect(render({ label: 'Tester la balance', onrun: () => {} }).dataset.kind).toBe('read')
  })

  it.each([
    ['write', 'Enregistrer'],
    ['destructive', 'Oublier la quarantaine'],
  ] as const)('porte la famille « %s » qu’on lui donne', (kind, label) => {
    expect(render({ kind, label, onrun: () => {} }).dataset.kind).toBe(kind)
  })
})

describe('ce que le bouton dit de lui-même', () => {
  it('annonce la clé AVANT le clic', () => {
    const button = render({ label: 'Recharger', protected: true, onrun: () => {} })
    expect(button.textContent).toContain('clé')
  })

  it('dit « En cours… » et refuse un second clic pendant qu’il travaille', () => {
    const onrun = vi.fn()
    const button = render({ label: 'Recharger', busy: true, onrun })
    expect(button.textContent).toContain('En cours')
    expect(button.textContent).not.toContain('Recharger')
    expect(button.disabled).toBe(true)
    button.click()
    expect(onrun).not.toHaveBeenCalled()
  })

  it('appelle son acte une fois par clic', () => {
    const onrun = vi.fn()
    render({ label: 'Tester', onrun }).click()
    expect(onrun).toHaveBeenCalledTimes(1)
  })

  it('un acte irréversible garde la cible de 72 px', () => {
    expect(render({ kind: 'destructive', label: 'Retirer', onrun: () => {} }).className).toContain(
      'touch-target',
    )
  })
})
