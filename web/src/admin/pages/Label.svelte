<script lang="ts">
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { Draft } from '../lib/draft.svelte'
  import type { Admin } from '../lib/session.svelte'

  /**
   * La page Étiquette de §14.4, et le seul écran de tout le produit qui rende un réglage
   * possible SANS IMPRIMER.
   *
   * L'aperçu est un PNG rendu par le même moteur que l'impression (A2). Le décalage y est
   * donc CUIT DANS LE BITMAP : les flèches ±1 dot le déplacent et l'image le montre, ce
   * qu'aucun formulaire de nombres ne permet — un réglage de 203 dpi ne se vérifie pas en
   * lisant un chiffre, il se vérifie en regardant si le texte tombe dans la découpe.
   *
   * Le symbole EAN-13 y apparaît volontairement tronqué : ce n'est pas un défaut mais un
   * compromis assumé (ADR-003), un symbole conforme n'entrant pas sur 40 × 25 mm avec les
   * cinq champs texte.
   */
  interface Props {
    admin: Admin
    draft: Draft
  }

  const { admin, draft }: Props = $props()

  /** Force le navigateur à redemander l'image : l'URL doit changer à chaque frappe. */
  let nonce = $state(1)
  let demo = $state(true)
  let dual = $state(false)

  const template = $derived(draft.text('printer.options.template'))
  const source = $derived(api.previewURL(template, demo, dual, nonce))

  /**
   * Déplace le décalage d'un dot et redemande l'aperçu.
   *
   * Les bornes sont celles de la GÉOMÉTRIE du gabarit, et elles ne sont pas connues d'ici :
   * l'aperçu répond 422 avec la phrase du moteur quand le décalage sort de la découpe, et
   * c'est cette phrase qui s'affiche. Inventer une borne côté écran reviendrait à inventer
   * une géométrie.
   *
   * @param axis - `offset_x` ou `offset_y`.
   * @param step - le nombre de dots, +1 ou -1.
   */
  function nudge(axis: 'offset_x' | 'offset_y', step: number): void {
    const path = 'printer.options.' + axis
    draft.set(path, draft.number(path) + step)
    nonce += 1
  }
</script>

<div class="pages">
  <Panel
    title="Aperçu de l’étiquette"
    note="Le même rendu que l’impression : le décalage se voit parce qu’il est cuit dans le bitmap (A2)."
  >
    <div class="preview">
      <img src={source} alt="Aperçu de l’étiquette telle qu’elle serait imprimée" />
      <div class="nudges">
        <p class="axis">
          Décalage horizontal
          <strong data-offset-x>{draft.number('printer.options.offset_x')} dots</strong>
        </p>
        <div class="pair">
          <button type="button" class="nudge touch-target" onclick={() => nudge('offset_x', -1)}>
            ← 1 dot
          </button>
          <button type="button" class="nudge touch-target" onclick={() => nudge('offset_x', 1)}>
            1 dot →
          </button>
        </div>
        <p class="axis">
          Décalage vertical
          <strong data-offset-y>{draft.number('printer.options.offset_y')} dots</strong>
        </p>
        <div class="pair">
          <button type="button" class="nudge touch-target" onclick={() => nudge('offset_y', -1)}>
            ↑ 1 dot
          </button>
          <button type="button" class="nudge touch-target" onclick={() => nudge('offset_y', 1)}>
            1 dot ↓
          </button>
        </div>
        <label class="check">
          <input type="checkbox" bind:checked={demo} onchange={() => (nonce += 1)} />
          <span>Valeurs de démonstration</span>
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={dual} onchange={() => (nonce += 1)} />
          <span>Deux tarifs — le cas le plus chargé</span>
        </label>
      </div>
    </div>
    <p class="truncation">
      Le symbole code-barres est volontairement tronqué (ADR-003) : un symbole conforme
      n’entre pas sur 40 × 25 mm avec les cinq champs texte. Ce n’est pas un défaut de
      rendu et il n’y a rien à corriger.
    </p>
  </Panel>

  <Panel title="Gabarit et impression">
    <Field
      label="Gabarit"
      path="printer.options.template"
      value={template}
      hint="Le gabarit reproduit à l’identique s’appelle weighing_identical (A1)."
      onchange={(value) => {
        draft.set('printer.options.template', value)
        nonce += 1
      }}
    />
    <Field
      label="Noircissement"
      path="printer.options.darkness"
      kind="number"
      value={draft.text('printer.options.darkness')}
      hint="Trop bas, l’étiquette pâlit au soleil ; trop haut, elle bave et le scanner refuse."
      onchange={(value) => draft.set('printer.options.darkness', Number(value))}
    />
    <Field
      label="Vitesse"
      path="printer.options.speed"
      kind="number"
      value={draft.text('printer.options.speed')}
      onchange={(value) => draft.set('printer.options.speed', Number(value))}
    />
    <Field
      label="Exemplaires"
      path="printer.options.copies"
      kind="number"
      value={draft.text('printer.options.copies')}
      hint="Un client repart avec une étiquette : deux exemplaires se justifient, pas se devinent."
      onchange={(value) => draft.set('printer.options.copies', Number(value))}
    />
    <div class="actions">
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(() => api.printerSelfTest('alignment'))}
      >
        Imprimer la mire d’alignement
      </button>
      <button
        type="button"
        class="action touch-target"
        onclick={() => void admin.run(() => api.printerSelfTest('ruler'))}
      >
        Imprimer la réglette
      </button>
    </div>
  </Panel>
</div>

<style>
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .preview {
    display: flex;
    flex-wrap: wrap;
    gap: 1.5rem;
    align-items: flex-start;
  }

  /* L'aperçu est agrandi et à pixels NETS : à 203 dpi, une étiquette de 40 × 25 mm fait
     320 × 200 pixels, et un lissage rendrait invisible le dot qu'on est venu régler. */
  img {
    width: 40rem;
    max-width: 100%;
    image-rendering: pixelated;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .nudges {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .axis {
    margin: 0;
    font-size: 1.125rem;
  }

  .pair {
    display: flex;
    gap: var(--touch-gap);
  }

  .nudge,
  .action {
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .check {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    font-size: 1.0625rem;
  }

  .check input {
    width: 1.5rem;
    height: 1.5rem;
  }

  .truncation {
    margin: 1rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    margin-top: 0.75rem;
  }
</style>
