<script lang="ts">
  import { fetchCatalog } from '../../lib/api'
  import {
    ALL_CATEGORIES,
    filterProducts,
    type Catalog,
    type Presentation,
    type Product,
  } from '../../lib/catalog'
  import Tile from '../../components/Tile.svelte'
  import { GRID_COLUMNS_AUTO, gridTemplateColumns } from '../../lib/grid'
  import {
    NAME_SIZE_MIN_PX,
    canvasMeasurer,
    fitNameSize,
    nameSizeCeiling,
  } from '../../lib/typography'
  import Act from '../components/Act.svelte'
  import CatalogSourcePanel from '../components/CatalogSourcePanel.svelte'
  import DecisionsInForcePanel from '../components/DecisionsInForcePanel.svelte'
  import FindingsPanel from '../components/FindingsPanel.svelte'
  import ImportHistoryPanel from '../components/ImportHistoryPanel.svelte'
  import Inventory from '../components/Inventory.svelte'
  import Panel from '../components/Panel.svelte'
  import Toggle from '../components/Toggle.svelte'
  import * as api from '../lib/api'
  import { decisionActs, decisionSentence, waiverTyped } from '../lib/decisions'
  import type { Draft } from '../lib/draft.svelte'
  import type { FindingDTO, HealthDTO, ImportDTO, ReloadDTO } from '../lib/dto'
  import { frenchInteger } from '../lib/format'
  import {
    byUnitSentence,
    gridSentences,
    otherScreenWarning,
    type ScreenCount,
  } from '../lib/grid-count'
  import { preferences } from '../lib/preferences.svelte'
  import { productNameOf, type ReadState } from '../lib/read-state'
  import { ReloadWatch } from '../lib/reload.svelte'
  import type { Admin } from '../lib/session.svelte'
  import { matchTally, withdrawnSentence } from '../lib/tally'

  /**
   * The Catalogue page of §14.4.
   *
   * What is left here is what only this page can hold: the acts ADR-033 protects, and the
   * grid preview, whose numbers are asked of the browser rather than computed. Everything
   * a reader recognises as a subject of its own — where the catalog comes from, the three
   * lists of findings, the decisions in force, the history — is a component of
   * `admin/components/`, and every sentence that is arithmetic and grammar rather than
   * layout is a module of `admin/lib/`.
   *
   * Six things this page owes ADR-033 and §14.4, and had not:
   *
   *  1. every list is CAPPED and says what the cap hides — a screen reading « 20
   *     anomalies » on a file carrying 116 makes whoever fixes the twenty believe the
   *     work done;
   *  2. « Le proposer de nouveau » is REACHABLE: the client catalog does not serve a
   *     product a human refused, so the search could never find it again, and the one
   *     button that undoes a withdrawal was unreachable from the moment it mattered;
   *  3. the withdrawal and the waiver are TWO acts on two columns of one row (§14.5),
   *     and sending them together erased a waiver every time somebody withdrew a
   *     product — and granted one every time the field held a leftover figure;
   *  4. dropping a CSV and deciding about a product CHANGE WHAT THE STATION SELLS: they
   *     go through {@link Admin.protect}, which asks for the password at the moment of
   *     the act and replays it (ADR-033);
   *  5. nothing here states a fact this page has not READ. A station with no journal
   *     (ADR-013) answers 503 to every read of the history, and four « Aucun… » sentences
   *     kept saying otherwise for ever — « Aucune anomalie sur le dernier import. » is the
   *     one sentence somebody is relieved to read;
   *  6. no button is armed that the station is CERTAIN to refuse: it asks for the password
   *     first, and the reward for typing it is a red banner.
   */
  interface Props {
    admin: Admin
    /**
     * The configuration document: this page edits the `catalog` block, and the one
     * `ui` key that decides WHAT THE GRID SHOWS.
     *
     * That key sits in `ui` and not in `catalog` on purpose: a change to the catalog
     * block runs the disk probe and restarts the catalog source, which is a heavy price
     * for a display choice — and it would fail outright while a WebDAV share is
     * momentarily unreachable. The draft is shared by every page and the save is global,
     * so hosting the switch here couples no blocks together.
     */
    draft: Draft
    health: HealthDTO
  }

  const { admin, draft, health }: Props = $props()

  /** How many products the search draws. Past that, the search gets narrowed. */
  const MATCHES_SHOWN = 20

  /** The reasons that describe a product WITH NO TILE, and that are not faults. */
  const NOT_WEIGHABLE_CODES = [
    'NO_BARCODE',
    'PREPACKAGED_PRODUCT',
    'INTERNAL_CODE_NOT_WEIGHABLE',
  ]

  /** The key that decides how many columns the client grid draws. */
  const GRID_COLUMNS_PATH = 'ui.grid_columns'

  /**
   * The counts an operator may pin the grid to.
   *
   * Guard rails and not a calculation: the same N is comfortable on a 4K and absurd
   * on a 15", so no pair of bounds is true of the whole estate. What protects the
   * operator is the sentence below, which shows the result BEFORE the save.
   */
  const GRID_COLUMNS_CHOICES = [3, 4, 5, 6, 7, 8, 9, 10, 11, 12]

  /** The key that decides from how many tiles a category gets its filter chip. */
  const CHIP_THRESHOLD_PATH = 'ui.min_products_for_chip'

  let imports = $state<ImportDTO[]>([])
  let findings = $state<FindingDTO[]>([])
  /**
   * The catalog IN SERVICE, whole, or null while it has not been read.
   *
   * Whole and not just its products: the grid preview below draws a REAL tile, and a tile
   * carries the tiers, the primary code and the price switch of this station. A preview
   * built from a tile of its own would be a second drawing to keep in step with the first.
   */
  let served = $state<Catalog | null>(null)
  const products = $derived<Product[]>(served?.products ?? [])

  /**
   * What this station publishes of its screen settings, empty until it has been read.
   *
   * `Partial` although the DTO declares every field: this page also runs against a
   * station whose binary predates one of them, and reading through an absent block takes
   * the WHOLE administration screen down. It is the same guard, for the same reason, as
   * the `?? []` {@link Draft.load} keeps over a list the service no longer serves null.
   */
  const presentation = $derived<Partial<Presentation>>(served?.presentation ?? {})
  const primaryCode = $derived<string>(served?.pricing?.primary_code ?? '')
  let query = $state('')
  /** The Odoo id of the product being decided about, or an empty string. */
  let chosenID = $state('')
  let reason = $state('')
  let waiver = $state('')
  /** True while a CSV is hovering over the drop zone. */
  let dropping = $state(false)
  /** What the last CSV drop answered, in French. */
  let report = $state('')
  /** Which protected act is in flight, or an empty string. */
  let working = $state('')
  /** Whether the import history and its findings have been READ. */
  let historyState = $state<ReadState>('loading')
  /** Whether the client catalog has been read, so a missing name can be explained. */
  let namesState = $state<ReadState>('loading')

  const busy = $derived(admin.busy || working !== '')

  const anomalies = $derived(findings.filter((finding) => finding.issue === 'anomaly'))
  const neutral = $derived(findings.filter((finding) => NOT_WEIGHABLE_CODES.includes(finding.code)))
  const mismatches = $derived(findings.filter((finding) => finding.code === 'UNIT_MISMATCH'))

  /** The name of every product the station serves, by Odoo id. */
  const names = $derived(
    new Map(products.map((product): [string, string] => [product.id, product.name])),
  )

  /**
   * How many products of the catalog IN SERVICE are sold by unit.
   *
   * Derived, never written down. Without this figure, the gap between the « 331
   * pesables » of the last import above and the « 316 produits pesables » the client
   * screen states at the bottom of its grid is explainable by nobody over the phone —
   * and a hard-coded 15 would pass a rendering test while lying at the next import.
   *
   * The client route serves EVERY weighable tile whatever this station shows, so the
   * count is right in both positions of the switch.
   */
  const byUnitCount = $derived(products.filter((product) => product.mode === 'by_unit').length)

  const byUnitSaid = $derived(
    byUnitSentence(namesState, byUnitCount, draft.flag('ui.show_by_unit_products')),
  )

  /** The column setting AS DRAFTED, `0` for as long as nobody has pinned one. */
  const gridColumns = $derived(draft.number(GRID_COLUMNS_PATH))

  /** The chip threshold AS DRAFTED. */
  const chipThreshold = $derived(draft.number(CHIP_THRESHOLD_PATH))

  /**
   * The tiles the grid would draw at the DRAFT, switch above included.
   *
   * Both settings of this panel act on the same grid, so a count that ignored the
   * switch would announce sixteen screens' worth of tiles fifteen of which the
   * station has just been told to hide.
   */
  const draftTiles = $derived(
    draftedFlag('ui.show_by_unit_products', presentation.show_by_unit_products ?? false)
      ? products
      : products.filter((product) => product.mode !== 'by_unit'),
  )

  /**
   * The tile the preview draws, or null while there is nothing to draw one from.
   *
   * A REAL tile, and that is the whole point: `--tile-height` leaves the price block out
   * of its `calc`, whose bodies do not shrink with the tile — a probe of that token
   * announced 189 px where the browser drew 245, and the screen would have promised three
   * rows of a grid showing two. What decides how many rows fit is what the browser draws,
   * so the browser is what gets asked.
   *
   * Its NAME is cut down to one glyph and its photo dropped. Neither changes the height —
   * the name block is a box of fixed height (ADR-030) and a plate is the same size with a
   * letter as with a photo — but a name that overflowed would measure THAT tile instead of
   * the row every tile shares, and a photo would fetch an image to measure nothing.
   */
  const sampleTile = $derived<Product | null>(
    draftTiles[0] === undefined
      ? null
      : // `prices ?? []` for the same reason the presentation above is `Partial`: an
        // absent list is what a tile iterates over, and taking the administration screen
        // down over a preview would be the worst possible trade.
        { ...draftTiles[0], name: '·', image_url: '', prices: draftTiles[0].prices ?? [] },
  )

  /**
   * A presentation flag as the DRAFT would leave it, falling back on what is served.
   *
   * `draft.flag` answers false for a key the file does not carry, and « absent » is not
   * « false »: the station has its own default for each of these, and a preview built on
   * false would draw a tile with no price block on a station that prints one.
   *
   * @param path - the dotted path of the key.
   * @param served - what the catalog in service says of it.
   */
  function draftedFlag(path: string, served: boolean): boolean {
    return draft.value(path) === undefined ? served : draft.flag(path)
  }

  /** The box the probes live in: it carries the deduced `--tile-scale`. */
  let probes = $state<HTMLElement | null>(null)
  /** The client grid, declared as the draft would draw it, holding one real tile. */
  let gridProbe = $state<HTMLElement | null>(null)
  /** The cell that tile sits in: stretched by the grid, so its height IS the row's. */
  let rowProbe = $state<HTMLElement | null>(null)
  /** A box of `--tile-min`: the width the automatic density is calibrated on. */
  let calibrationProbe = $state<HTMLElement | null>(null)
  /** The height the grid occupies at the client, its three bars taken off. */
  let viewportProbe = $state<HTMLElement | null>(null)

  /** Bumped on every resize, so the count follows the window it is read in. */
  let resized = $state(0)

  let screen = $state<ScreenCount | null>(null)

  $effect(() => {
    const bump = (): void => {
      resized += 1
    }
    window.addEventListener('resize', bump)
    return () => window.removeEventListener('resize', bump)
  })

  $effect(() => {
    // Named rather than read inside the call: what re-runs this measurement is the
    // draft and the size of the window, and nothing else.
    const wanted = gridColumns
    void resized
    screen = measureGrid(wanted)
  })

  /**
   * How many names come out at the floor, and how many rows carry one.
   *
   * `null` and not zero when nothing could be measured: « aucun nom n'atteint le
   * plancher » is a fine piece of news, and it would be false on every browser with
   * no canvas.
   */
  const floorReached = $derived.by<{ names: number; rows: number } | null>(() => {
    const layout = screen
    if (layout === null || layout.contentWidthPx <= 0 || layout.nameBoxPx <= 0) return null
    if (draftTiles.length === 0) return null
    const face = tileNameFace()
    if (face === null) return null
    const measure = canvasMeasurer(face.family, face.weight)
    if (measure === null) return null
    // The ceiling is the one the scaled tile gives — starting every name at 34 px on a
    // tile that is not `--tile-min` wide would count a floor nobody reaches.
    const ceiling = nameSizeCeiling(layout.tileScale)
    const rows = new Set<number>()
    let names = 0
    for (const [index, product] of draftTiles.entries()) {
      const body = fitNameSize(
        product.name,
        layout.contentWidthPx,
        measure,
        layout.nameBoxPx,
        ceiling,
      )
      if (body > NAME_SIZE_MIN_PX) continue
      names += 1
      rows.add(Math.floor(index / layout.columns))
    }
    return { names, rows: rows.size }
  })

  /** What the draft grid comes to, in French, and what it costs. */
  const gridLines = $derived(
    gridSentences({
      columns: gridColumns,
      layout: screen,
      tileCount: draftTiles.length,
      floor: floorReached,
      viewport: { width: window.innerWidth, height: window.innerHeight },
    }),
  )

  /**
   * The sentence that keeps « sur cet écran » honest, empty when it is.
   *
   * It is born WITH the count it qualifies: a screen that has measured nothing has
   * nothing to relativise.
   */
  const otherScreen = $derived(
    screen === null ? '' : otherScreenWarning(window.location.hostname),
  )

  /**
   * Asks the LAYOUT what the draft grid comes to, and answers null when it cannot.
   *
   * Not one number here is arithmetic run in parallel with the browser: how many
   * columns `auto-fill` makes of `clamp()` on this screen is known to nobody else,
   * and the three bars the grid shares its height with are read from their own
   * tokens — the day one of them changes height, this count follows without
   * anybody thinking about it.
   *
   * The declaration is written here rather than in the markup so that the whole
   * measurement happens in one pass: the column width has to be known before the
   * scale can be set, and the scale before the tile has a height.
   *
   * @param columns - the draft setting.
   */
  function measureGrid(columns: number): ScreenCount | null {
    const box = probes
    const gridBox = gridProbe
    const row = rowProbe
    const calibration = calibrationProbe
    const viewport = viewportProbe
    if (box === null || gridBox === null || calibration === null || viewport === null) return null
    // No tile to draw is no measurement: a preview without the price block, on a station
    // that prints one, is exactly the 30 % error this probe exists to stop making.
    if (row === null) return null

    gridBox.style.gridTemplateColumns = gridTemplateColumns(columns)
    const tracks = trackWidths(gridBox)
    const columnWidth = tracks[0]
    if (columnWidth === undefined) return null

    // The factor is DEDUCED and never set by hand: the column the browser gave over
    // the width `--tile-min` calibrates. Automatic keeps the grid of today, so it
    // keeps the factor of today, and nothing of what follows takes place.
    //
    // `data-tile-scale` on that box is what makes this line do anything at all: a custom
    // property has its `var()` substituted AT THE ELEMENT THAT DECLARES IT, so without
    // the selector `app.css` provides, this box would inherit four tokens already
    // computed against the root's factor of 1 — and every count would be the automatic
    // one, whatever the draft says, silently.
    const calibrationWidth = calibration.clientWidth
    const scale =
      columns === GRID_COLUMNS_AUTO || calibrationWidth <= 0 ? 1 : columnWidth / calibrationWidth
    box.style.setProperty('--tile-scale', String(scale))

    const gap = Number.parseFloat(getComputedStyle(gridBox).rowGap)
    const available = viewport.clientHeight
    // The CELL and not the tile: the grid stretches it to `minmax(var(--tile-height),
    // auto)`, so its height is the row's — token floor and overflowing content both.
    const rowHeight = row.offsetHeight
    const nameBox = row.querySelector<HTMLElement>('.name-box')
    if (!Number.isFinite(gap) || available <= 0 || rowHeight <= 0 || nameBox === null) return null

    return {
      columns: tracks.length,
      rows: Math.max(1, Math.floor((available + gap) / (rowHeight + gap))),
      contentWidthPx: nameBox.clientWidth,
      nameBoxPx: nameBox.clientHeight,
      tileScale: scale,
    }
  }

  /**
   * The width of every column the browser laid out, in px, or nothing.
   *
   * `getComputedStyle` gives back RESOLVED tracks — « 352px 352px … » — where there
   * is a layout, and the declaration itself where there is none. Accepting px and
   * nothing else is what turns « jsdom lays nothing out » into a fallback instead of
   * a wrong number: `repeat(auto-fill, …)` read back verbatim is not a count.
   *
   * @param gridBox - the grid probe, already carrying the draft declaration.
   */
  function trackWidths(gridBox: HTMLElement): number[] {
    const tracks = getComputedStyle(gridBox)
      .gridTemplateColumns.split(/\s+/u)
      .filter((track) => track !== '')
    if (tracks.length === 0) return []
    const widths = tracks.map((track) =>
      track.endsWith('px') ? Number.parseFloat(track) : Number.NaN,
    )
    return widths.every((width) => Number.isFinite(width) && width > 0) ? widths : []
  }

  /**
   * The face a tile name is drawn with, asked of the layout, or null.
   *
   * Read off the REAL name of the probe tile rather than declared here: what a canvas
   * is asked to measure with is then what the browser will draw with, family and weight
   * both — the mistake `Grid.svelte` documents at length about a font that had not
   * loaded yet, made once more here would size names against a face nobody renders.
   */
  function tileNameFace(): { family: string; weight: number } | null {
    const name = rowProbe?.querySelector<HTMLElement>('.name') ?? null
    if (name === null) return null
    const style = getComputedStyle(name)
    const weight = Number.parseInt(style.fontWeight, 10)
    if (style.fontFamily === '' || !Number.isFinite(weight)) return null
    return { family: style.fontFamily, weight }
  }

  /**
   * The products the search retains, and how many there are BEFORE the cap.
   *
   * This is the SAME filter as the customer grid — accent-blind, one word per typed
   * fragment — because a product unfindable here would be unfindable there, and a second
   * normalisation in the browser is exactly what the shared `web/testdata/normalization.json`
   * exists to prevent.
   */
  const found = $derived(
    query === '' ? [] : filterProducts(products, ALL_CATEGORIES, query),
  )
  const matches = $derived(found.slice(0, MATCHES_SHOWN))
  const matchesSaid = $derived(matchTally(matches.length, found.length))

  /** The human decisions in force, most recent first. */
  const decisions = $derived(
    [...health.decisions].sort((a, b) => b.decided_at.localeCompare(a.decided_at)),
  )

  /** The decision in force about the chosen product, or null when there is none. */
  const chosenDecision = $derived(
    chosenID === '' ? null : (decisions.find((d) => d.product_id === chosenID) ?? null),
  )

  /**
   * Whether the chosen product is offered today, and the waiver it already carries.
   *
   * Both are read from the decision IN FORCE and never from the form: they are the two
   * columns the other act must carry unchanged (§14.5).
   */
  const offeredInForce = $derived(chosenDecision === null ? true : chosenDecision.offered)
  const waiverInForce = $derived(chosenDecision?.min_weight_g ?? null)

  /** The waiver typed in the form, and whether it can travel. */
  const typedWaiver = $derived(waiverTyped(waiver))

  /** The motive as it will TRAVEL: trimmed, because a space is not an explanation. */
  const motive = $derived(reason.trim())

  /** Which of the four acts of a decision the station would accept, as things stand. */
  const acts = $derived(
    decisionActs({ motive, offeredInForce, waiverInForce, typedWaiver }),
  )

  /** How many products the last import withdrew, and since which import (§10.9). */
  const withdrawnCount = $derived(health.catalog?.products_withdrawn_count ?? 0)
  /**
   * The last import that actually PUT A CATALOG IN SERVICE before the one in force.
   *
   * `result` is not decoration: the history carries the refused, the failed and the
   * unchanged too, and dating « 4 produits retirés » from a file the station discarded
   * names an import that never served anything. What a withdrawal is measured against is
   * the last APPLIED import — `withdrawUnseen`, internal/store/catalog.go.
   */
  const previousImport = $derived(
    imports.find(
      (record) =>
        health.catalog !== null && record.id < health.catalog.id && record.result === 'applied',
    ) ?? null,
  )
  const withdrawnSaid = $derived(
    withdrawnSentence(withdrawnCount, previousImport?.occurred_at ?? ''),
  )

  /**
   * The import whose findings are on screen, `undefined` before the first read.
   *
   * Deliberately NOT a rune: the effect below both reads and writes it, and a `$state`
   * would make that effect depend on its own write.
   */
  let shownImportID: number | null | undefined

  /**
   * Re-reads the findings whenever the station changes import.
   *
   * They used to be read ONCE, at mount, and then contradicted by everything around them.
   * « Recharger le catalogue » refreshed `health` — so the « Dernier import » box described
   * import N+1 — while « Anomalies à corriger dans Odoo » still listed the sixteen
   * anomalies of import N, and whoever had just corrected them in Odoo read their work as
   * vain. A dropped CSV is the same case seen from the other end: the station answers « il
   * prend service dans quelques secondes », so a read fired at that instant lands BEFORE
   * the import it is meant to show. The dashboard is polled every three seconds, and the id
   * of the import in force is what says the wait is over.
   */
  $effect(() => {
    const id = health.catalog?.id ?? null
    if (id === shownImportID) return
    shownImportID = id
    void load()
  })

  /**
   * What « Recharger le catalogue » left pending, and the sentence that will conclude it.
   *
   * The very mechanism above, put to a second use: the reload answers a 202 that carries
   * no outcome, so the id of the import in force is again what says the wait is over. The
   * wait itself lives in a shared module because the SAME act is reachable from the
   * troubleshooting page, and it may not read differently there.
   */
  const reloadWatch = new ReloadWatch()

  $effect(() => {
    reloadWatch.observe(health)
  })

  /**
   * Reads the import history, the findings IN FORCE, and the catalog in service.
   *
   * « In force » and not « of the last import », because the two part company on the most
   * ordinary event there is: a producer who drops a byte-identical export a second night
   * has that file recorded « inchangé », it swaps nothing, and it writes no finding of its
   * own — they belong to the import that produced the grid, one row above. The station
   * names the row to read (`catalog_findings_id`), because it is the only side that can
   * see past the twenty imports this page is served.
   */
  async function load(): Promise<void> {
    const shown = health.catalog_findings_id
    const history = await admin.load(() => api.fetchImports(shown === 0 ? undefined : shown))
    if (history === null) {
      historyState = 'unread'
    } else {
      imports = history.imports
      findings = history.findings
      historyState = 'read'
    }
    // The whole catalog comes from the CLIENT route, which asks for no password and which
    // the browser may already hold in cache: it is the only way to name a product in a
    // local decision without inventing a second search route. The Rules page does the same.
    const catalog = await admin.load(() => fetchCatalog())
    if (catalog === null) {
      namesState = 'unread'
      return
    }
    served = catalog
    namesState = 'read'
  }

  /**
   * Runs one PROTECTED act, and keeps the French sentence the station answered.
   *
   * ADR-033 protects what changes what the station sells or how it weighs, and every act
   * of this page is in that set: a reload republishes the grid, forgetting the quarantine
   * lets a refused file back in, a dropped CSV replaces the grid outright, and a decision
   * takes a product out of it. `admin.run` would have shown a bare 401 instead of asking
   * for the password.
   *
   * @param label - the act, so its own button can say « en cours ».
   * @param action - the protected call, which may be passed TWICE.
   * @returns true when the station accepted it.
   */
  async function guarded<T extends { message: string }>(
    label: string,
    action: () => Promise<T>,
  ): Promise<T | null> {
    working = label
    // What an act leaves on screen lives until ANOTHER act replaces it (§5.1), and
    // starting this one is that replacement: `protect` does not clear it by itself, so a
    // refusal from a minute ago would sit above the sentence this act is about to write.
    admin.actionError = ''
    admin.notice = ''
    reloadWatch.forget()
    try {
      const done = await admin.protect(action)
      if (done === null) return null
      admin.notice = done.message
      await admin.refresh()
      return done
    } finally {
      working = ''
    }
  }

  /**
   * Reloads the catalog through the EXPERT door, and waits for the same outcome the
   * troubleshooting page waits for.
   *
   * The answer is a 202 and carries no outcome — the watch reads, qualifies and applies on
   * its own thread — so what the screen shows a few seconds later comes from the polling,
   * word for word the sentence the other door writes. An act cannot announce itself
   * differently depending on the screen it is reached from.
   */
  async function reloadCatalog(): Promise<void> {
    const answer: ReloadDTO | null = await guarded('reload', api.reloadCatalogAsExpert)
    if (answer !== null) reloadWatch.begin(answer)
  }

  /**
   * The name of a product, or an honest sentence when this page cannot give one.
   *
   * @param id - the Odoo id of the product.
   */
  function nameOf(id: string): string {
    return productNameOf(names.get(id), namesState, 'Nom inconnu : le catalogue n’a pas pu être lu')
  }

  /**
   * Opens a product for decision, wherever it was picked from.
   *
   * The waiver field opens on the waiver IN FORCE and not empty: an empty field in front
   * of a product that already weighs from 8 g reads as « no waiver », which is false.
   *
   * @param id - the Odoo id of the product.
   */
  function choose(id: string): void {
    chosenID = id
    reason = ''
    const grams = decisions.find((d) => d.product_id === id)?.min_weight_g ?? null
    waiver = grams === null ? '' : String(grams)
  }

  /**
   * Writes the OFFERED column, and it alone (§10.6, ADR-017).
   *
   * The waiver travels UNCHANGED, read back from the decision in force. Both columns live
   * in one row of `local_decisions`, so a call that took the grams from the form erased a
   * waiver every time somebody withdrew a product.
   *
   * @param offered - whether the product goes back into the grid.
   */
  async function setOffered(offered: boolean): Promise<void> {
    if (chosenID === '') return
    const id = chosenID
    const grams = waiverInForce
    const text = motive
    const written = await guarded('offered', () =>
      api.saveDecision(id, { offered, min_weight_g: grams, reason: text }),
    )
    if (written !== null) reason = ''
  }

  /**
   * Writes the WAIVER column, and it alone (§10.6).
   *
   * Symmetrically, the offered column travels unchanged: granting a waiver must not put
   * back into the grid a product a human took out of it.
   *
   * @param grams - the floor this product may weigh, or null to drop the waiver.
   */
  async function setWaiver(grams: number | null): Promise<void> {
    if (chosenID === '') return
    const id = chosenID
    const offered = offeredInForce
    const text = motive
    // Two acts drawn side by side, two labels of work. They shared one, so « Enregistrer la
    // dérogation » was the button that read « En cours… » while the waiver was being
    // DROPPED, and the button actually pressed said nothing at all.
    const written = await guarded(grams === null ? 'waiver-off' : 'waiver', () =>
      api.saveDecision(id, { offered, min_weight_g: grams, reason: text }),
    )
    if (written !== null) reason = ''
  }

  /**
   * Imports a CSV dropped on the screen (A4, ADR-011).
   *
   * The file is written where the ordinary watcher will find it: same parser, same
   * qualification, same acknowledgement as a file left by the producer. There is
   * therefore no second import path to maintain — and no second way for it to be wrong.
   *
   * @param file - what the drop or the file chooser gave.
   */
  async function importFile(file: File | null | undefined): Promise<void> {
    dropping = false
    if (file === null || file === undefined) return
    report = ''
    // What an act leaves on screen lives until ANOTHER act replaces it (§5.1), and starting
    // this one is that replacement — the rule `guarded` follows, and which this act did
    // not: a green « La décision est enregistrée. » sat above the drop, and above its
    // refusal.
    admin.actionError = ''
    admin.notice = ''
    working = 'import'
    try {
      // A protected act (ADR-033): it replaces the whole grid with a file somebody brought.
      const record = await admin.protect(() => api.importCatalog(file))
      if (record === null) return
      // The station has NOT applied this file, and says so itself: the route writes it where
      // the ordinary watch will find it and answers an inventory whose RESULT IS EMPTY —
      // « Why the record it returns carries no result », cmd/openscale/catalogadmin.go. The
      // branch that read `record.result` was therefore dead, and every file, including one
      // the watch discarded two seconds later, was announced as about to be applied. What is
      // certain is the inventory of the bytes; what happens next is the history, and the
      // sentence that says so is the station's own.
      report =
        `${file.name} : ${frenchInteger(record.rows_read_count)} lignes lues, ` +
        `${frenchInteger(record.weighable_count)} pesables. ` +
        (record.reason === ''
          ? 'Le fichier est déposé ; son résultat s’inscrira dans l’historique des imports.'
          : record.reason)
      // No read of the findings here: the import this drop is about does not exist yet. The
      // effect above reads them when the station changes import, which is the only instant
      // at which there is something new to read.
      await admin.refresh()
    } finally {
      working = ''
    }
  }

  /**
   * Takes a CSV let go on the zone, or says why it was not taken.
   *
   * This was the one act of the page outside the lock the page gave itself: a second file
   * let go while an import was in flight — or while the password panel was waiting —
   * started a second POST and a second replacement of the whole grid. Refusing in silence
   * would be no better, so the refusal NAMES the file it did not take.
   *
   * @param event - the drop.
   */
  function dropCSV(event: DragEvent): void {
    event.preventDefault()
    dropping = false
    const file = event.dataTransfer?.files.item(0)
    if (busy) {
      report =
        `${file?.name ?? 'Ce fichier'} n’a pas été déposé : un acte est déjà en cours sur ` +
        'cette page. Réessayez quand il aura répondu.'
      return
    }
    void importFile(file)
  }

  /**
   * Takes the CSV picked in the file chooser, and REARMS the field.
   *
   * A file input keeps the path it was given and fires no second `change` for the same one:
   * choosing the same file twice — exactly what « Oublier la quarantaine » then re-dropping
   * asks for — did strictly nothing. No message, no refusal, no answer at all.
   *
   * @param chooser - the field, emptied before the import travels.
   */
  function chooseCSV(chooser: HTMLInputElement): void {
    const file = chooser.files?.item(0)
    chooser.value = ''
    void importFile(file)
  }
