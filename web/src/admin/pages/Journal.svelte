<script lang="ts">
  import Act from '../components/Act.svelte'
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { TechnicalLineDTO, WeighingDTO } from '../lib/dto'
  import { frenchDateTime, frenchDuration, frenchInteger } from '../lib/format'
  import type { Admin } from '../lib/session.svelte'

  /**
   * The Journal page of §14.4: the 200 last weighings, and the technical journal under it.
   *
   * The detail carries the RAW FRAME and the « Rejouer cette trame » button. That is what
   * turns an unexplained refusal into a permanent test, with no trip to the shop and no
   * scale: the frame goes back through the station's own decoder, so « it decodes » comes
   * to mean « it decodes in service » (§15.4).
   *
   * Five things this page owed §14.4 and ADR-033, and had not:
   *
   *  1. the table drew its 200 rows AT FULL HEIGHT — about 17 000 px — so the technical
   *     journal below it did not exist for anyone who never scrolled that far, and the
   *     column headings left the screen after eleven rows. It now scrolls inside its own
   *     box, under a header that stays;
   *  2. the detail opened UNDER THE WHOLE TABLE. Clicking row 3 sent the answer 16 000 px
   *     away, which reads as a button that does nothing. It now opens where it was asked
   *     for, in the row below the one that was clicked;
   *  3. the « imprimées » filter asked for `result=printed`, a value the service has never
   *     written: there is no `printed` and there never was — a print hands bytes to a
   *     transport, no transport can say the label physically came out, so a successful
   *     weighing is `sent` (important-7). The filter therefore selected NOTHING on a
   *     station that had printed all day, which reads as « the journal is broken »;
   *  4. `result`, `source` and `stability` reached a volunteer as the English tokens of
   *     `internal/domain/journal.go`. « unstable » in a column headed « Stabilité » is not
   *     a word this screen is allowed to use;
   *  5. « Rejouer cette trame » went through {@link Admin.run}, which shows a bare 401
   *     instead of asking for the password. It injects a measurement into the running
   *     station and it is protected by §14.5, so it goes through {@link Admin.protect}
   *     and is REPLAYED once the session is open.
   */
  interface Props {
    admin: Admin
  }

  const { admin }: Props = $props()

  /**
   * How many weighings the TABLE asks for — a figure of this page, not of the station.
   *
   * `GET /admin/api/journal` defaults to the same 200 and caps nothing: `intParam` returns
   * the fallback and never a ceiling (internal/web/admin.go), and the store serves any page
   * up to `maxPageSize` (internal/store/journal.go). So the screen may never say « le poste
   * n'en publie jamais plus de deux cents »: it publishes what it is asked for.
   */
  const WEIGHINGS_PAGE = 200

  /**
   * How many weighings the CSV EXPORT asks for, and why it is not the figure above.
   *
   * `maxPageSize` (internal/store/journal.go) is the largest page a journal read ever
   * serves, and it equals the shipped `journal.max_rows`: the whole journal IS the largest
   * legitimate page « because that is what /admin/api/journal/export.csv asks for ». Asking
   * for the page bound instead handed back the two hundred rows already on screen, which is
   * what made « les précédentes sont dans l'export CSV » a promise nothing kept.
   */
  const EXPORT_PAGE = 5000

  /** How many technical lines the page asks for. The route defaults to 200 and caps none. */
  const TECHNICAL_PAGE = 50

  /** How many technical lines `diagnostic.zip` carries (`archivedTechnical`, §15.4). */
  const DIAGNOSTIC_TECHNICAL = 500

  /** How many columns the table draws, so the detail row can span every one of them. */
  const COLUMN_COUNT = 7

  /**
   * How a weighing ended, in French — never the token the service wrote.
   *
   * The four values are the whole of `internal/domain/journal.go`. `sent` is the SUCCESS,
   * and it is spelled out rather than shortened to « imprimée » because the station does
   * not know whether the label came out: it knows it handed the bytes over (important-7).
   */
  const RESULTS: Record<string, string> = {
    sent: 'envoyée à l’imprimante',
    rejected: 'refusée',
    failed: 'en échec',
    reprint: 'réimpression',
  }

  /** The same four values as a filter, plus « toutes ». The plural reads as a heading. */
  const RESULT_FILTERS: { value: string; label: string }[] = [
    { value: '', label: 'toutes' },
    { value: 'sent', label: 'envoyées à l’imprimante' },
    { value: 'rejected', label: 'refusées' },
    { value: 'failed', label: 'en échec' },
    { value: 'reprint', label: 'réimpressions' },
  ]

  /** Where a weight came from, in French. The three values of §12.3. */
  const SOURCES: Record<string, string> = {
    scale: 'balance',
    manual: 'saisie manuelle',
    replay: 'trame rejouée',
  }

  /** What the frame said about stability, in French. The four values of §9.2. */
  const STABILITIES: Record<string, string> = {
    stable: 'stable',
    unstable: 'instable',
    unknown: 'non déclarée par la balance',
    not_applicable: 'sans objet — saisie manuelle',
  }

  /** How the product is sold, in French. The two modes of ADR-021. */
  const MODES: Record<string, string> = {
    by_weight: 'au poids',
    by_unit: 'à l’unité',
  }

  /** The severity of a technical line, in French. The five levels of `internal/store`. */
  const LEVELS: Record<string, string> = {
    debug: 'mise au point',
    info: 'information',
    warn: 'avertissement',
    error: 'erreur',
    critical: 'critique',
  }

  /** What part of the station wrote a technical line, in French. */
  const LOG_SOURCES: Record<string, string> = {
    scale: 'balance',
    printer: 'imprimante',
    catalog: 'catalogue',
    ui: 'écran',
    config: 'configuration',
    http: 'réseau',
    system: 'système',
  }

  let weighings = $state<WeighingDTO[]>([])
  let technical = $state<TechnicalLineDTO[]>([])
  let result = $state('')
  /** The id of the weighing whose detail is open, or null. */
  let opened = $state<number | null>(null)
  /** True while a protected replay is in flight: its button disarms, and it alone. */
  let replaying = $state(false)
  /** True while a page is being read: « Rafraîchir » disarms rather than queueing reads. */
  let reading = $state(false)
  /**
   * What the last read of each journal did.
   *
   * Three values and not a boolean, because an empty table has THREE meanings — not read
   * yet, read and empty, could not be read — and drawing « Aucune pesée ne correspond »
   * over the third is how a station that lost its database reads as a quiet day.
   */
  let readState = $state<'loading' | 'read' | 'unread'>('loading')
  let technicalState = $state<'loading' | 'read' | 'unread'>('loading')
  /**
   * Why the last read of each journal was refused, in the words of the service.
   *
   * COPIED here rather than pointed at: {@link Admin.load} clears `actionError` on its
   * SUCCESS path, so the technical read that follows the weighing read wipes the banner a
   * moment after it was drawn. A panel that said « la raison est écrite en haut de
   * l'écran » sent a volunteer to a sentence nobody draws any more — and when both reads
   * failed, the one banner left carried the reason of the OTHER journal, above the panel
   * of the weighings. Each refusal is now written UNDER the journal it belongs to.
   */
  let readFailure = $state('')
  let technicalFailure = $state('')

  /**
   * The filters of the TABLE, exactly as they leave in the query string.
   *
   * `limit` travels even though the service defaults to the same figure, so that the
   * sentence under the table states a bound this page owns rather than one that could
   * move elsewhere without the sentence knowing.
   */
  const filters = $derived<Record<string, string>>({
    limit: String(WEIGHINGS_PAGE),
    ...(result === '' ? {} : { result }),
  })

  /** The same filter, without the table's bound: the export is what reaches the rest. */
  const exportFilters = $derived<Record<string, string>>({
    ...filters,
    limit: String(EXPORT_PAGE),
  })

  const detail = $derived(weighings.find((row) => row.id === opened) ?? null)

  /** What the filter is called in French, for a screen that must not accuse one at random. */
  const resultLabel = $derived(RESULT_FILTERS.find((choice) => choice.value === result)?.label ?? '')

  const weighingTally = $derived(
    tally(
      weighings.length,
      WEIGHINGS_PAGE,
      'pesée',
      'pesées',
      `L’export CSV en emporte jusqu’à ${frenchInteger(EXPORT_PAGE)}.`,
    ),
  )
  const technicalTally = $derived(
    tally(
      technical.length,
      TECHNICAL_PAGE,
      'ligne',
      'lignes',
      `Le fichier de diagnostic emporte les ${frenchInteger(DIAGNOSTIC_TECHNICAL)} dernières.`,
    ),
  )

  void load()

  /**
   * Reads one page of the weighing journal and one page of the technical journal.
   *
   * Everything the screen knew is DROPPED before the wait, and not after it. Only the
   * first read used to be honest: picking « refusées » left the `sent` rows of the previous
   * read on screen, their tally under them and the select already reading « refusées », so
   * the page asserted a filter it was not showing. « Je ne sais pas encore » is the only
   * true thing a screen has to say while it waits.
   */
  async function load(): Promise<void> {
    // The open detail belongs to a row of the PREVIOUS page: a filter change can take
    // that row away, and a detail left open under a row that no longer exists would be
    // read as the detail of whatever ended up in its place.
    opened = null
    reading = true
    readState = 'loading'
    technicalState = 'loading'
    weighings = []
    technical = []
    readFailure = ''
    technicalFailure = ''
    try {
      const rows = await admin.load(() => api.fetchJournal(filters))
      readState = rows === null ? 'unread' : 'read'
      readFailure = rows === null ? admin.actionError : ''
      weighings = rows ?? []

      const lines = await admin.load(() => api.fetchTechnical({ limit: String(TECHNICAL_PAGE) }))
      technicalState = lines === null ? 'unread' : 'read'
      technicalFailure = lines === null ? admin.actionError : ''
      technical = lines ?? []
    } finally {
      reading = false
      // The shared banner of `App.svelte` is left EMPTY: both refusals are now drawn in
      // place, under the journal each one belongs to. Leaving one of the two up there
      // meant the technical reason stood over the weighing panel, as if it were the page's.
      admin.actionError = ''
    }
  }

  /**
   * Replays a frame — a PROTECTED act (§14.5, ADR-033).
   *
   * It pushes a measurement into the RUNNING station: the weight a customer is looking at
   * changes, and nothing takes that back. Hence the password at the moment of the act,
   * the key before the click, and the 72 px this page grants to nothing else.
   *
   * A row without a raw frame has NO BUTTON TO PRESS and a sentence in its place. The
   * guard that used to live here — a refusal written into `actionError` — could not be
   * reached, because the button was already disarmed on the same condition; what a
   * volunteer actually got was a greyed control and no explanation anywhere.
   *
   * @param frame - the raw frame, exactly as the journal recorded it.
   */
  async function replay(frame: string): Promise<void> {
    admin.notice = ''
    admin.actionError = ''
    replaying = true
    try {
      const done = await admin.protect(() => api.replayFrame(frame))
      if (done !== null) admin.notice = done.message
    } finally {
      replaying = false
    }
  }

  /**
   * « 200 pesées : c'est ce que cette page demande au poste, pas ce qu'il garde. »
   *
   * A bound that does not say what it hides is a lie by omission: a screen reading « 200
   * pesées » on a station that weighed six hundred that day makes whoever counts them
   * believe the day counted.
   *
   * The bound is attributed to the PAGE and never to the station, because that is where it
   * comes from: both routes read `limit` off the query string, fall back to a default and
   * cap nothing (internal/web/admin.go). A screen that blamed the station for its own
   * figure would send whoever wants the rest looking for a ceiling that does not exist.
   *
   * @param shown - how many rows are drawn.
   * @param asked - how many rows this page asked the station for.
   * @param singular - what one of them is called.
   * @param plural - what several of them are called.
   * @param beyond - where the ones this page did not ask for are to be found.
   */
  function tally(
    shown: number,
    asked: number,
    singular: string,
    plural: string,
    beyond: string,
  ): string {
    const noun = shown > 1 ? plural : singular
    if (shown < asked) return `${frenchInteger(shown)} ${noun}.`
    return (
      `${frenchInteger(shown)} ${noun} : c’est ce que cette page demande au poste, ` +
      `pas ce qu’il garde. ${beyond}`
    )
  }

  /**
   * Reads one token of the service in French, and never lets an unknown one through.
   *
   * A value this file has never heard of must not reach a volunteer as it came: it would
   * appear as an English word in a French column, and nobody would know it was new.
   *
   * @param table - the translations of that field.
   * @param token - what the service wrote.
   * @param unknown - the French sentence for a token nobody has declared.
   */
  function french(table: Record<string, string>, token: string, unknown: string): string {
    return table[token] ?? unknown
  }
