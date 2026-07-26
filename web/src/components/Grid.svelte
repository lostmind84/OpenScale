<script lang="ts">
  import type { Product } from '../lib/catalog'
  import {
    NAME_SIZE_MAX_PX,
    REFERENCE_SIZE_PX,
    canvasMeasurer,
    fitNameSize,
    type Measurer,
  } from '../lib/typography'
  import Tile from './Tile.svelte'

  /**
   * The grid, and there is only one of it.
   *
   * `repeat(auto-fill, minmax(230px, 1fr))` with a single density: the density is
   * DERIVED from two physical constraints — a touch target of at least 72 px and a
   * 69 character name read at 60 to 80 cm — so there is no comfort/dense setting
   * to arbitrate, and `auto-fill` absorbs a change of resolution (§14.3-1).
   *
   * There is NO ceiling on the number of tiles, here or anywhere else. The legacy
   * application had 120 slots per category and crossed that in production: six
   * sellable products are invisible on every station today, with no message and no
   * log line. A list has no such failure mode.
   */
  interface Props {
    /** Every product to show. All of them are rendered — there is no window. */
    products: Product[]
    /** Colour per category code, for the plate every tile carries. */
    colors?: Record<string, string>
    selectedID?: string | null
    rejectedID?: string | null
    busyID?: string | null
    showPrices?: boolean
    /** True while the catalog has not arrived yet: the grid draws its own shape. */
    loading?: boolean
    /** French sentence to show when the grid is legitimately empty. */
    emptyMessage?: string
    /** What to do about it, when there is something to do. */
    emptyHint?: string
    onpick: (product: Product) => void
  }

  const {
    products,
    colors = {},
    selectedID = null,
    rejectedID = null,
    busyID = null,
    showPrices = true,
    loading = false,
    emptyMessage = 'Aucun produit ne correspond.',
    emptyHint = '',
    onpick,
  }: Props = $props()

  /** Minimum column width of the CSS above, in px on the reference base. */
  const TILE_MIN_PX = 230
  /** Gap between two columns, in px: the 8 px minimum spacing of §14.2. */
  const GAP_PX = 8
  /** What a tile spends before its text: 2 x 12 px of padding, 2 x 1 px of border. */
  const TILE_INSET_PX = 26
  /** The family and weight tile names are drawn with, measured against verbatim. */
  const NAME_FONT = "'Inter Variable', system-ui, sans-serif"
  /** The face alone, because `FontFaceSet.load` parses a font SHORTHAND, not a stack. */
  const NAME_FACE = "'Inter Variable'"
  /** Tiles drawn while the catalog is on its way. Two rows of the reference screen. */
  const SKELETON_COUNT = 16

  let gridWidth = $state(0)

  /**
   * False until the embedded font is the one a canvas would measure.
   *
   * A `canvas` asked for « Inter Variable » before the WOFF2 has loaded answers
   * with the metrics of the FALLBACK family, and the difference is not academic:
   * names came out fitted against a narrower face, then rendered in Inter, and
   * « TOURNESOL DECORTIQUE » was cut into « TOURNESO / L DECORTIQU / E » on a
   * screen whose one promise is that a name is never cut.
   */
  let fontsReady = $state(false)

  $effect(() => {
    // `load()` and not `ready`: `document.fonts.ready` settles as soon as nothing
    // is PENDING, and at first paint nothing is — the face has not been asked for
    // yet. Naming the face is what requests it and what makes the promise mean
    // « Inter is now what a canvas measures ».
    //
    // `document.fonts` is absent from jsdom and from any browser older than the
    // Font Loading API; there, the first measurement is the only one, which is
    // exactly what happened before this existed.
    const fonts = document.fonts
    if (fonts === undefined) return
    void fonts
      .load(`700 ${REFERENCE_SIZE_PX}px ${NAME_FACE}`)
      // A face that cannot load is not a reason to leave the grid unfitted: the
      // fallback metrics are what the browser will draw with anyway.
      .catch(() => [])
      .then(() => {
        fontsReady = true
      })
  })

  /**
   * Measures text against the real font, or null when there is no canvas.
   *
   * Rebuilt when the font arrives, because the measurer memoises: keeping it
   * would keep the widths of the fallback face along with it.
   */
  const measure = $derived.by<Measurer | null>(() => {
    void fontsReady
    return canvasMeasurer(NAME_FONT, 700)
  })

  /** The scroller, so a tile already on screen can be asked for its real width. */
  let root = $state<HTMLElement | null>(null)

  /** Width of a name block as the browser laid it out, or 0 before the first one. */
  let measuredWidthPx = $state(0)

  /**
   * Reproduces the column count `auto-fill` computes, for the FIRST paint only.
   *
   * It is an estimate, and estimating is exactly what went wrong before: the two
   * hairline borders of a tile were missing from it, the names were fitted
   * against 207 px and laid out in 205, and « GRAINES DE CHIA BIO » took the
   * third line the fit had ruled out — on a screen whose tiles are supposed to
   * share one height.
   */
  const estimatedWidthPx = $derived.by(() => {
    if (gridWidth <= 0) return 0
    const columns = Math.max(1, Math.floor((gridWidth + GAP_PX) / (TILE_MIN_PX + GAP_PX)))
    return (gridWidth - (columns - 1) * GAP_PX) / columns - TILE_INSET_PX
  })

  /**
   * The width a name really has, asked of the layout rather than recomputed.
   *
   * Self-correcting: whatever a future padding, border or gutter does to a tile,
   * the number the fit uses is the number the browser used. The first paint runs
   * on the estimate above, the second on the measurement, and the two agree.
   */
  const contentWidthPx = $derived(measuredWidthPx > 0 ? measuredWidthPx : estimatedWidthPx)

  $effect(() => {
    // Re-measured when the width changes and when the set of tiles changes,
    // which are the only two ways a column can become something else.
    void gridWidth
    void products.length
    const box = root?.querySelector<HTMLElement>('.name-box')
    measuredWidthPx = box?.clientWidth ?? 0
  })

  /**
   * The body of every name, decided once per grid width rather than per tile.
   *
   * A single map for the whole catalog: recomputing 331 fits on every keystroke
   * of the search would be the one place where this screen could feel slow.
   */
  const nameSizes = $derived.by(() => {
    const sizes = new Map<string, number>()
    if (measure === null || contentWidthPx <= 0) return sizes
    for (const p of products) {
      sizes.set(p.id, fitNameSize(p.name, contentWidthPx, measure))
    }
    return sizes
  })
