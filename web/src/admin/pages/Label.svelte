<script lang="ts">
  import Act from '../components/Act.svelte'
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { Draft } from '../lib/draft.svelte'
  import { frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * The Label page of §14.4, and the one screen of the whole product that makes a setting
   * adjustable WITHOUT PRINTING.
   *
   * The preview is a PNG rendered by the SAME engine as the print path (A2), so the
   * offset is baked into the bitmap and is looked at rather than read: a 203 dpi setting
   * is not checked by reading a number, it is checked by seeing whether the text falls
   * inside the die-cut.
   *
   * What the route takes, exactly. `GET /admin/api/label/preview.png` reads `template`,
   * `demo` and `dual` (`internal/web/admin.go`) and nothing else. The template is
   * therefore the one being EDITED here — it travels in the address — while the offsets
   * are those of the configuration IN SERVICE, recomposed into the geometry by
   * `templatesFor` (`cmd/openscale/serve.go`). The ±1 dot arrows write into the draft, so
   * the image does not move under them: this page says which offsets the image carries,
   * announces the gap for as long as the draft differs, and asks for the image again the
   * moment a save lands.
   *
   * The EAN-13 symbol shows up deliberately truncated: not a defect but an accepted
   * trade-off (ADR-003), a conforming symbol not fitting on 40 × 25 mm along with the
   * five text fields.
   */
  interface Props {
    admin: Admin
    draft: Draft
  }

  const { admin, draft }: Props = $props()

  /**
   * The template key, spelled the way the DOCUMENT spells it.
   *
   * `printer.template` is a field of `PrinterConfig`, not an entry of `printer.options`
   * (`internal/domain/config.go`), and control 29 names `printer.template` when it
   * refuses. Written under `printer.options`, the field read empty on a correctly
   * configured station, the fault of control 29 could never be paired with it, and every
   * keystroke added an option the service does not read.
   */
  const TEMPLATE = 'printer.template'

  /** The two offset keys, spelled once. */
  const OFFSET_X = 'printer.options.offset_x'
  const OFFSET_Y = 'printer.options.offset_y'

  /**
   * The lowest offset control 38 accepts, in dots.
   *
   * The control refuses `offset < 0` WHATEVER the template (`internal/domain/config.go`):
   * the floor is the one bound this screen knows without knowing the geometry, and §14.4
   * asks for arrows « bornées par la géométrie ». Below it, the arrow disarms instead of
   * writing a value the save is certain to reject.
   */
  const FLOOR_DOTS = 0

  /** What the screen says when the station refused to render and said nothing readable. */
  const RENDER_REFUSED = 'L’aperçu n’a pas pu être rendu par le poste.'

  /** What it says when the station DID render the address and the browser showed nothing. */
  const IMAGE_UNREADABLE = 'Le poste a rendu l’aperçu, mais le navigateur ne l’a pas affiché.'

  /** Which self-test to print (§8.6). */
  type SelfTest = 'alignment' | 'ruler'

  /** The two self-tests this page offers, and what each button says. */
  const SELF_TESTS: { what: SelfTest; label: string }[] = [
    { what: 'alignment', label: 'Imprimer la mire d’alignement' },
    { what: 'ruler', label: 'Imprimer la réglette' },
  ]

  /** One offset, in printer dots. */
  interface Offset {
    x: number
    y: number
  }

  /** Forces the browser to ask for the image again: the URL has to change to do so. */
  let nonce = $state(1)
  let demo = $state(true)
  let dual = $state(false)
  /** Why the preview is missing, in French. Empty while the image is on screen. */
  let refusal = $state('')
  /** The offsets the image on screen carries, or null while they are not known. */
  let served = $state<Offset | null>(null)
  /** Which self-test is printing right now, or an empty string. */
  let printing = $state<SelfTest | ''>('')

  /**
   * The same value as {@link served}, OUTSIDE reactivity.
   *
   * The effect below writes `served`; comparing against `served` inside it would make the
   * effect depend on what it writes. This mirror is what keeps the comparison honest and
   * the effect free of itself.
   */
  let lastServed: Offset | null = null
  /** True while the station is being asked what the image carries. Outside reactivity too. */
  let asking = false

  const template = $derived(draft.text(TEMPLATE))
  const source = $derived(api.previewURL(template, demo, dual, nonce))
  /**
   * Has the configuration arrived?
   *
   * Until it has, `draft.set` silently drops what is written into a document that is not
   * there yet, and `draft.number` answers zero for every key. A settings page does not
   * guess: it waits, and it says so.
   */
  const configRead = $derived(draft.config !== null)
  const offsetX = $derived(draft.number(OFFSET_X))
  const offsetY = $derived(draft.number(OFFSET_Y))
  const faultX = $derived(faultOf(OFFSET_X))
  const faultY = $derived(faultOf(OFFSET_Y))
  const staleSentence = $derived(staleSentenceOf())
  /** True while an act of this page is in flight: the print buttons disarm. */
  const busy = $derived(admin.busy || printing !== '')

  /**
   * Reads the offsets the preview actually renders, and refreshes the image when they
   * change.
   *
   * A CLEAN draft is, by definition, the configuration in service: that is the only
   * moment where this page knows what the route renders without asking the station. It
   * comes back on every save — `dirty` falls back to false and the document is replaced
   * by the one the station applied — which is exactly when the image has to be requested
   * again, and when a nudge finally becomes visible.
   *
   * The FIRST reading asks for nothing: the `<img>` has just been mounted on that very
   * address, and bumping the nonce here cost a second round trip to redraw the same
   * bitmap at every single mount of the page.
   *
   * A DIRTY draft is the case this effect used to walk away from, and App.svelte unmounts
   * this page on every navigation: going to « Règles » and coming back left the screen
   * showing « 1 dot » beside an image carrying 0, without a word. The document has been
   * written over, so the offsets in service cannot be read from it — they are read from
   * the station, on the open route that serves them.
   */
  $effect(() => {
    if (draft.config === null) return
    if (draft.dirty) {
      if (lastServed === null) void askWhatTheImageCarries()
      return
    }
    const inService: Offset = { x: draft.number(OFFSET_X), y: draft.number(OFFSET_Y) }
    if (lastServed !== null && lastServed.x === inService.x && lastServed.y === inService.y) {
      return
    }
    const first = lastServed === null
    lastServed = inService
    served = inService
    if (!first) refresh()
  })

  /**
   * Asks the station which offsets it is rendering, and says nothing rather than guessing.
   *
   * `GET /admin/api/config` is open (`internal/web/server.go`) and serves the
   * configuration IN SERVICE — the very one the preview draws. It is read here and NOT
   * through `draft.load`, which would throw away what the operator has typed. A station
   * that does not answer leaves {@link served} null, and the banner then says it does not
   * know: an invented zero would be a figure this screen made up.
   */
  async function askWhatTheImageCarries(): Promise<void> {
    if (asking) return
    asking = true
    try {
      const body = await api.fetchConfig()
      const inService: Offset = {
        x: dotsAt(body.config, OFFSET_X),
        y: dotsAt(body.config, OFFSET_Y),
      }
      lastServed = inService
      served = inService
    } catch {
      // Nothing readable came back. The banner says so, which is all that is known.
    } finally {
      asking = false
    }
  }

  /**
   * One number of a document, read by its dotted path; zero when the key is absent.
   *
   * @param document - a configuration exactly as the station serves it.
   * @param path - the dotted key.
   */
  function dotsAt(document: Record<string, unknown>, path: string): number {
    let node: unknown = document
    for (const key of path.split('.')) {
      if (node === null || typeof node !== 'object') return 0
      node = (node as Record<string, unknown>)[key]
    }
    return typeof node === 'number' ? node : 0
  }

  /**
   * Moves one offset by one dot, and never below the floor.
   *
   * It does NOT ask for the image again, and that is the whole point of this page's
   * repair: the route renders the configuration in service, so re-requesting would cost a
   * round trip to redraw the very same bitmap — which is what made four arrows look
   * broken. What changes on screen is the announced gap; the image follows on save.
   *
   * Of the two bounds of control 38, only the floor is knowable from here: the ceiling is
   * `MaxOffsetDots` of the template GEOMETRY, which no route serves. It comes back as a
   * fault of §11.3 on `PUT /admin/api/config`, with the admissible maximum written in its
   * sentence, and that sentence is displayed beside the arrow that caused it. The preview
   * cannot carry it: it renders the offsets IN SERVICE, which have already passed control
   * 38, so it has nothing to refuse about the draft.
   *
   * @param path - the key to move, `printer.options.offset_x` or `…offset_y`.
   * @param step - the number of dots, +1 or -1.
   */
  function nudge(path: string, step: number): void {
    const next = draft.number(path) + step
    if (next < FLOOR_DOTS) return
    draft.set(path, next)
  }

  /** Asks the browser for the image again. */
  function refresh(): void {
    nonce += 1
    refusal = ''
  }

  /**
   * Says why the preview is missing — with the ENGINE's sentence whenever it can.
   *
   * An `<img>` tag does not hand over the body of a refusal: it fires `error` and says
   * nothing more. The same address is therefore asked once again, as JSON, to read the
   * sentence of the 422 — « le décalage sort de la découpe » — which is the only one that
   * says which dot to give back.
   *
   * NOTHING is written before that answer comes back. Posting the fallback sentence up
   * front accused the station of a refusal it had not uttered, and left that accusation on
   * screen when the second request came back with a perfectly rendered image.
   *
   * @param url - the address that failed, kept to drop an answer that arrived too late.
   */
  async function explain(url: string): Promise<void> {
    const sentence = await refusalOf(url)
    // A newer request has already replaced the image: its refusal, or its success, is the
    // one that counts.
    if (url !== source) return
    refusal = sentence
  }

  /**
   * What the station answers about an address the browser could not display.
   *
   * @param url - the address to ask about, as JSON this time.
   */
  async function refusalOf(url: string): Promise<string> {
    try {
      const response = await fetch(url, { headers: { accept: 'application/json' } })
      if (response.ok) return IMAGE_UNREADABLE
      const problem = JSON.parse(await response.text()) as { message?: string }
      if (typeof problem.message === 'string' && problem.message !== '') return problem.message
      return RENDER_REFUSED
    } catch {
      // The station answered nothing readable at all: the fallback sentence says what is
      // known, and inventing a cause would say more than that.
      return RENDER_REFUSED
    }
  }

  /**
   * What the screen owes the operator while the draft and the image may disagree.
   *
   * Empty when they agree — which is the nominal case — so that nothing is said for the
   * sake of saying it. Not empty when the offsets in service are UNKNOWN and the document
   * has been edited: « I do not know yet » is the only true thing to say there, and it is
   * worth more than a silence that lets a figure on screen pass for the one on the label.
   */
  function staleSentenceOf(): string {
    if (served === null) {
      if (!draft.dirty) return ''
      return (
        'Cet écran ne sait pas encore quel décalage l’image ci-dessous porte : elle rend le ' +
        'décalage ENREGISTRÉ, et la configuration a été modifiée sans être enregistrée. ' +
        'Enregistrez-la pour que les deux coïncident.'
      )
    }
    if (served.x === offsetX && served.y === offsetY) return ''
    return (
      `L’image ci-dessous porte le décalage ENREGISTRÉ : ${dots(served.x)} en horizontal, ` +
      `${dots(served.y)} en vertical. Le réglage en cours vaut ${dots(offsetX)} et ` +
      `${dots(offsetY)} — enregistrez la configuration pour le voir sur l’étiquette.`
    )
  }

  /** An offset as it is read out loud: « 0 dot », « 1 dot », « 2 dots ». */
  function dots(value: number): string {
    return `${frenchInteger(value)} dot${Math.abs(value) >= 2 ? 's' : ''}`
  }

  /**
   * Writes a number the operator typed, and writes NOTHING when the field is empty.
   *
   * `Number('')` is 0. Clearing « Exemplaires » therefore wrote `copies: 0` — a station
   * that prints nothing at all, saved by a keystroke that looked like an erasure rather
   * than an edit. An emptied field keeps what the file holds.
   *
   * @param path - the key to write.
   * @param raw - what the field carries, exactly as it was typed.
   */
  function writeNumber(path: string, raw: string): void {
    const value = Number(raw)
    if (raw.trim() === '' || Number.isNaN(value)) return
    draft.set(path, value)
  }

  /** The message of the control that refused this key, empty when there is none (§11.3). */
  function faultOf(path: string): string {
    return draft.faults.find((fault) => fault.field === path)?.message ?? ''
  }

  /**
   * Prints one of the self-tests of §8.6 — a PROTECTED act that costs a label.
   *
   * `POST /admin/api/printer/test` is in the guarded table of `internal/web/server.go`.
   * Passed through `admin.run`, an expired session answered a bare 401 in the banner: no
   * password panel, no replay, and a volunteer left in front of a printer that had not
   * moved. `admin.protect` asks for the password AT THE MOMENT OF ACTING and passes the
   * call a second time.
   *
   * @param what - which of the two self-tests.
   */
  async function selfTest(what: SelfTest): Promise<void> {
    if (busy) return
    printing = what
    // What an act leaves on screen lives until ANOTHER act replaces it: `protect` does not
    // clear it by itself, and a refusal from a minute ago would sit above the sentence
    // this one is about to write.
    admin.actionError = ''
    admin.notice = ''
    try {
      const done = await admin.protect(() => api.printerSelfTest(what))
      if (done === null) return
      admin.notice = done.message
      await admin.refresh()
    } finally {
      printing = ''
    }
  }
</script>

<div class="pages">
  <Panel
    title="Aperçu de l’étiquette"
    note="Le même moteur que l’impression (A2) : le décalage se voit parce qu’il est cuit dans le bitmap. L’image porte le gabarit en cours d’édition, mais le décalage ENREGISTRÉ — jamais celui que les flèches sont en train de régler."
  >
    {#if staleSentence !== ''}
      <p class="stale" data-preview-stale>{staleSentence}</p>
    {/if}

    <div class="preview">
      <div class="sheet">
        <img
          src={source}
          alt="Aperçu de l’étiquette telle qu’elle serait imprimée"
          onload={() => (refusal = '')}
          onerror={() => void explain(source)}
        />
        {#if refusal !== ''}
          <p class="refused" data-preview-refused>{refusal}</p>
        {/if}
      </div>

      <div class="nudges">
        <p class="axis">
          <span>Décalage horizontal</span>
          <strong data-offset-x>{dots(offsetX)}</strong>
        </p>
        <div class="pair">
          <Act
            label="← 1 dot"
            disabled={!configRead || offsetX <= FLOOR_DOTS}
            onrun={() => nudge(OFFSET_X, -1)}
          />
          <Act label="1 dot →" disabled={!configRead} onrun={() => nudge(OFFSET_X, 1)} />
        </div>
        {#if faultX !== ''}
          <p class="fault" data-fault-offset-x>{faultX}</p>
        {/if}

        <p class="axis">
          <span>Décalage vertical</span>
          <strong data-offset-y>{dots(offsetY)}</strong>
        </p>
        <div class="pair">
          <Act
            label="↑ 1 dot"
            disabled={!configRead || offsetY <= FLOOR_DOTS}
            onrun={() => nudge(OFFSET_Y, -1)}
          />
          <Act label="1 dot ↓" disabled={!configRead} onrun={() => nudge(OFFSET_Y, 1)} />
        </div>
        {#if faultY !== ''}
          <p class="fault" data-fault-offset-y>{faultY}</p>
        {/if}

        <p class="bound">
          Le décalage ne descend pas sous zéro : le poste refuse un décalage négatif quel que
          soit le gabarit. Le maximum, lui, dépend de la géométrie du gabarit ; il est annoncé
          ici si l’enregistrement le dépasse.
        </p>

        <label class="check">
          <input type="checkbox" bind:checked={demo} onchange={() => refresh()} />
          <span>Valeurs de démonstration</span>
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={dual} onchange={() => refresh()} />
          <span>Deux tarifs — le cas le plus chargé</span>
        </label>

        {#if !configRead}
          <p class="waiting" data-reading>
            Lecture de la configuration en cours… les flèches attendent qu’elle soit arrivée.
          </p>
        {/if}
      </div>
    </div>

    <p class="truncation">
      Le symbole code-barres est volontairement tronqué : un symbole conforme n’entre pas
      sur 40 × 25 mm avec les cinq champs texte. Ce n’est pas un défaut de rendu et il n’y
      a rien à corriger.
    </p>
    <!--
      §14.4 asks for the FIGURES of the symbol here — the numbered banner of `Diagnose()`.
      No route carries them and no DTO has a field for them, so the page says so instead
      of drawing a number it would have made up.
    -->
    <p class="absent" data-diagnose-absent>
      Le détail chiffré du symbole — largeur de module, modules rendus, modules attendus —
      n’est pas servi par ce poste. Cet écran n’affiche pas un chiffre qu’il aurait deviné.
    </p>
  </Panel>

  <Panel title="Gabarit et impression">
    <Field
      label="Gabarit"
      path={TEMPLATE}
      value={template}
      hint="Le gabarit reproduit à l’identique s’appelle weighing_identical (A1)."
      fault={faultOf(TEMPLATE)}
      disabled={!configRead}
      onchange={(value) => {
        draft.set(TEMPLATE, value)
        refresh()
      }}
    />
    <Field
      label="Noircissement"
      path="printer.options.darkness"
      kind="number"
      value={draft.text('printer.options.darkness')}
      hint="Trop bas, l’étiquette pâlit au soleil ; trop haut, elle bave et le scanner refuse."
      fault={faultOf('printer.options.darkness')}
      disabled={!configRead}
      onchange={(value) => writeNumber('printer.options.darkness', value)}
    />
    <Field
      label="Vitesse"
      path="printer.options.speed"
      kind="number"
      value={draft.text('printer.options.speed')}
      fault={faultOf('printer.options.speed')}
      disabled={!configRead}
      onchange={(value) => writeNumber('printer.options.speed', value)}
    />
    <Field
      label="Exemplaires"
      path="printer.options.copies"
      kind="number"
      value={draft.text('printer.options.copies')}
      hint="Un client repart avec une étiquette : deux exemplaires se justifient, pas se devinent."
      fault={faultOf('printer.options.copies')}
      disabled={!configRead}
      onchange={(value) => writeNumber('printer.options.copies', value)}
    />
    <div class="actions">
      <!--
        Neutres, comme les trois auto-tests de la page Matériel : un auto-test INTERROGE le
        poste, il ne change ni ce qu'il vend ni la façon dont il pèse. Ce qu'il coûte — une
        étiquette de la bobine — est dit par la phrase juste en dessous, et la densité de
        44 px de cette page est ce qu'ADR-033 lui laisse.
      -->
      {#each SELF_TESTS as test (test.what)}
        <Act
          act={test.what}
          label={test.label}
          protected
          busy={printing === test.what}
          disabled={busy}
          onrun={() => void selfTest(test.what)}
        />
      {/each}
    </div>
    <p class="cost">
      Chaque appui sort une étiquette pour de bon : le mot de passe est demandé au moment
      d’imprimer, et l’impression repart d’elle-même une fois la session ouverte.
    </p>
  </Panel>
</div>

<style>
  /*
   * The density is the one of the administration (ADR-033): 44 px on the form controls,
   * and NO 72 px target on this page. Nothing here changes what the station sells nor the
   * way it weighs — a mis-touched self-test costs one label, and the arrows are read with
   * a mouse in one hand and a printed label in the other.
   */
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  /* The gap between what is being adjusted and what the image shows. */
  .stale {
    margin: 0 0 0.75rem;
    padding: 0.5rem 0.75rem;
    background: var(--warning-wash);
    border-left: 0.375rem solid var(--warning);
    border-radius: var(--radius-sm);
    font-size: 1.0625rem;
  }

  .preview {
    display: flex;
    flex-wrap: wrap;
    gap: 1.5rem;
    align-items: flex-start;
  }

  /* A neutral wash under the label, so that a white sheet on a white panel stops
     dissolving into it. */
  .sheet {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 0;
    padding: 0.75rem;
    background: var(--waiting-wash);
    border-radius: var(--radius-lg);
  }

  /* The preview is enlarged and kept at SHARP pixels: at 203 dpi a 40 × 25 mm label is
     320 × 200 pixels, and smoothing would hide the very dot one came to adjust. */
  img {
    width: 40rem;
    max-width: 100%;
    image-rendering: pixelated;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--shadow-1);
  }

  /* What the 422 said, where the image would have been. */
  .refused {
    margin: 0;
    padding: 0.5rem 0.75rem;
    background: var(--fault-wash);
    border-left: 0.375rem solid var(--fault);
    border-radius: var(--radius-sm);
    font-size: 1.0625rem;
  }

  .nudges {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 0;
  }

  .axis {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
    margin: 0;
    font-size: 1.125rem;
  }

  .pair {
    display: flex;
    gap: var(--touch-gap);
  }

  /* The sentence of the control that refused an offset, beside the arrow that wrote it. */
  .fault {
    margin: 0;
    padding-left: 0.5rem;
    border-left: 0.25rem solid var(--fault);
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .check {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    min-height: 2.75rem;
    font-size: 1.0625rem;
  }

  .check input {
    width: 1.5rem;
    height: 1.5rem;
    flex: 0 0 auto;
  }

  .truncation,
  .absent,
  .waiting,
  .bound,
  .cost {
    margin: 1rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .absent {
    margin-top: 0.5rem;
  }

  .bound {
    margin-top: 0.25rem;
  }

  .cost {
    margin-top: 0.5rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    margin-top: 0.75rem;
  }
</style>