</script>

<div class="pages">
  <CatalogSourcePanel {draft} station={health.station} />

  <Panel title="Dernier import">
    {#if health.catalog === null}
      <p class="fact">Aucun import enregistré sur ce poste.</p>
    {:else}
      <Inventory record={health.catalog} motives={health.catalog_motives} />
    {/if}
    <p class="fact muted" data-source>
      {health.catalog_source === null
        ? 'Aucune source de catalogue publiée par ce poste.'
        : 'Source : ' + health.catalog_source.label}
    </p>
    {#if reloadWatch.sentence !== ''}
      <p class="fact" data-reload>{reloadWatch.sentence}</p>
    {/if}
    <div class="actions">
      <Act
        act="reload"
        kind="write"
        label="Recharger le catalogue"
        protected
        busy={working === 'reload'}
        disabled={busy}
        onrun={() => void reloadCatalog()}
      />
      <Act
        act="quarantine"
        kind="destructive"
        label="Oublier la quarantaine"
        protected
        busy={working === 'quarantine'}
        disabled={busy}
        onrun={() => void guarded('quarantine', api.forgetQuarantine)}
      />
    </div>
    <p class="fact muted">
      « Oublier la quarantaine » fait relire un fichier que le poste avait écarté : c’est le
      seul geste de cette page qui puisse remettre en service un catalogue refusé.
    </p>
  </Panel>

  <!--
    Right under the inventory, and not further down: this panel exists to explain the gap
    between the pesables the import announces above and the number the client screen
    states at the bottom of its grid.
  -->
  <Panel
    title="Ce que la grille montre"
    note="Un réglage d’affichage : il ne change ni le fichier reçu, ni ce que le poste sait peser."
  >
    <Toggle
      path="ui.show_by_unit_products"
      label="Afficher les produits vendus à l’unité"
      hint={'Décoché, leurs tuiles quittent la grille et la recherche ne les retrouve plus. ' +
        'Ce que le poste perd : une tuile vendue à l’unité imprime une étiquette sans ' +
        'jamais lire la balance, et c’est le seul geste que ce réglage retire.'}
      on={draft.flag('ui.show_by_unit_products')}
      onchange={(on) => draft.set('ui.show_by_unit_products', on)}
    />
    <p class="fact" data-by-unit>{byUnitSaid}</p>
    <p class="fact muted">
      Un produit masqué reste vendable : la caisse lit toujours son code-barres, et une
      étiquette déjà imprimée reste valable. Ce réglage ne fait que retirer sa tuile.
    </p>

    <!--
      Onze choix visibles d'un coup, et non une glissière : ce sont des entiers qu'on
      nomme, et « Automatique » n'est pas un cran de plus au bout d'une course, c'est une
      autre nature — la grille d'aujourd'hui, sur n'importe quel écran.
    -->
    <p class="columns-label">
      Colonnes de la grille
      {#if preferences.showTechnicalNames}<code>{GRID_COLUMNS_PATH}</code>{/if}
    </p>
    <div class="columns" role="radiogroup" aria-label="Colonnes de la grille">
      <label
        class="column"
        data-columns={GRID_COLUMNS_AUTO}
        data-on={String(gridColumns === GRID_COLUMNS_AUTO)}
      >
        <input
          type="radio"
          name="grid-columns"
          value={GRID_COLUMNS_AUTO}
          checked={gridColumns === GRID_COLUMNS_AUTO}
          onchange={() => draft.set(GRID_COLUMNS_PATH, GRID_COLUMNS_AUTO)}
        />
        Automatique
      </label>
      {#each GRID_COLUMNS_CHOICES as count (count)}
        <label class="column" data-columns={count} data-on={String(gridColumns === count)}>
          <input
            type="radio"
            name="grid-columns"
            value={count}
            checked={gridColumns === count}
            onchange={() => draft.set(GRID_COLUMNS_PATH, count)}
          />
          {frenchInteger(count)}
        </label>
      {/each}
    </div>
    <div class="fact" data-grid-count>
      {#each gridLines as line, index (index)}
        <p>{line}</p>
      {/each}
      {#if otherScreen !== ''}
        <p class="muted" data-other-screen>{otherScreen}</p>
      {/if}
    </div>
    <p class="fact muted">
      Se tromper ne coûte rien d’autre que de revenir ici : le réglage ne change ni le
      fichier reçu, ni les étiquettes déjà imprimées.
    </p>

    <p class="columns-label">
      <label for="chip-threshold">Articles minimum pour afficher une catégorie</label>
      {#if preferences.showTechnicalNames}<code>{CHIP_THRESHOLD_PATH}</code>{/if}
    </p>
    <input
      id="chip-threshold"
      type="number"
      min="1"
      step="1"
      value={chipThreshold}
      oninput={(event) => draft.set(CHIP_THRESHOLD_PATH, Number(event.currentTarget.value))}
    />
    <p class="fact muted">
      En dessous de ce nombre, la catégorie n’a pas de puce dans la barre du bas. Ses
      produits restent dans « Tout » et à la recherche : ce réglage ne retire aucune
      tuile. Pour ne plus montrer une catégorie du tout, c’est sa case « visible » qu’il
      faut décocher.
    </p>

    <!--
      Les sondes, vides et invisibles : la grille du brouillon est DÉCLARÉE ici, et c'est
      le navigateur qui dit ce qu'elle donne. Personne d'autre ne sait combien de colonnes
      le mode automatique tire de cet écran, et les trois barres avec lesquelles la grille
      partage la hauteur sont lues dans leurs propres jetons — le jour où l'une d'elles
      change de hauteur, ce compte suit sans qu'on y pense.

      `data-tile-scale` n'est pas décoratif et ne vaut rien par lui-même : c'est le
      sélecteur sous lequel `app.css` RECALCULE les quatre jetons mis à l'échelle. Sans
      lui, cette boîte hériterait de valeurs déjà substituées contre le facteur de la
      racine, qui vaut 1, et le facteur posé ci-dessous ne déplacerait rien — des nombres
      plausibles, faux, et une phrase qui ne bouge pas quand on change de choix.
    -->
    <div class="probes" data-tile-scale inert aria-hidden="true" bind:this={probes}>
      <div class="probe grid-probe" bind:this={gridProbe}>
        <!--
          Une VRAIE tuile, et c'est tout le sujet : `--tile-height` laisse le bloc des
          prix hors de son calcul, et une sonde de ce jeton annonçait 189 px là où le
          navigateur en dessinait 245 — trois rangées promises sur une grille qui en
          montre deux. La cellule qui la porte est étirée par la grille : sa hauteur EST
          celle de la rangée.
        -->
        {#if sampleTile !== null}
          <div class="row-probe" bind:this={rowProbe}>
            <Tile
              product={sampleTile}
              nameSizePx={NAME_SIZE_MIN_PX}
              {primaryCode}
              showPrice={draftedFlag(
                'ui.show_grid_prices',
                presentation.show_grid_prices ?? true,
              )}
              onpick={() => {}}
            />
          </div>
        {/if}
      </div>
      <div class="probe calibration-probe" bind:this={calibrationProbe}></div>
      <div class="probe viewport-probe" bind:this={viewportProbe}></div>
    </div>
  </Panel>

  <Panel
    title="Déposer un catalogue"
    note="Glissez le fichier CSV ici, ou choisissez-le. Il passe par le même chemin que le fichier du producteur."
  >
    {#if report !== ''}
      <p class="fact" data-report>{report}</p>
    {/if}
    <div
      class="drop"
      class:dropping
      class:working={working === 'import'}
      role="group"
      aria-label="Zone de dépôt du catalogue"
      ondragover={(event) => {
        event.preventDefault()
        // No highlight while the page is locked: a zone that lights up is a zone that
        // takes, and this one will not.
        if (busy) return
        dropping = true
      }}
      ondragleave={() => (dropping = false)}
      ondrop={dropCSV}
    >
      <p>
        Déposez ici le fichier <code>flv_{health.station}.csv</code>
        <span class="key" title="Demande le mot de passe">clé</span>
      </p>
      <label class="choose touch-target">
        {working === 'import' ? 'Import en cours…' : 'Choisir un fichier'}
        <input
          type="file"
          accept=".csv,text/csv"
          disabled={busy}
          onchange={(event) => chooseCSV(event.currentTarget)}
        />
      </label>
    </div>
    <p class="fact muted">
      Ce dépôt remplace toute la grille par le fichier apporté : il change ce que le poste
      vend, et le mot de passe est donc demandé au moment du dépôt.
    </p>
  </Panel>

  <FindingsPanel
    title="Anomalies à corriger dans Odoo"
    note="Chaque ligne porte le nom du produit, son numéro dans le CSV, son motif et la valeur fautive."
    list="anomalies"
    state={historyState}
    findings={anomalies}
    singular="anomalie"
    plural="anomalies"
    none="Aucune anomalie sur le dernier import."
    remedy="Corrigez celles-ci dans Odoo : l’import suivant ne signalera que ce qui reste."
  />

  <FindingsPanel
    title="Unités divergentes"
    note="Le produit reste proposé : le code-barres fait foi, seul le libellé du prix est faux."
    list="mismatches"
    state={historyState}
    findings={mismatches}
    singular="unité divergente"
    plural="unités divergentes"
    none="Aucune unité divergente sur le dernier import."
  />

  <FindingsPanel
    title="Produits non pesables"
    note="Un inventaire, pas une liste d’erreurs : ces produits portent déjà leur code-barres et n’ont aucune raison d’être pesés."
    list="not-weighable"
    state={historyState}
    findings={neutral}
    singular="produit non pesable"
    plural="produits non pesables"
    none="Aucun produit non pesable sur le dernier import."
  />

  <Panel
    title="Produits retirés depuis l’import précédent"
    note="Un produit absent du nouveau fichier est marqué retiré à sa date, jamais supprimé."
  >
    {#if health.catalog === null}
      <p class="fact">Aucun import enregistré : rien n’a encore pu être retiré.</p>
    {:else if withdrawnCount === 0}
      <p class="fact">Aucun produit retiré par le dernier import.</p>
    {:else}
      <p class="fact" data-withdrawn>{withdrawnSaid}</p>
      <p class="fact muted">
        Ils restent enregistrés avec leur historique : une étiquette déjà collée reste
        lisible en caisse, et un produit qui revient dans un prochain fichier retrouve sa
        tuile. Ce poste n’en publie encore que le nombre — leurs noms se lisent dans Odoo,
        en comparant avec l’export précédent.
      </p>
    {/if}
  </Panel>

  <Panel
    title="Décider d’un produit"
    note="Retirer un produit et l’autoriser à peser moins sont deux décisions séparées : l’une n’efface pas l’autre."
  >
    <label class="search" for="product-search">Chercher un produit</label>
    <input id="product-search" type="search" bind:value={query} placeholder="ail, tomme, œufs…" />
    {#if query !== ''}
      <p class="fact muted" data-tally="matches">{matchesSaid}</p>
      <div class="scroll" data-rows="matches">
        <ul class="rows">
          {#each matches as product (product.id)}
            <li>
              <button type="button" class="pick" onclick={() => choose(product.id)}>
                {product.name}
              </button>
              <span class="id">{product.id}</span>
              <span class="value">{product.unit_price_text}{product.price_suffix}</span>
            </li>
          {/each}
        </ul>
      </div>
      <p class="fact muted">
        Un produit retiré de la grille ne se trouve plus ici : il se reprend dans
        « Décisions en vigueur », plus bas.
      </p>
    {/if}

    {#if chosenID !== ''}
      <div class="decision" data-decision={chosenID}>
        <p class="fact">
          Produit choisi : <strong>{nameOf(chosenID)}</strong> ({chosenID})
        </p>
        <p class="fact muted" data-in-force>
          En vigueur : {chosenDecision === null
            ? 'aucune décision — ce produit suit les règles générales'
            : decisionSentence(chosenDecision)}
        </p>

        <label for="decision-reason">
          Motif — exigé par le poste pour toute décision qu’il enregistre, et ce qui la rendra
          lisible dans six mois, par quelqu’un qui n’était pas là
        </label>
        <input id="decision-reason" type="text" bind:value={reason} />
        {#if motive === ''}
          <p class="fact muted" data-motive-needed>
            Sans motif, le poste refuse la décision. Le seul acte qui s’en passe est celui qui
            EFFACE la décision en vigueur : proposer de nouveau un produit qui ne porte aucune
            dérogation.
          </p>
        {/if}

        <!--
          Two acts, two buttons, two calls. They used to be one, and the one carried
          whatever the grams field happened to hold.
        -->
        <div class="act-block">
          <p class="what">Ce produit est-il proposé dans la grille ?</p>
          <div class="actions">
            {#if offeredInForce}
              <Act
                act="offered"
                kind="destructive"
                label="Ne plus proposer ce produit"
                protected
                busy={working === 'offered'}
                disabled={busy || !acts.canWithdraw}
                onrun={() => void setOffered(false)}
              />
            {:else}
              <Act
                act="offered"
                kind="write"
                label="Le proposer de nouveau"
                protected
                busy={working === 'offered'}
                disabled={busy || !acts.canOfferAgain}
                onrun={() => void setOffered(true)}
              />
            {/if}
          </div>
        </div>

        <div class="act-block">
          <label for="decision-waiver">
            Autoriser ce produit à peser moins que la limite générale, en grammes
          </label>
          <!--
            `value` and `oninput`, never `bind:value`: on a number input Svelte binds a
            NUMBER, so an empty field arrived as `null`, `Number(null)` is 0, and the
            station was told this product may weigh 0 g every time somebody withdrew a
            product without touching this field.
          -->
          <input
            id="decision-waiver"
            type="number"
            min="1"
            value={waiver}
            oninput={(event) => (waiver = event.currentTarget.value)}
          />
          <div class="actions">
            <Act
              act="waiver"
              kind="write"
              label="Enregistrer la dérogation"
              protected
              busy={working === 'waiver'}
              disabled={busy || !acts.canSaveWaiver}
              onrun={() => void setWaiver(typedWaiver)}
            />
            {#if waiverInForce !== null}
              <Act
                act="waiver-off"
                kind="write"
                label="Retirer la dérogation"
                protected
                busy={working === 'waiver-off'}
                disabled={busy || !acts.canDropWaiver}
                onrun={() => void setWaiver(null)}
              />
            {/if}
          </div>
          <p class="fact muted">
            La dérogation s’enregistre seule : elle ne remet pas dans la grille un produit
            qui en a été retiré, et retirer un produit n’efface pas sa dérogation.
          </p>
        </div>
      </div>
    {/if}
  </Panel>

  <DecisionsInForcePanel {decisions} {nameOf} onchoose={choose} />

  <ImportHistoryPanel {imports} state={historyState} />
</div>

<style>
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    margin: 0.75rem 0 0;
  }

  /*
   * The eleven column choices, laid side by side so the whole range is read at once.
   *
   * A slider was the other candidate and it is the wrong instrument twice over: these
   * are integers somebody names out loud, and « Automatique » is not one more notch at
   * the end of a travel — it is the grid deciding for itself. The administration is
   * driven with a mouse, so 44 px is the density here and not the 72 px of a finger.
   */
  .columns-label {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
    margin: 1rem 0 0.375rem;
    font-size: 1.0625rem;
    font-weight: 700;
  }

  .columns {
    display: flex;
    flex-wrap: wrap;
    gap: 0.375rem;
  }

  .column {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    min-height: 2.75rem;
    margin: 0;
    padding: 0 0.75rem;
    font-weight: 400;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--waiting-wash);
    cursor: pointer;
    transition:
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease);
  }

  /* The chosen one wears the same wash as the switch above it — two controls of one
     panel may not read as two different mechanisms — and it says so through the same
     attribute rather than through `:has()`, which a kiosk browser may be too old for.
     A choice nobody can tell from the other ten is the one failure this control has. */
  .column[data-on='true'] {
    background: var(--ready-wash);
    border-color: var(--ink-muted);
    font-weight: 700;
  }

  @media (hover: hover) {
    .column:hover {
      border-color: var(--ink-muted);
    }
  }

  /*
   * A radio is not a text field, and the `input` rule of this page would hand it the
   * width, the height, the border and the background of one.
   */
  .column input {
    width: 1.25rem;
    height: 1.25rem;
    min-height: 0;
    flex: 0 0 auto;
    padding: 0;
    background: none;
    border: none;
    border-radius: 0;
    accent-color: var(--focus);
  }

  [data-grid-count] p {
    margin: 0 0 0.25rem;
  }

  [data-grid-count] p:last-child {
    margin-bottom: 0;
  }

  /*
   * The probes: empty, invisible, and the only place the numbers above come from.
   *
   * Clipped by a box of zero height rather than pushed off-screen — one of them is as
   * wide as the window, and this page would otherwise scroll sideways. `visibility:
   * hidden` and never `display: none`, which would take the layout away with the probe
   * and leave nothing to read.
   */
  .probes {
    position: relative;
    height: 0;
    overflow: hidden;
  }

  .probe {
    position: absolute;
    top: 0;
    left: 0;
    visibility: hidden;
    pointer-events: none;
  }

  /* The grid the client draws, minus the padding its scroller spends on both sides,
     and with the row rule it draws its tiles by. Its own column declaration is written
     by the measurement: it follows the draft. */
  .grid-probe {
    display: grid;
    width: calc(100vw - var(--touch-gap) * 2);
    grid-auto-rows: minmax(var(--tile-height), auto);
    gap: var(--touch-gap);
    align-content: start;
  }

  /* What the automatic density is calibrated on, and the only way to know it in px:
     a custom property that is not registered gives back its substituted value —
     the string `clamp(…)` — and never a length. */
  .calibration-probe {
    width: var(--tile-min);
  }

  /* The height the grid really occupies at the client: the window, less the three
     permanent bars, less what its scroller pads with. */
  .viewport-probe {
    height: calc(
      100vh - var(--banner-height) - var(--category-height) - var(--status-height) -
        var(--touch-gap) * 2
    );
  }

  /* A key, not a red padlock: the act is possible, it only asks who you are. The word is
     written out — an icon alone teaches nothing to whoever does not know it (§14.4). The
     acts of this page carry their own; this one belongs to the drop zone, which is a
     LABEL and not a button. */
  .key {
    padding: 0.0625rem 0.375rem;
    border-radius: var(--radius-pill);
    background: var(--bg);
    color: var(--ink-muted);
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  /*
   * Every list of this page is BOUNDED and scrolls inside its own box.
   *
   * A real file carries 116 anomalies; unbounded, the page grew past 17 000 px and the
   * panels below it — the decisions, the history — stopped existing for whoever never
   * scrolled that far.
   */
  .scroll {
    max-height: 24rem;
    overflow: auto;
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
    background: var(--bg);
  }

  .rows {
    margin: 0;
    padding: 0 0.75rem;
    list-style: none;
  }

  .rows li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .rows li:first-child {
    border-top: none;
  }

  .id,
  .value {
    color: var(--ink-muted);
  }

  .what {
    font-weight: 700;
  }

  .pick {
    height: 2.75rem;
    padding: 0 0.5rem;
    font-weight: 700;
    color: var(--ink);
    text-decoration: underline;
    border-radius: var(--radius-sm);
    transition: background-color var(--tap) var(--ease);
  }

  @media (hover: hover) {
    .pick:hover:not(:disabled) {
      background: var(--surface);
    }
  }

  .search,
  label {
    display: block;
    margin-top: 0.5rem;
    font-size: 1.0625rem;
    font-weight: 700;
  }

  input {
    min-height: 2.75rem;
    width: 100%;
    max-width: 34rem;
    padding: 0 0.75rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .decision {
    margin-top: 0.75rem;
    padding-top: 0.75rem;
    border-top: 1px solid var(--border);
  }

  /* One act, one block: the withdrawal and the waiver are two decisions, and drawing them
     in one row of buttons is what made them look like one. */
  .act-block {
    margin-top: 1rem;
    padding: 0.75rem 1rem 1rem;
    background: var(--bg);
    border-radius: var(--radius);
  }

  .act-block .what {
    margin: 0;
    font-size: 1.0625rem;
    font-weight: 700;
  }

  /*
   * The drop zone. Its chooser is a LABEL and not a button, and it gets the same press
   * feedback all the same: a command that answers nothing under the finger reads as a
   * dead page, whatever element it is made of (§3.2).
   */
  .drop {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 1rem;
    padding: 1rem;
    border: 2px dashed var(--border);
    border-radius: var(--radius);
    transition:
      border-color var(--tap) var(--ease),
      background-color var(--tap) var(--ease);
  }

  .drop.dropping {
    border-color: var(--focus);
    background: var(--waiting-wash);
  }

  .drop.working {
    border-color: var(--waiting);
    background: var(--waiting-wash);
  }

  .drop p {
    margin: 0;
    font-size: 1.125rem;
  }

  /*
   * The chooser wears the IRREVERSIBLE red of `Act`, without being one: a drop replaces
   * the whole grid by the file brought in, and that is the same nature of act as
   * withdrawing a product. It cannot become an `<Act>` — turning the label into a button
   * would break the file picker it wraps — so it takes the token and nothing else.
   */
  .choose {
    display: inline-flex;
    align-items: center;
    margin: 0;
    padding: 0 1rem;
    font-size: 1.125rem;
    font-weight: 700;
    color: var(--surface);
    background: var(--danger);
    border: 1px solid var(--danger);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
    cursor: pointer;
    transition:
      transform var(--tap) var(--ease),
      border-color var(--tap) var(--ease);
  }

  .choose:active {
    transform: scale(0.975);
  }

  /*
   * The global escape hatch of `app.css` neutralises `button:active`, and this chooser is a
   * LABEL: on a station set to reduced motion, it was the one thing left on screen that
   * still jumped under the finger (§14.2).
   */
  @media (prefers-reduced-motion: reduce) {
    .choose:active {
      transform: none;
    }
  }

  /* Same reading as `Act`: a solid background DARKENS under the pointer. Lightening it
     drops the white ink below the 7:1 the token was chosen for. */
  @media (hover: hover) {
    .choose:hover {
      filter: brightness(0.92);
    }
  }

  .choose input {
    display: none;
  }
</style>