</script>

<div class="grid-scroll" bind:this={root} bind:clientWidth={gridWidth}>
  {#if loading}
    <!-- The shape of the grid, drawn before its content: a station that has just
         been switched on shows what is coming instead of a sentence in a void.
         Nothing here pulses — an animation that repeats is exactly what §14.2
         forbids on a screen someone stands in front of all day. -->
    <div class="grid" aria-hidden="true">
      {#each { length: SKELETON_COUNT } as _, i (i)}
        <div class="ghost">
          <span class="ghost-head">
            <span class="ghost-plate"></span>
            <span class="ghost-price"></span>
          </span>
          <span class="ghost-line"></span>
          <span class="ghost-line short"></span>
        </div>
      {/each}
    </div>
    <p class="loading" role="status">Chargement du catalogue…</p>
  {:else if products.length === 0}
    <div class="state">
      <p class="empty">{emptyMessage}</p>
      {#if emptyHint !== ''}
        <p class="hint">{emptyHint}</p>
      {/if}
    </div>
  {:else}
    <div class="grid" role="list" data-tile-count={products.length}>
      {#each products as product (product.id)}
        <div role="listitem">
          <Tile
            {product}
            nameSizePx={nameSizes.get(product.id) ?? NAME_SIZE_MAX_PX}
            categoryColor={colors[product.category_code]}
            selected={product.id === selectedID}
            rejected={product.id === rejectedID}
            busy={product.id === busyID}
            showPrice={showPrices}
            {onpick}
          />
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .grid-scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    /* Inertial scrolling, and no rubber band past the ends. */
    -webkit-overflow-scrolling: touch;
    overscroll-behavior: contain;
    padding: var(--touch-gap);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--tile-min), 1fr));
    /* Every row is drawn at the same height, and a row grows only when a name
       overflows even at the 18 px floor — then its tiles grow TOGETHER. */
    grid-auto-rows: minmax(var(--tile-height), auto);
    gap: var(--touch-gap);
    align-content: start;
  }

  /* Centred in what is left of the screen, and not dropped at the top of an
     empty page: this is the only thing there is to read. */
  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.75rem;
    height: 100%;
    padding: 2rem;
    text-align: center;
  }

  /* Full ink: « Catalogue vide. En attente du fichier flv_2.csv » is the most
     important sentence this screen ever shows, and --ink-muted on --bg reaches
     only 6,57:1 against the 7:1 §14.2 demands at 28 px. */
  .empty {
    margin: 0;
    max-width: 46rem;
    font-size: 2rem;
    font-weight: 600;
    line-height: 1.3;
    text-wrap: balance;
  }

  .hint {
    margin: 0;
    max-width: 40rem;
    color: var(--ink-muted);
    font-size: 1.5rem;
    line-height: 1.35;
    text-wrap: balance;
  }

  .loading {
    margin: 1.5rem 0 0;
    color: var(--ink-muted);
    font-size: 1.5rem;
    text-align: center;
  }

  /* The ghost is the tile, band for band: what appears is what was outlined. */
  .ghost {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    height: var(--tile-height);
    padding: var(--tile-pad);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .ghost-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    height: var(--tile-media);
  }

  .ghost-plate {
    width: var(--tile-media);
    height: var(--tile-media);
    border-radius: var(--radius-sm);
    background: var(--bg);
  }

  .ghost-price {
    width: 6rem;
    height: 1.25rem;
    margin-left: auto;
    border-radius: var(--radius-sm);
    background: var(--bg);
  }

  .ghost-line {
    height: 1.75rem;
    border-radius: var(--radius-sm);
    background: var(--bg);
  }

  .ghost-line.short {
    width: 55%;
  }
</style>