</script>

<!--
  The page LEAVES the 68 rem reading column, and it is one of the three that may (§14.4):
  a journal is a table of seven columns, and a table narrowed to a paragraph either wraps
  its cells or pushes the whole body sideways. What is bounded here is the PROSE, which is
  what the reading column exists for; the tables take the width of the body and scroll
  inside their own box.
-->
<div class="journal">
  <Panel title="Deux cents dernières pesées">
    <div class="filters">
      <label for="journal-result">Résultat</label>
      <select
        id="journal-result"
        bind:value={result}
        onchange={() => {
          void load()
        }}
      >
        {#each RESULT_FILTERS as choice (choice.value)}
          <option value={choice.value}>{choice.label}</option>
        {/each}
      </select>
      <Act label="Rafraîchir" busy={reading} onrun={() => void load()} />
      <!--
        The link appears once the journal HAS ANSWERED, and 'loading' is not that. The
        `{:else}` caught 'loading' as much as 'read', so on a station without a journal the
        export stood there for the whole first read — and for every refresh after it, since
        the state kept its old value while the request was in flight. Touching it in that
        window laid the JSON of a refusal in a kiosk nobody knows how to come back from.
      -->
      {#if readState === 'loading'}
        <span class="fact muted">L’export sera proposé quand le journal aura répondu.</span>
      {:else if readState === 'unread'}
        <span class="fact muted">
          L’export n’est pas proposé : ce poste n’a pas répondu à la lecture du journal.
        </span>
      {:else}
        <a class="export" href={api.journalCSVURL(exportFilters)} download>Exporter en CSV</a>
      {/if}
    </div>
    {#if readState === 'read'}
      <p class="fact muted" data-export-note>
        L’export emporte le même filtre que le tableau, mais pas son plafond : il descend
        jusqu’à {frenchInteger(EXPORT_PAGE)} pesées, en point-virgule et en UTF-8 — il s’ouvre
        tel quel dans le tableur d’un Windows français. Il ne demande aucun mot de passe : la
        lecture du journal n’en demande pas non plus, et le fichier de diagnostic emporte déjà
        les deux cents dernières pesées.
      </p>
    {/if}

    {#if weighings.length === 0}
      <p class="fact" data-empty="weighings">
        {#if readState === 'loading'}
          Lecture du journal…
        {:else if readState === 'unread'}
          Le journal n’a pas pu être lu : {readFailure} Ce n’est pas « aucune pesée ».
        {:else if result === ''}
          Le journal ne contient aucune pesée.
        {:else}
          Aucune pesée ne correspond au filtre « {resultLabel} ».
        {/if}
      </p>
    {:else}
      <p class="fact muted" data-tally="weighings">{weighingTally}</p>
      <!--
        The table scrolls INSIDE this box, and the box is what keeps its heading.
        Unbounded, the 200 rows made a page of about 17 000 px whose column headings were
        gone after eleven of them.
      -->
      <div class="table-box" data-scroll="weighings">
        <table>
          <thead>
            <tr>
              <th>Quand</th>
              <th>Produit</th>
              <th>Net</th>
              <th>Code-barres</th>
              <th>Résultat</th>
              <th>Durée</th>
              <th><span class="sr-only">Détail</span></th>
            </tr>
          </thead>
          <tbody>
            {#each weighings as row (row.id)}
              <tr class:open={opened === row.id}>
                <td>{frenchDateTime(row.occurred_at)}</td>
                <td>{row.product_name}</td>
                <td>{frenchInteger(row.net_g)} g</td>
                <td>{row.barcode}</td>
                <td data-result>{french(RESULTS, row.result, 'résultat inconnu')}</td>
                <td>{frenchDuration(row.duration_ms)}</td>
                <td>
                  <button
                    type="button"
                    class="pick"
                    aria-expanded={opened === row.id}
                    onclick={() => (opened = opened === row.id ? null : row.id)}
                  >
                    {opened === row.id ? 'fermer' : 'détail'}
                  </button>
                </td>
              </tr>

              <!--
                The answer opens WHERE IT WAS ASKED FOR. It used to open under the whole
                table, so clicking row 3 put the frame 16 000 px away and the button read
                as one that does nothing.
              -->
              {#if opened === row.id && detail !== null}
                <tr class="detail-row" data-detail={row.id}>
                  <td class="detail-cell" colspan={COLUMN_COUNT}>
                    <!--
                      The id is written PLAIN. `frenchInteger` groups by thousands with a
                      non-breaking space, and it exists for QUANTITIES: an identifier read
                      out on the telephone or searched for in the export must match what
                      the CSV writes, which is 1236 and never « 1 236 ».
                    -->
                    <h3>Pesée {detail.id}</h3>
                    <dl>
                      <dt>Produit</dt>
                      <dd>{detail.product_name} ({detail.product_id})</dd>
                      <dt>Référence</dt>
                      <dd>{detail.reference}</dd>
                      <dt>Vente</dt>
                      <dd>
                        {french(MODES, detail.mode, 'mode de vente inconnu')} —
                        {frenchInteger(detail.quantity)}
                        {detail.quantity > 1 ? 'unités' : 'unité'}
                      </dd>
                      <dt>Brut / tare / net</dt>
                      <dd>
                        {frenchInteger(detail.gross_g)} g / {frenchInteger(detail.tare_g)} g /
                        {frenchInteger(detail.net_g)} g
                      </dd>
                      <dt>Stabilité</dt>
                      <dd data-stability>
                        {french(STABILITIES, detail.stability, 'stabilité inconnue')} — cadence
                        médiane {frenchDuration(detail.rate_ms)}
                      </dd>
                      <dt>Origine du poids</dt>
                      <dd data-source>{french(SOURCES, detail.source, 'origine inconnue')}</dd>
                      <dt>Résultat</dt>
                      <dd>
                        {french(RESULTS, detail.result, 'résultat inconnu')}{detail.detail === ''
                          ? ''
                          : ` — ${detail.detail}`}
                      </dd>
                      <dt>Trame brute</dt>
                      <dd>
                        <code>
                          {detail.frame === '' ? 'aucune trame enregistrée' : detail.frame}
                        </code>
                      </dd>
                    </dl>
                    <!--
                      A row without a frame gets a SENTENCE, and not a greyed button. A
                      control disabled on a condition it does not name teaches nothing: the
                      refusal that used to be written into `actionError` could never be
                      reached, because the same condition had already disarmed the button.
                    -->
                    {#if detail.frame === ''}
                      <p class="fact" data-no-frame>
                        Aucune trame brute n’a été enregistrée pour cette pesée : il n’y a
                        rien à rejouer.
                      </p>
                    {:else}
                      <div class="replay">
                        <Act
                          kind="destructive"
                          label="Rejouer cette trame"
                          protected
                          busy={replaying}
                          onrun={() => void replay(detail.frame)}
                        />
                      </div>
                      <p class="fact muted">
                        La trame repart dans le décodeur du poste EN SERVICE : le poids
                        affiché au client change, et rien ne le remet comme il était. C’est
                        ce qui fait d’un refus inexpliqué un test permanent, sans
                        déplacement au magasin et sans balance.
                      </p>
                    {/if}
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Panel>

  <Panel title="Journal technique">
    {#if technical.length === 0}
      <p class="fact" data-empty="technical">
        {#if technicalState === 'loading'}
          Lecture du journal technique…
        {:else if technicalState === 'unread'}
          Le journal technique n’a pas pu être lu : {technicalFailure} Ce n’est pas « aucune
          ligne ».
        {:else}
          Aucune ligne technique.
        {/if}
      </p>
    {:else}
      <p class="fact muted" data-tally="technical">{technicalTally}</p>
      <div class="lines-box" data-scroll="technical">
        <ul class="lines">
          {#each technical as line (line.id)}
            <li data-level={line.level}>
              <span class="when">{frenchDateTime(line.occurred_at)}</span>
              <span class="level">{french(LEVELS, line.level, 'niveau inconnu')}</span>
              <span class="from">{french(LOG_SOURCES, line.source, 'origine inconnue')}</span>
              {#if line.code !== ''}<span class="code">{line.code}</span>{/if}
              <span class="message">{line.message}</span>
              {#if line.detail !== ''}<span class="detail-text">{line.detail}</span>{/if}
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  </Panel>
</div>

<style>
  /*
   * `max-width: none` beats the 68 rem of `App.svelte` on specificity — an element and
   * two classes against one class and a child combinator — so this page leaves the
   * reading column without the shell knowing which pages do.
   */
  div.journal {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    max-width: none;
    /*
     * The reading column of §14.4, applied to the PROSE of this page and to nothing else.
     *
     * It is the SAME figure `App.svelte` bounds every other page with, written twice
     * because no token carries it and a page that has left `.page > *` cannot inherit it.
     * Two numbers that must move together and nothing holding them is how they drift, so
     * `admin-journal.test.ts` reads both files and fails when they stop being equal.
     */
    --reading-column: 68rem;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
    align-items: center;
    margin-bottom: 0.75rem;
    font-size: 1.0625rem;
  }

  .filters label {
    font-weight: 700;
  }

  /*
   * A form control of the administration is 44 px and not the 72 px of the customer
   * grid: this page is driven with a mouse (ADR-033). The one control that keeps its
   * 72 px is the replay, which cannot be taken back.
   */
  select {
    min-height: 2.75rem;
    padding: 0 0.5rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  /*
   * The export is an `<a>` and stays one — a download is what the browser does with an
   * href, not with a click handler. It therefore cannot be an `<Act>`, and copies the
   * neutral family by hand: reading the journal changes nothing on the station.
   *
   * It also gets the press feedback of a command: `app.css` only gives that to `button`,
   * and a command that answers nothing under the finger reads as a dead page whatever
   * element it is made of (§3.2).
   */
  .export {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    height: 2.75rem;
    padding: 0 1rem;
    font-size: 1.0625rem;
    font-weight: 700;
    text-decoration: none;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
    transition:
      transform var(--tap) var(--ease),
      border-color var(--tap) var(--ease),
      box-shadow var(--slide) var(--ease);
  }

  .export:active {
    transform: scale(0.975);
  }

  @media (hover: hover) {
    .export:hover {
      border-color: var(--ink-muted);
      box-shadow: var(--shadow-2);
    }
  }

  /* The replay sits under its own prose and needs the air the old button carried itself. */
  .replay {
    margin-top: 0.5rem;
  }

  /* Prose stays in the reading column even though the tables have left it. */
  .fact {
    margin: 0.5rem 0;
    max-width: var(--reading-column);
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  /*
   * The two boxes that hold a list, and the reason both exist.
   *
   * Nothing on this page may grow without a bound: 200 weighings drew about 17 000 px,
   * which pushed the technical journal out of anyone's reach and took the column headings
   * with it. `overflow: auto` is what lets seven nowrap columns be wider than the body
   * without the body itself ever scrolling sideways.
   */
  .table-box,
  .lines-box {
    overflow: auto;
    background: var(--bg);
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
  }

  .table-box {
    max-height: 34rem;
  }

  .lines-box {
    max-height: 28rem;
  }

  table {
    border-collapse: collapse;
    /* A minimum and not a width: the table fills the box, and grows past it when seven
       nowrap columns need more — at which point the box scrolls, and only the box. */
    min-width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.5rem;
    text-align: left;
    white-space: nowrap;
    border-bottom: 1px solid var(--border);
  }

  /* The heading STAYS. Eleven rows is all a 1080 px screen shows of two hundred. */
  th {
    position: sticky;
    top: 0;
    z-index: 1;
    color: var(--ink-muted);
    font-size: 1rem;
    background: var(--bg);
  }

  tbody tr {
    transition: background-color var(--tap) var(--ease);
  }

  @media (hover: hover) {
    tbody tr:hover {
      background: var(--surface);
    }
  }

  /*
   * The row whose detail is open is marked, so that the block below it is read as its
   * answer and not as the answer of whichever row happens to be next.
   *
   * It is marked by WEIGHT and by the rule its own detail already carries, never by a
   * state wash: `--waiting-wash` is the 10 % of `--waiting`, « weight not latched yet »,
   * a state of the CUSTOMER screen. §14.2 gives colour a meaning and never a decoration,
   * and a selection is not a state of the weighing.
   */
  tbody tr.open {
    background: var(--surface);
    font-weight: 700;
  }

  /* One rule runs from the row into the detail below it: same 3 px, same hue. */
  tbody tr.open > td:first-child {
    box-shadow: inset 0.1875rem 0 0 0 var(--focus);
  }

  .pick {
    height: 2.75rem;
    padding: 0 0.5rem;
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

  .detail-row {
    background: var(--surface);
  }

  /*
   * The detail is a paragraph inside a table of nowrap cells, so it says so: a raw frame
   * carries control characters and no spaces to break at, and left nowrap it would widen
   * the box by its own length.
   */
  .detail-cell {
    padding: 0.75rem 1rem 1rem;
    white-space: normal;
    box-shadow: inset 0.1875rem 0 0 0 var(--focus);
  }

  .detail-cell h3 {
    margin: 0 0 0.5rem;
    font-size: 1.25rem;
  }

  .detail-cell dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
    max-width: var(--reading-column);
    margin: 0;
    font-size: 1.0625rem;
    font-weight: 400;
  }

  .detail-cell dt {
    color: var(--ink-muted);
  }

  .detail-cell dd {
    margin: 0;
  }

  .detail-cell code {
    overflow-wrap: anywhere;
  }

  .lines {
    margin: 0;
    padding: 0 0.75rem;
    list-style: none;
  }

  .lines li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .lines li:first-child {
    border-top: none;
  }

  /* A line in fault carries a rule and never red ink: --fault reaches 6,54:1 on
     --surface, under the 7:1 that §14.2 demands of anything read. */
  .lines li[data-level='error'],
  .lines li[data-level='critical'] {
    border-left: 0.25rem solid var(--fault);
    padding-left: 0.5rem;
  }

  .lines li[data-level='warn'] {
    border-left: 0.25rem solid var(--warning);
    padding-left: 0.5rem;
  }

  .when,
  .level,
  .from,
  .code,
  .detail-text {
    color: var(--ink-muted);
  }

  .level {
    flex: none;
    width: 8rem;
  }

  .message {
    flex: 1 1 20rem;
    font-weight: 700;
  }

  /* The heading of the last column is read aloud and drawn nowhere: a column of « détail »
     buttons needs no title above it, and a screen reader needs one all the same. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
