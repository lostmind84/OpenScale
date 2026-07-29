<script lang="ts">
  import Banner from './components/Banner.svelte'
  import CategoryBar from './components/CategoryBar.svelte'
  import FullScreen from './components/FullScreen.svelte'
  import Grid from './components/Grid.svelte'
  import SearchField from './components/SearchField.svelte'
  import StatusBar from './components/StatusBar.svelte'
  import TarePad from './components/TarePad.svelte'
  import * as api from './lib/api'
  import { ALL_CATEGORIES, chips, filterProducts, visibleProducts, type Product } from './lib/catalog'
  import { catalogStamp } from './lib/format'
  import { hardwareIsHealthy } from './lib/health'
  import { Session } from './lib/session.svelte'
  import { ulid } from './lib/ulid'

  /** The client screen: one grid, one banner, two bottom bars, nothing modal. */
  const session = new Session()

  let activeCategory = $state(ALL_CATEGORIES)
  let query = $state('')
  /** True once the search key has been used to reveal the field with nothing typed yet. */
  let typedOpen = $state(false)
  /** The field shows as soon as either something is typed or it was opened empty. */
  const fieldVisible = $derived(query !== '' || typedOpen)
  /** The tile disabled from the pointerdown until the stream answers (§14.3). */
  let busyID = $state<string | null>(null)
  let busySinceRevision = $state(-1)
  let tareEntry = $state<string | null>(null)

  const catalog = $derived(session.catalog)
  const products = $derived(catalog === null ? [] : visibleProducts(catalog))
  const bar = $derived(catalog === null ? [] : chips(catalog))
  const shown = $derived(filterProducts(products, activeCategory, query))
  const colors = $derived.by(() => {
    const map: Record<string, string> = {}
    for (const c of catalog?.categories ?? []) map[c.code] = c.color
    return map
  })

  // Named `snapshot` and not `state`: a variable called `state` makes every
  // `$state` rune ambiguous with the `$store` prefix, and svelte-check says so.
  const snapshot = $derived(session.state)
  const settings = $derived(session.presentation)
  const healthy = $derived(hardwareIsHealthy(snapshot))
  /**
   * What the grid says when it has nothing to draw — and the three cases differ.
   *
   * « Aucun produit ne correspond » is true of a filter that matched nothing and
   * FALSE of a station whose catalog has not arrived: there is nothing to
   * correspond to, nobody typed anything, and the sentence sends a volunteer
   * looking for a search box. §14.4 fixes the wording of that second case, and
   * the name of the awaited file is DERIVED from the station number — never
   * written in a message.
   */
  const empty = $derived.by(() => {
    if (session.catalogError !== '') {
      return { message: session.catalogError, hint: 'Le poste réessaie tout seul.' }
    }
    if (products.length > 0) {
      return { message: 'Aucun produit ne correspond.', hint: 'Effacez des lettres ou changez de rayon.' }
    }
    const station = snapshot?.station ?? 0
    return {
      message:
        station > 0
          ? `Catalogue vide. En attente du fichier flv_${station}.csv.`
          : 'Catalogue vide. En attente du fichier du poste.',
      hint: 'Prévenez un responsable : aucun produit ne peut être pesé.',
    }
  })

  /** The tile a refusal points at: an orange ribbon, and the grid stays visible. */
  const rejectedID = $derived(snapshot?.state === 'rejected' ? (snapshot.product?.id ?? null) : null)

  /**
   * The two states that take the WHOLE screen, and the only ones (§14.3).
   *
   * Derived once and read twice — by the keyboard guard and by the template
   * below — so that a state added to one can never be forgotten by the other.
   */
  const screenTaken = $derived(
    snapshot?.state === 'faulted' || snapshot?.state === 'out_of_service',
  )

  /**
   * The states in which a tile is still IN HAND, and therefore still ringed.
   *
   * `succeeded` is not one of them, and that is the whole point: the label has come
   * out, the sale is over, and the acknowledgement §14.3 asks for is in the banner
   * and on the paper. Ringing the tile after the fact left it green until the bag
   * was taken off the plate — which on a station without a scale, and on any
   * station whose customer walks away, is forever.
   */
  const HOLDING: readonly string[] = [
    'product_armed',
    'weight_present',
    'weight_stable',
    'awaiting_stability',
    'validating',
    'printing',
  ]
  const selectedID = $derived(
    snapshot !== null && HOLDING.includes(snapshot.state) ? (snapshot.product?.id ?? null) : null,
  )

  /** When the catalog on screen entered service, shown permanently (§14.3). */
  const catalogAt = $derived(catalogStamp(catalog?.updated_at ?? ''))

  $effect(() => {
    session.start()
    return () => session.stop()
  })

  // The tile is re-enabled by the STREAM, not by the HTTP answer: what the customer
  // must see acknowledged is the state of the station, not the acceptance of a POST.
  $effect(() => {
    const revision = session.state?.revision ?? -1
    if (busyID !== null && revision > busySinceRevision) {
      busyID = null
      busySinceRevision = -1
    }
  })

  /**
   * One tap, one label — including by unit.
   *
   * The key is generated HERE, on the pointerdown, and the Hub keeps the last 32:
   * a double tap prints once (§4, failure test 15).
   */
  async function pick(product: Product): Promise<void> {
    if (busyID !== null) return
    busyID = product.id
    busySinceRevision = session.state?.revision ?? -1
    try {
      await api.weigh({
        product_id: product.id,
        tare_g: session.state?.weight.tare_g ?? 0,
        units: 1,
        manual_weight_g: 0,
        seen_weight_g: session.state?.weight.gross_g ?? 0,
        measurement_seq: session.state?.weight.seq ?? 0,
        key: ulid(),
      })
    } catch {
      // The stream is what says whether anything happened; a failed POST only
      // means this tap did not reach the station, so the tile comes back.
      busyID = null
    }
  }

  /** Asks for the last label again. One reprint, and the label says RÉIMPRESSION. */
  async function reprint(): Promise<void> {
    const jobID = session.state?.reprint.job_id
    if (jobID === undefined || jobID === '') return
    await api.reprint(jobID, ulid())
  }

  /** Clears the query and hides the field. Closing IS the clearing (§14.3-3). */
  function clearQuery(): void {
    query = ''
    typedOpen = false
  }

  /** Reveals the field from the search key, with nothing typed yet. */
  function openTyped(): void {
    typedOpen = true
  }

  /** A single match: Enter weighs it, physical keyboard or click alike. */
  function pickIfSingleMatch(): void {
    const only = shown.length === 1 ? shown[0] : undefined
    if (only !== undefined) void pick(only)
  }

  /**
   * Global PHYSICAL keyboard listener: the station is driven with a mouse and
   * a keyboard, never a finger — this screen has no touch keyboard (§14.3-3,
   * revised 28/07/2026). Ignored while a real <input> has focus: SearchField
   * then handles its own typing natively.
   *
   * Two things on this screen come BEFORE the grid, and typing must reach
   * neither the search nor them:
   *
   *   - the tare pad is made of KEYS and not of an `<input>`, so nothing about
   *     a keystroke distinguishes « 500 grammes of jar » from a product being
   *     looked up. Left unguarded, the search field opens over the entry in
   *     progress and the tare the customer believed they typed exists nowhere.
   *   - a full screen is the only moment this screen is TAKEN (§14.3): what is
   *     typed behind it has no addressee, and a volunteer who dismisses the
   *     fault would find the grid filtered by letters nobody meant for it.
   */
  function onGlobalKey(event: KeyboardEvent): void {
    if (event.metaKey || event.ctrlKey || event.altKey) return
    if (event.target instanceof HTMLElement && event.target.tagName === 'INPUT') return
    if (tareEntry !== null || screenTaken) return
    if (event.key === 'Escape') {
      event.preventDefault()
      clearQuery()
      return
    }
    if (event.key === 'Backspace') {
      event.preventDefault()
      query = query.slice(0, -1)
      return
    }
    if (event.key === 'Enter') {
      pickIfSingleMatch()
      return
    }
    if (event.key.length !== 1) return
    if (!/[a-zA-Z0-9 ]/.test(event.key)) return
    if (event.key === ' ' && query === '') return
    event.preventDefault()
    query += event.key
  }

  $effect(() => {
    window.addEventListener('keydown', onGlobalKey)
    return () => window.removeEventListener('keydown', onGlobalKey)
  })

  /**
   * Loads the administration bundle, lazily, into this same window (§14.1).
   *
   * `mountAdmin` refuses a second mount BY ITSELF, and releases that refusal when the
   * screen closes. A guard kept here instead — as ADR-032 first shipped — never released,
   * so the key answered exactly once per page load: after one trip to the administration
   * and back, the way in was dead for good.
   */
  async function openAdmin(): Promise<void> {
    const module = await import('./admin/mount')
    module.mountAdmin(document.body)
  }
