<script lang="ts">
  import type { Product } from '../lib/catalog'
  import { gridTemplateColumns } from '../lib/grid'
  import {
    NAME_BOX_PX,
    REFERENCE_SIZE_PX,
    canvasMeasurer,
    fitNameSize,
    nameSizeCeiling,
    type Measurer,
  } from '../lib/typography'
  import Tile from './Tile.svelte'

  /**
   * The grid, and there is only one of it.
   *
   * `repeat(auto-fill, minmax(var(--tile-min), 1fr))` by default: the density is
   * DERIVED from two physical constraints — a touch target of at least 72 px and a
   * 69 character name read at 60 to 80 cm — and `auto-fill` absorbs a change of
   * resolution on its own, which is why a 4K already shows ten columns without
   * anyone setting anything (§14.3-1, ADR-035).
   *
   * A station may OVERRIDE that with a column count, and only override it: how
   * many products to see at once is a shop's decision, and no screen measurement
   * answers it. `minmax(0, 1fr)` there and never `1fr`, which means
   * `minmax(auto, 1fr)`: an `auto` track does not go under the min-content width
   * of what it holds, and the catalog holds « CRANBERRY/CANNEBERGES ». That is the
   * trap `Tile.svelte:132` already documents — a tile that sized itself to 407 px
   * inside a 231 px column and drew over its neighbour.
   *
   * There is NO ceiling on the number of tiles, here or anywhere else. The legacy
   * application had 120 slots per category and crossed that in production: six
   * sellable products are invisible on every station today, with no message and no
   * log line. A list has no such failure mode.
   */
  interface Props {
    /** Every product to show. All of them are rendered — there is no window. */
    products: Product[]
    /**
     * `ui.grid_columns`: `0` — the default — leaves `auto-fill` to decide, any
     * other value is that many columns on every screen.
     */
    gridColumns?: number
    /** Colour per category code, for the plate every tile carries. */
    colors?: Record<string, string>
    /** Code of the tier printed large — which of `product.prices` is primary. */
    primaryCode?: string
    /** Abbreviation by tier code, e.g. `{ A: 'A', S: 'S' }`. */
    tierAbbrev?: Record<string, string>
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
    gridColumns = 0,
    colors = {},
    primaryCode = '',
    tierAbbrev = {},
    selectedID = null,
    rejectedID = null,
    busyID = null,
    showPrices = true,
    loading = false,
    emptyMessage = 'Aucun produit ne correspond.',
    emptyHint = '',
    onpick,
  }: Props = $props()

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

  /**
   * The width the tracks share, read off the grid ITSELF and not off its scroller.
   *
   * The scroller pads by `--touch-gap` on both sides, and its `clientWidth`
   * carries that padding: dividing THAT by a column count would make every column
   * 16 px wider than it is, and the scale that ratio produces wrong by as much.
   * The grid box has no padding of its own, so what it reports is what its tracks
   * divide — asked of the layout rather than recomputed from a token.
   */
  let gridWidth = $state(0)

  /**
   * Width of `--tile-min`, in px, read from an empty probe of that width.
   *
   * MEASURED and not read back from the stylesheet: `getComputedStyle` on an
   * unregistered custom property answers with its SUBSTITUTED value, the string
   * `clamp(15rem, 19vw, 22rem)`, and never with pixels. The token also lands
   * anywhere between 240 and 352 px depending on the screen, which is what the
   * constant this replaces got wrong every time it was not a 1366.
   *
   * 0 where nothing lays out — jsdom, and the first paint before the observer
   * fires. Everything below falls back to the automatic mode there.
   */
  let tileMinPx = $state(0)

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
   * Height of the block a name is fitted into, read from the layout.
   *
   * It changes with the density of the grid (ADR-031), and the fit has to follow:
   * a name fitted for a 84 px block and drawn in a 96 px one wastes a line, and the
   * other way round overflows. Measured on a probe of `height: var(--tile-name)`
   * rather than on a real tile — a tile whose name overflows is taller than its
   * block, and measuring THAT would feed the overflow back into the fit.
   */
  let nameBoxPx = $state(0)

  /**
   * How many columns the grid draws, or 0 while nothing can be told yet.
   *
   * Asked once and answered twice: a column count is the count itself, and
   * automatic is the same arithmetic `auto-fill` runs, on the width the probe
   * measured rather than on a constant that guessed it.
   */
  const columnCount = $derived.by(() => {
    if (gridColumns > 0) return gridColumns
    if (gridWidth <= 0 || tileMinPx <= 0) return 0
    return Math.max(1, Math.floor((gridWidth + GAP_PX) / (tileMinPx + GAP_PX)))
  })

  /** Width of one column, exactly as the grid divides its own width. */
  const columnWidthPx = $derived(
    columnCount > 0 && gridWidth > 0 ? (gridWidth - (columnCount - 1) * GAP_PX) / columnCount : 0,
  )

  /**
   * What a column count does to the rest of the tile: `--tile-scale` of `app.css`.
   *
   * DERIVED, never set by an operator — a column count describes a grid, and the
   * plate, the padding and the name block follow it by the ratio the layout
   * really produced. 1 in automatic mode and wherever nothing lays out, where
   * the tokens then compute exactly the values they compute today.
   *
   * There is no loop to fear in that ratio: a column is as wide as the grid is,
   * divided by a count, and the gutter it subtracts is `--touch-gap`, which the
   * scale does not touch. Nothing here measures anything the scale then changes.
   */
  const tileScale = $derived(
    gridColumns > 0 && columnWidthPx > 0 && tileMinPx > 0 ? columnWidthPx / tileMinPx : 1,
  )

  /** The body every name starts its shrink from; the floor never moves with it. */
  const nameCeilingPx = $derived(nameSizeCeiling(tileScale))

  /**
   * The column declaration, and only when it OVERRIDES the one in the stylesheet.
   *
   * Automatic gets no inline style at all, so the stylesheet below stays the
   * single place the default is written — and stays true word for word.
   */
  const columnsStyle = $derived(
    gridColumns > 0 ? `grid-template-columns: ${gridTemplateColumns(gridColumns)}` : undefined,
  )

  /**
   * The usable width of a tile, for the FIRST paint only.
   *
   * The column width above is exact — a count divides a measured width — but what
   * a tile spends before its text is still the estimate {@link TILE_INSET_PX} has
   * always been, and estimating is exactly what went wrong before: the two
   * hairline borders of a tile were missing from it, the names were fitted
   * against 207 px and laid out in 205, and « GRAINES DE CHIA BIO » took the
   * third line the fit had ruled out — on a screen whose tiles are supposed to
   * share one height. The second paint measures, and that one has not changed.
   */
  const estimatedWidthPx = $derived(columnWidthPx > 0 ? columnWidthPx - TILE_INSET_PX : 0)

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
    const box = nameBoxPx > 0 ? nameBoxPx : NAME_BOX_PX
    for (const p of products) {
      sizes.set(p.id, fitNameSize(p.name, contentWidthPx, measure, box, nameCeilingPx))
    }
    return sizes
  })
