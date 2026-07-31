<script lang="ts">
  import { fetchCatalog } from '../../lib/api'
  import { ALL_CATEGORIES, filterProducts, type Product } from '../../lib/catalog'
  import Act from '../components/Act.svelte'
  import Field from '../components/Field.svelte'
  import Inventory from '../components/Inventory.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { Draft } from '../lib/draft.svelte'
  import type { DecisionDTO, FindingDTO, HealthDTO, ImportDTO, ReloadDTO } from '../lib/dto'
  import { labelOf } from '../lib/fields'
  import { frenchDate, frenchDateTime, frenchInteger } from '../lib/format'
  import { importResultWord, importSourceWord } from '../lib/inventory'
  import { preferences } from '../lib/preferences.svelte'
  import { ReloadWatch } from '../lib/reload.svelte'
  import type { Admin } from '../lib/session.svelte'

  /**
   * The Catalogue page of §14.4.
   *
   * The list of not-weighable products is a NEUTRAL INVENTORY and never a list of
   * errors: a prepackaged bulgur is not at fault, it is simply no business of a scale.
   * Anomalies, on the other hand, each carry the NAME of the product, their CSV line
   * number, their reason in French and the offending value — that is the difference
   * between « 16 anomalies » and a work plan somebody can follow in Odoo (§10.3 bis).
   * The name comes from the import itself and never from the catalog in service: it is
   * the one the file carried, so the findings of an import from March keep saying what
   * that import said, and a batch the station refused — whose products never entered the
   * base at all — is named too.
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

  /** How many rows one list of findings draws. The file itself carries the rest. */
  const FINDINGS_SHOWN = 50

  /** How many human decisions in force are drawn at once. */
  const DECISIONS_SHOWN = 20

  /** The reasons that describe a product WITH NO TILE, and that are not faults. */
  const NOT_WEIGHABLE_CODES = [
    'NO_BARCODE',
    'PREPACKAGED_PRODUCT',
    'INTERNAL_CODE_NOT_WEIGHABLE',
  ]

  /**
   * The settings each source owns OUTRIGHT.
   *
   * Switching source wipes them, because the station refuses their mere PRESENCE under
   * the other one: no account and no password to read a directory one owns (control 39),
   * no directory of this machine behind an address (control 47). Without that clean-up,
   * the one gesture this panel exists to offer came back as three refusals over fields
   * nobody had filled in. What holds for both sources — the watch cadence, the separator,
   * the ceilings — is not listed here and therefore never moves.
   */
  const OWN_OPTIONS: Record<string, string[]> = {
    local_drop: ['catalog.options.directory'],
    webdav: ['catalog.options.url', 'catalog.options.username', 'catalog.options.password'],
  }

  /**
   * The source the document declares, empty for as long as it has not been read.
   *
   * No fallback on « dépôt local »: a document with no source is refused by the station
   * (control 5), and ticking a radio in its place would have the screen read out a choice
   * the file does not carry — then answer « aucune source de catalogue n'est déclarée » to
   * a save that nothing had announced.
   */
  const source = $derived(draft.text('catalog.type'))

  let imports = $state<ImportDTO[]>([])
  let findings = $state<FindingDTO[]>([])
  let products = $state<Product[]>([])
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
  /**
   * Whether the import history and its findings have been READ.
   *
   * Three values and not two, for the reason {@link Admin.load} gives about itself: an
   * empty array with no explanation reads as « there is none », which is false. A station
   * with no journal (ADR-013) answers 503 « ce poste n'a pas d'historique d'imports » to
   * every read, and four panels of this page used to answer that with « Aucune anomalie
   * sur le dernier import. » — permanently, and reassuringly.
   */
  let historyState = $state<'loading' | 'read' | 'unread'>('loading')
  /** Whether the client catalog has been read, so a missing name can be explained. */
  let namesState = $state<'loading' | 'read' | 'unread'>('loading')

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

  /**
   * What this station does with them, in one French sentence.
   *
   * It follows the SWITCH and not the saved file, so that the trade-off is legible
   * before the save rather than after it. And it states nothing this page has not read:
   * a station whose client catalog could not be opened does not know this number.
   */
  const byUnitSentence = $derived.by(() => {
    if (namesState === 'loading') return 'Lecture du catalogue en service…'
    if (namesState === 'unread') {
      return 'Le catalogue en service n’a pas pu être lu : cet écran ne sait pas combien de ' +
        'produits se vendent à l’unité.'
    }
    if (byUnitCount === 0) {
      return 'Aucun produit vendu à l’unité dans le catalogue en service.'
    }
    const many = byUnitCount > 1
    const subject = many ? 'produits vendus à l’unité sont' : 'produit vendu à l’unité est'
    const said = draft.flag('ui.show_by_unit_products')
      ? `${many ? 'montrés' : 'montré'} dans la grille de ce poste`
      : `${many ? 'masqués' : 'masqué'} sur ce poste`
    return `${frenchInteger(byUnitCount)} ${subject} ${said}.`
  })

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

  const shownAnomalies = $derived(anomalies.slice(0, FINDINGS_SHOWN))
  const shownMismatches = $derived(mismatches.slice(0, FINDINGS_SHOWN))
  const shownNeutral = $derived(neutral.slice(0, FINDINGS_SHOWN))

  /** The human decisions in force, most recent first, capped when drawn. */
  const decisions = $derived(
    [...health.decisions].sort((a, b) => b.decided_at.localeCompare(a.decided_at)),
  )
  const shownDecisions = $derived(decisions.slice(0, DECISIONS_SHOWN))

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

  /**
   * The waiver typed in the form, and whether it can travel.
   *
   * `Number('')` is 0 and `Number('abc')` is NaN, which `JSON.stringify` turns into
   * `null`: an unusable field would silently write « this product may weigh 0 g » or
   * silently drop a waiver somebody meant to grant.
   */
  const typedWaiver = $derived(waiver.trim() === '' ? null : Number(waiver))
  const waiverIsUsable = $derived(
    typedWaiver !== null && Number.isFinite(typedWaiver) && typedWaiver > 0,
  )

  /** The motive as it will TRAVEL: trimmed, because a space is not an explanation. */
  const motive = $derived(reason.trim())

  /**
   * Whether the four acts of a decision can travel as things stand.
   *
   * The motive is a CONDITION and not a comment, and the field used to present it as one:
   * the station answers 422 « Indiquez le motif de cette décision. » to every decision it
   * writes. Arming a button it is certain to refuse — after asking for the password, which
   * is worse — is a promise this page cannot keep.
   */
  const canWithdraw = $derived(motive !== '' || !needsMotive(false, waiverInForce))
  const canOfferAgain = $derived(motive !== '' || !needsMotive(true, waiverInForce))
  const canSaveWaiver = $derived(
    waiverIsUsable && (motive !== '' || !needsMotive(offeredInForce, typedWaiver)),
  )
  const canDropWaiver = $derived(motive !== '' || !needsMotive(offeredInForce, null))

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
  const withdrawnSentence = $derived(
    `${frenchInteger(withdrawnCount)} ` +
      `${withdrawnCount > 1 ? 'produits retirés' : 'produit retiré'}` +
      (previousImport === null
        ? '.'
        : ` depuis l’import du ${frenchDateTime(previousImport.occurred_at)}.`),
  )

  /** « 20 produits affichés sur 47 — précisez votre recherche. » */
  const matchTally = $derived(
    found.length > matches.length
      ? `${frenchInteger(matches.length)} produits affichés sur ` +
        `${frenchInteger(found.length)} trouvés — précisez votre recherche.`
      : `${frenchInteger(found.length)} ${found.length > 1 ? 'produits trouvés' : 'produit trouvé'}.`,
  )

  /** « 7 imports affichés. » The route never serves more than twenty (§14.5). */
  const importTally = $derived(
    `${frenchInteger(imports.length)} ` +
      `${imports.length > 1 ? 'imports affichés' : 'import affiché'} : ` +
      'le poste n’en publie jamais plus de vingt.',
  )

  /** What the three lists of findings say as long as they have not been read. */
  const findingsUnknown = $derived(
    historyState === 'loading'
      ? 'Lecture des signalements du dernier import…'
      : 'Les signalements du dernier import n’ont pas pu être lus : cet écran ne sait pas ce ' +
        'qu’ils disent.',
  )

  /** What the history table says as long as it has not been read. */
  const historyUnknown = $derived(
    historyState === 'loading'
      ? 'Lecture de l’historique des imports…'
      : 'L’historique des imports n’a pas pu être lu : cet écran ne sait pas ce qu’il contient.',
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
    products = catalog.products
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
   * Chooses the catalog source, and wipes the settings of the other one.
   *
   * @param chosen - the source, as the service names it.
   */
  function chooseSource(chosen: string): void {
    draft.set('catalog.type', chosen)
    for (const [type, paths] of Object.entries(OWN_OPTIONS)) {
      if (type === chosen) continue
      for (const path of paths) draft.unset(path)
    }
  }

  /** The message of the control that refused this key, empty when there is none. */
  function faultOf(path: string): string {
    return draft.faults.find((fault) => fault.field === path)?.message ?? ''
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
   * Whether an act writing these two columns needs a motive to be ACCEPTED.
   *
   * The station requires one for every decision it WRITES. The single act exempt is the one
   * that writes nothing: offered again AND no waiver ERASES the row (`ClearDecision`,
   * internal/web/admin.go), and a row that no longer exists needs no explaining.
   *
   * @param offered - whether the product would go back into the grid.
   * @param grams - the waiver that would travel with it.
   */
  function needsMotive(offered: boolean, grams: number | null): boolean {
    return !(offered && grams === null)
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

  /**
   * The name of a product, or an honest sentence when this page cannot give one.
   *
   * THREE cases and not two. `unread` used to answer like `read`, so a failed read of the
   * client catalog made this page state, of every decision in force, that its product had
   * left the catalog — having never managed to open the catalog.
   *
   * @param id - the Odoo id of the product.
   */
  function nameOf(id: string): string {
    const name = names.get(id)
    if (name !== undefined) return name
    if (namesState === 'loading') return 'Lecture du nom…'
    if (namesState === 'unread') return 'Nom inconnu : le catalogue n’a pas pu être lu'
    return 'Produit absent du catalogue en service'
  }

  /**
   * « 50 lignes affichées sur 116 anomalies. », or « 16 anomalies. » when nothing is hidden.
   *
   * A cap that does not say what it hides is a lie by omission, and this one used to be
   * silent on both counts: the search truncated at twenty without a word.
   *
   * @param shown - how many rows are drawn.
   * @param total - how many there are.
   * @param singular - what one of them is called.
   * @param plural - what several of them are called.
   */
  function tally(shown: number, total: number, singular: string, plural: string): string {
    const noun = total > 1 ? plural : singular
    if (shown >= total) return `${frenchInteger(total)} ${noun}.`
    return `${frenchInteger(shown)} lignes affichées sur ${frenchInteger(total)} ${noun}.`
  }

  /** What one decision in force says, in one French line. */
  function decisionSentence(decision: DecisionDTO): string {
    const parts: string[] = []
    if (!decision.offered) parts.push('retiré de la grille')
    if (decision.min_weight_g !== null) {
      parts.push(`peut peser à partir de ${frenchInteger(decision.min_weight_g)} g`)
    }
    return parts.length === 0 ? 'aucune restriction' : parts.join(' · ')
  }

  /** How an import ended, in French — never the token the service wrote. */
  function frenchResult(record: ImportDTO): string {
    const said = importResultWord(record.result)
    return record.code === '' ? said : `${said} (${record.code})`
  }

  /** Where an import came from, in French — never the token the service wrote. */
  function frenchSource(record: ImportDTO): string {
    return importSourceWord(record.source)
  }
</script>

<!--
  One list per subject, each capped, each announcing its total, and the two acts of a
  decision separated. Nothing here is a list that can grow without saying so.
-->
{#snippet rows(list: FindingDTO[], label: string)}
  <!--
    Deliberately UNKEYED: `csv_line` is not a key. One CSV row raises one finding per
    problem it has — a zero price AND a wrong check digit is two findings on line 42 —
    and a keyed each would have thrown `each_key_duplicate` on the first such file,
    taking the whole administration screen down with it.
  -->
  <div class="scroll" data-rows={label}>
    <ul class="rows">
      {#each list as finding}
        <li>
          <span class="line">ligne {frenchInteger(finding.csv_line)}</span>
          <!--
            The NAME first, and the Odoo id after it: « 4412 » is a number somebody has to
            look up before they can start, « TOMATE GRAPPE » is the product they already
            know. The id stays because it is what opens the record in Odoo.

            Drawn only when the import recorded one. Two findings never carry a name — one
            that bears on no product, and a row too damaged to hold one, which is exactly
            what UNREADABLE_ROW says in its own message — and inventing a wording for that
            absence would state a fact this page has not read.
          -->
          {#if finding.product_name !== ''}
            <span class="what" data-name>{finding.product_name}</span>
          {/if}
          <span class="id">{finding.product_id}</span>
          <span class="value">{finding.value}</span>
          <span class="message">{finding.message}</span>
        </li>
      {/each}
    </ul>
  </div>
{/snippet}

<!--
  A switch, drawn as the Rules page draws its own. `Field` has no boolean kind, and the
  one flag of this page is not a reason to grow one. It takes a RECORD and not three
  arguments so that the key it writes reads as `path:` — the form the field index checks
  for, and the only way this switch cannot be added without its French name.
-->
{#snippet toggle(field: { path: string; label: string; hint: string })}
  <label class="toggle" data-flag={field.path} data-on={String(draft.flag(field.path))}>
    <input
      type="checkbox"
      checked={draft.flag(field.path)}
      onchange={(event) => draft.set(field.path, event.currentTarget.checked)}
    />
    <span class="toggle-text">
      <span class="toggle-label">{field.label}</span>
      <!-- Behind the switch, as `Field` puts it: the key is written for whoever edits
           the file, and read out loud by nobody else. -->
      {#if preferences.showTechnicalNames}<code>{field.path}</code>{/if}
      <span class="hint">{field.hint}</span>
    </span>
  </label>
{/snippet}

<div class="pages">
  <!--
    At the top of the page: where the catalog comes from is read before what it delivered.
    The directory field only appears under the source that watches one, and the address
    only under the source that queries one — an empty field under a source that ignores it
    is an invitation to fill it in, and the station would refuse the save.
  -->
  <Panel title="Où le poste va chercher le catalogue">
    <div class="choice" role="radiogroup" aria-label="Source du catalogue">
      <label>
        <input
          type="radio"
          name="catalog-source"
          value="local_drop"
          checked={source === 'local_drop'}
          onchange={() => chooseSource('local_drop')}
        />
        Un répertoire de ce poste ou du réseau
      </label>
      <label>
        <input
          type="radio"
          name="catalog-source"
          value="webdav"
          checked={source === 'webdav'}
          onchange={() => chooseSource('webdav')}
        />
        Un serveur WebDAV
      </label>
    </div>

    {#if draft.config === null}
      <p class="fact muted">Lecture des réglages du poste…</p>
    {:else if source === 'local_drop'}
      <Field
        label={labelOf('catalog.options.directory')}
        path="catalog.options.directory"
        value={draft.text('catalog.options.directory')}
        hint="Laissez vide pour le répertoire du poste, celui que le service crée lui-même. Un répertoire nommé ici doit exister : le poste ne le crée pas."
        fault={faultOf('catalog.options.directory')}
        onchange={(value) => draft.set('catalog.options.directory', value)}
      />
      <p class="fact muted">
        Le poste y cherche le fichier <code>flv_{health.station}.csv</code>, et le supprime
        une fois lu : c’est ce qui dit au producteur que la livraison est prise.
      </p>
    {:else if source === 'webdav'}
      <Field
        label={labelOf('catalog.options.url')}
        path="catalog.options.url"
        value={draft.text('catalog.options.url')}
        fault={faultOf('catalog.options.url')}
        onchange={(value) => draft.set('catalog.options.url', value)}
      />
      <Field
        label={labelOf('catalog.options.username')}
        path="catalog.options.username"
        value={draft.text('catalog.options.username')}
        fault={faultOf('catalog.options.username')}
        onchange={(value) => draft.set('catalog.options.username', value)}
      />
      <!--
        The field opens EMPTY and not on the value in service: the station no longer serves
        the password to the browser. Left empty, it changes nothing.
      -->
      <Field
        label={labelOf('catalog.options.password')}
        path="catalog.options.password"
        kind="password"
        value=""
        hint="Laissez vide : le mot de passe actuel est conservé."
        fault={faultOf('catalog.options.password')}
        onchange={(value) => draft.set('catalog.options.password', value)}
      />
      <p class="fact muted" data-webdav-warning>
        Sur un serveur WebDAV, le dépôt d’un fichier CSV depuis cet écran n’est plus
        possible : le poste n’a plus de répertoire local où l’écrire. C’est le seul recours
        du jour de la mise en service.
      </p>
    {:else}
      <p class="fact">
        Ce poste ne déclare aucune source : choisissez-en une ci-dessus, sinon il n’ira
        chercher aucun catalogue.
      </p>
    {/if}
  </Panel>

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
    {@render toggle({
      path: 'ui.show_by_unit_products',
      label: 'Afficher les produits vendus à l’unité',
      hint:
        'Décoché, leurs tuiles quittent la grille et la recherche ne les retrouve plus. ' +
        'Ce que le poste perd : une tuile vendue à l’unité imprime une étiquette sans ' +
        'jamais lire la balance, et c’est le seul geste que ce réglage retire.',
    })}
    <p class="fact" data-by-unit>{byUnitSentence}</p>
    <p class="fact muted">
      Un produit masqué reste vendable : la caisse lit toujours son code-barres, et une
      étiquette déjà imprimée reste valable. Ce réglage ne fait que retirer sa tuile.
    </p>
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

  <Panel
    title="Anomalies à corriger dans Odoo"
    note="Chaque ligne porte le nom du produit, son numéro dans le CSV, son motif et la valeur fautive."
  >
    {#if historyState !== 'read'}
      <p class="fact" data-unread="anomalies">{findingsUnknown}</p>
    {:else if anomalies.length === 0}
      <p class="fact">Aucune anomalie sur le dernier import.</p>
    {:else}
      <p class="fact muted" data-tally="anomalies">
        {tally(shownAnomalies.length, anomalies.length, 'anomalie', 'anomalies')}
        {#if anomalies.length > shownAnomalies.length}
          Corrigez celles-ci dans Odoo : l’import suivant ne signalera que ce qui reste.
        {/if}
      </p>
      {@render rows(shownAnomalies, 'anomalies')}
    {/if}
  </Panel>

  <Panel
    title="Unités divergentes"
    note="Le produit reste proposé : le code-barres fait foi, seul le libellé du prix est faux."
  >
    {#if historyState !== 'read'}
      <p class="fact" data-unread="mismatches">{findingsUnknown}</p>
    {:else if mismatches.length === 0}
      <p class="fact">Aucune unité divergente sur le dernier import.</p>
    {:else}
      <p class="fact muted" data-tally="mismatches">
        {tally(shownMismatches.length, mismatches.length, 'unité divergente', 'unités divergentes')}
      </p>
      {@render rows(shownMismatches, 'mismatches')}
    {/if}
  </Panel>

  <Panel
    title="Produits non pesables"
    note="Un inventaire, pas une liste d’erreurs : ces produits portent déjà leur code-barres et n’ont aucune raison d’être pesés."
  >
    {#if historyState !== 'read'}
      <p class="fact" data-unread="not-weighable">{findingsUnknown}</p>
    {:else if neutral.length === 0}
      <p class="fact">Aucun produit non pesable sur le dernier import.</p>
    {:else}
      <p class="fact muted" data-tally="not-weighable">
        {tally(shownNeutral.length, neutral.length, 'produit non pesable', 'produits non pesables')}
      </p>
      {@render rows(shownNeutral, 'not-weighable')}
    {/if}
  </Panel>

  <Panel
    title="Produits retirés depuis l’import précédent"
    note="Un produit absent du nouveau fichier est marqué retiré à sa date, jamais supprimé."
  >
    {#if health.catalog === null}
      <p class="fact">Aucun import enregistré : rien n’a encore pu être retiré.</p>
    {:else if withdrawnCount === 0}
      <p class="fact">Aucun produit retiré par le dernier import.</p>
    {:else}
      <p class="fact" data-withdrawn>{withdrawnSentence}</p>
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
      <p class="fact muted" data-tally="matches">{matchTally}</p>
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
                disabled={busy || !canWithdraw}
                onrun={() => void setOffered(false)}
              />
            {:else}
              <Act
                act="offered"
                kind="write"
                label="Le proposer de nouveau"
                protected
                busy={working === 'offered'}
                disabled={busy || !canOfferAgain}
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
              disabled={busy || !canSaveWaiver}
              onrun={() => void setWaiver(typedWaiver)}
            />
            {#if waiverInForce !== null}
              <Act
                act="waiver-off"
                kind="write"
                label="Retirer la dérogation"
                protected
                busy={working === 'waiver-off'}
                disabled={busy || !canDropWaiver}
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

  <Panel
    title="Décisions en vigueur"
    note="Ce qu’un humain a décidé, produit par produit. C’est ici que se reprend un produit retiré de la grille."
  >
    {#if decisions.length === 0}
      <p class="fact">Aucune décision locale : la grille est celle du fichier.</p>
    {:else}
      <p class="fact muted" data-tally="decisions">
        {tally(shownDecisions.length, decisions.length, 'décision en vigueur', 'décisions en vigueur')}
      </p>
      <div class="scroll" data-rows="decisions">
        <ul class="rows">
          {#each shownDecisions as decision (decision.product_id)}
            <li>
              <button type="button" class="pick" onclick={() => choose(decision.product_id)}>
                Reprendre cette décision
              </button>
              <span class="what">{nameOf(decision.product_id)}</span>
              <span class="id">{decision.product_id}</span>
              <span class="message">{decisionSentence(decision)}</span>
              <span class="value">{decision.reason}</span>
              <span class="line">{frenchDate(decision.decided_at)}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  </Panel>

  <Panel title="Vingt derniers imports">
    {#if historyState !== 'read'}
      <p class="fact" data-unread="imports">{historyUnknown}</p>
    {:else if imports.length === 0}
      <p class="fact">Aucun import dans l’historique.</p>
    {:else}
      <p class="fact muted" data-tally="imports">{importTally}</p>
      <div class="scroll">
        <table>
          <thead>
            <tr>
              <th>Quand</th>
              <th>Fichier</th>
              <th>Source</th>
              <th>Résultat</th>
              <th>Motif</th>
              <th>Lues</th>
              <th>Pesables</th>
              <th>Non pesables</th>
              <th>Anomalies</th>
              <th>Retirés</th>
            </tr>
          </thead>
          <tbody>
            {#each imports as record (record.id)}
              <tr>
                <td>{frenchDateTime(record.occurred_at)}</td>
                <td>{record.file_name}</td>
                <td>{frenchSource(record)}</td>
                <td data-result>{frenchResult(record)}</td>
                <td class="why">{record.reason}</td>
                <td>{frenchInteger(record.rows_read_count)}</td>
                <td>{frenchInteger(record.weighable_count)}</td>
                <td>{frenchInteger(record.not_weighable_count)}</td>
                <td>{frenchInteger(record.anomalies_count)}</td>
                <td>{frenchInteger(record.products_withdrawn_count)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Panel>
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

  /* The switch, drawn exactly as the Rules page draws its own: two pages that offer the
     same kind of control may not look like two different mechanisms. */
  .toggle {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-height: 2.75rem;
    margin: 0.5rem 0 0;
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--waiting-wash);
    /* Taken back from the `label` rule of this page: only the switch's own name is bold,
       and the sentence behind it is prose. */
    font-weight: 400;
    cursor: pointer;
    transition:
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease);
  }

  .toggle[data-on='true'] {
    background: var(--ready-wash);
  }

  /* What a mouse expects, and a finger never asked for (app.css). */
  @media (hover: hover) {
    .toggle:hover {
      border-color: var(--ink-muted);
    }
  }

  .toggle input {
    flex: none;
    width: 1.5rem;
    height: 1.5rem;
    min-height: 0;
    min-width: 0;
    padding: 0;
    accent-color: var(--focus);
  }

  .toggle-text {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
  }

  .toggle-label {
    font-size: 1.0625rem;
    font-weight: 700;
  }

  .hint {
    flex: 1 1 20rem;
    color: var(--ink-muted);
    font-size: 1rem;
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

  .line,
  .id,
  .value {
    color: var(--ink-muted);
  }

  .line {
    flex: none;
    width: 7rem;
  }

  .what {
    font-weight: 700;
  }

  .message {
    flex: 1 1 20rem;
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

  /*
   * The two sources, one under the other: they rule each other out, and two lines compare
   * better than two boxes side by side on a screen held to a reading width.
   */
  .choice {
    display: flex;
    flex-direction: column;
  }

  .choice label {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    /* 44 px: the density of the administration's form controls. */
    min-height: 2.75rem;
    margin: 0;
    font-weight: 400;
  }

  /*
   * A radio button is not a text field, and the `input` rule of this page would hand it
   * the width, the height, the border and the background of one. Everything is taken back
   * here so that the browser draws it the way it draws a radio.
   */
  .choice input {
    width: 1.5rem;
    height: 1.5rem;
    min-height: 0;
    flex: 0 0 auto;
    padding: 0;
    background: none;
    border: none;
    border-radius: 0;
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

  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.5rem;
    text-align: left;
    white-space: nowrap;
    border-bottom: 1px solid var(--border);
  }

  /* The reason a file was refused is a sentence, and the one cell allowed to wrap. */
  .why {
    white-space: normal;
    min-width: 16rem;
    color: var(--ink-muted);
  }

  th {
    position: sticky;
    top: 0;
    color: var(--ink-muted);
    font-size: 1rem;
    background: var(--bg);
  }
</style>