</script>

<main class="screen">
  <Banner
    {snapshot}
    showWeight={session.link.showWeight}
    linkBanner={session.link.banner}
    taring={tareEntry !== null}
    ontare={() => (tareEntry = '')}
  />

  {#if tareEntry !== null}
    <TarePad
      grams={tareEntry}
      ondigit={(d) => (tareEntry = (tareEntry ?? '') + d)}
      onclear={() => (tareEntry = '')}
      oncancel={() => (tareEntry = null)}
      onconfirm={() => (tareEntry = null)}
    />
  {/if}

  <Grid
    products={shown}
    {colors}
    primaryCode={catalog?.pricing.primary_code ?? ''}
    tierAbbrev={Object.fromEntries((catalog?.pricing.tiers ?? []).map((t) => [t.code, t.abbrev]))}
    {selectedID}
    {rejectedID}
    {busyID}
    showPrices={settings.show_grid_prices}
    loading={catalog === null && session.catalogError === ''}
    emptyMessage={empty.message}
    emptyHint={empty.hint}
    onpick={pick}
  />

  {#if fieldVisible}
    <SearchField {query} onquery={(q) => (query = q)} onclose={clearQuery} onenter={pickIfSingleMatch} />
  {/if}

  <CategoryBar
    chips={bar}
    active={activeCategory}
    searchFieldOpen={fieldVisible}
    onselect={(code) => (activeCategory = code)}
    onopensearch={openTyped}
  />

  <StatusBar
    label={snapshot?.last_label ?? null}
    available={snapshot?.reprint.available ?? false}
    {catalogAt}
    productCount={products.length}
    appVersion={catalog?.app_version ?? ''}
    {healthy}
    onreprint={reprint}
    onadmin={openAdmin}
  />

  <!-- Les deux seules prises de l'écran entier, et `screenTaken` les compte
       toutes les deux pour la garde clavier plus haut. -->
  {#if snapshot?.state === 'faulted'}
    <FullScreen
      title="Poste indisponible"
      detail={snapshot.message?.text ?? 'Prévenez un responsable.'}
      code={snapshot.fault_code}
      action={{ label: 'J’ai compris', run: () => void api.dismiss() }}
    />
  {:else if snapshot?.state === 'out_of_service'}
    <FullScreen
      title="Poste hors service"
      detail="Ce poste ne peut pas peser. Prévenez un responsable."
      code={snapshot.fault_code}
    />
  {/if}
</main>

<style>
  .screen {
    display: flex;
    flex-direction: column;
    height: 100%;
    /*
     * The four bands keep their height and the grid takes what is left, which is
     * why each of them declares `flex: 0 0 auto` and the grid `min-height: 0`.
     * Left to the default `flex-shrink: 1`, the overflow of a 331 tile grid is
     * spent on the elements that must not move: the banner loses its weight
     * display and the two PERMANENT bars of §14.3 are squeezed to nothing.
     */
    overflow: hidden;
  }
</style>