</script>

<!-- Le facteur est porté par le scroller, et non par la racine du document :
     l'administration s'ouvre dans LA MÊME fenêtre (§14.1), et ses sondes posent
     le leur pour mesurer un brouillon. Deux échelles à la fois, chacune sur son
     sous-arbre, plutôt qu'une seule que les deux écrans se disputent.
     `data-tile-scale` est le crochet de sélection d'`app.css` ; la propriété
     personnalisée en porte la valeur. -->
<div
  class="grid-scroll"
  bind:this={root}
  data-tile-scale={tileScale}
  style="--tile-scale: {tileScale}"
>
  <!-- La hauteur du bloc de nom, lue dans la mise en page plutôt que recalculée :
       elle change avec la densité, et une sonde vide ne peut pas déborder. -->
  <span class="probe" aria-hidden="true" bind:clientHeight={nameBoxPx}></span>
  <!-- La largeur de --tile-min, mesurée pour la même raison : c'est le dénominateur
       du facteur d'échelle, et une propriété personnalisée ne se lit pas en pixels. -->
  <span class="min-probe" aria-hidden="true" bind:clientWidth={tileMinPx}></span>

  {#if loading}
    <!-- The shape of the grid, drawn before its content: a station that has just
         been switched on shows what is coming instead of a sentence in a void.
         Nothing here pulses — an animation that repeats is exactly what §14.2
         forbids on a screen someone stands in front of all day. -->
    <div class="grid" aria-hidden="true" style={columnsStyle} bind:clientWidth={gridWidth}>
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
    <div
      class="grid"
      role="list"
      data-tile-count={products.length}
      style={columnsStyle}
      bind:clientWidth={gridWidth}
    >
      {#each products as product (product.id)}
        <div role="listitem">
          <Tile
            {product}
            nameSizePx={nameSizes.get(product.id) ?? nameCeilingPx}
            categoryColor={colors[product.category_code]}
            {primaryCode}
            {tierAbbrev}
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

  .probe {
    position: absolute;
    width: 0;
    height: var(--tile-name);
    visibility: hidden;
    pointer-events: none;
  }

  /* The other probe, and the only one that must NOT carry the scale: it is what
     the scale is measured against. */
  .min-probe {
    position: absolute;
    width: var(--tile-min);
    height: 0;
    visibility: hidden;
    pointer-events: none;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--tile-min), 1fr));
    /* Every row is drawn at the same height, and a row grows only when a name
       overflows even at the floor — then its tiles grow TOGETHER. */
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
