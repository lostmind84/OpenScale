<script lang="ts">
  import Banner from './components/Banner.svelte'
  import FilterBar from './components/FilterBar.svelte'
  import FullScreen from './components/FullScreen.svelte'
  import Grid from './components/Grid.svelte'
  import ReprintBar from './components/ReprintBar.svelte'
  import SearchPanel from './components/SearchPanel.svelte'
  import TarePad from './components/TarePad.svelte'
  import * as api from './lib/api'
  import { ALL_CATEGORIES, chips, filterProducts, visibleProducts, type Product } from './lib/catalog'
  import { Session } from './lib/session.svelte'
  import { ulid } from './lib/ulid'

  /** The client screen: one grid, one banner, two bottom bars, nothing modal. */
  const session = new Session()

  let activeCategory = $state(ALL_CATEGORIES)
  let searchOpen = $state(false)
  let query = $state('')
  /** The tile disabled from the pointerdown until the stream answers (§14.3). */
  let busyID = $state<string | null>(null)
  let busySinceRevision = $state(-1)
  let tareEntry = $state<string | null>(null)

  const catalog = $derived(session.catalog)
  const products = $derived(catalog === null ? [] : visibleProducts(catalog))
  const bar = $derived(catalog === null ? [] : chips(catalog))
  const shown = $derived(filterProducts(products, activeCategory, searchOpen ? query : ''))
  const colors = $derived.by(() => {
    const map: Record<string, string> = {}
    for (const c of catalog?.categories ?? []) map[c.code] = c.color
    return map
  })

  // Named `snapshot` and not `state`: a variable called `state` makes every
  // `$state` rune ambiguous with the `$store` prefix, and svelte-check says so.
  const snapshot = $derived(session.state)
  const settings = $derived(session.presentation)
  const healthy = $derived(
    snapshot === null || (snapshot.scale.connected && snapshot.printer.health !== 'faulted'),
  )
  /** The tile a refusal points at: an orange ribbon, and the grid stays visible. */
  const rejectedID = $derived(snapshot?.state === 'rejected' ? (snapshot.product?.id ?? null) : null)
  const selectedID = $derived(snapshot?.state === 'rejected' ? null : (snapshot?.product?.id ?? null))

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

  /** Opens or closes the search panel. Closing IS the clearing (§14.3-3). */
  function toggleSearch(): void {
    searchOpen = !searchOpen
    if (!searchOpen) query = ''
  }

  /** Loads the administration bundle, lazily, into this same window (§14.1). */
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
    {selectedID}
    {rejectedID}
    {busyID}
    showPrices={settings.show_grid_prices}
    emptyMessage={session.catalogError ||
      (catalog === null ? 'Chargement du catalogue…' : 'Aucun produit ne correspond.')}
    onpick={pick}
  />

  {#if searchOpen}
    <SearchPanel
      {query}
      matches={shown.length}
      onquery={(q) => (query = q)}
      onclose={toggleSearch}
    />
  {/if}

  <ReprintBar
    label={snapshot?.last_label ?? null}
    available={snapshot?.reprint.available ?? false}
    onreprint={reprint}
  />

  <FilterBar
    chips={bar}
    active={activeCategory}
    {searchOpen}
    {healthy}
    onselect={(code) => (activeCategory = code)}
    ontogglesearch={toggleSearch}
    onadmin={openAdmin}
  />

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
  }
</style>
