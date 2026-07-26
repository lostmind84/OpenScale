<script lang="ts">
  import type { Product } from '../lib/catalog'
  import {
    NAME_SIZE_MAX_PX,
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
    /** Colour per category code, for the tiles that have no photo. */
    colors?: Record<string, string>
    selectedID?: string | null
    rejectedID?: string | null
    busyID?: string | null
    showPrices?: boolean
    /** French sentence to show when the grid is legitimately empty. */
    emptyMessage?: string
    onpick: (product: Product) => void
  }

  const {
    products,
    colors = {},
    selectedID = null,
    rejectedID = null,
    busyID = null,
    showPrices = true,
    emptyMessage = 'Aucun produit ne correspond.',
    onpick,
  }: Props = $props()

  /** Minimum column width of the CSS above, in px on the reference base. */
  const TILE_MIN_PX = 230
  /** Gap between two columns, in px: the 8 px minimum spacing of §14.2. */
  const GAP_PX = 8
  /** Horizontal padding INSIDE a tile, both sides, in px. */
  const TILE_PADDING_PX = 24

  let gridWidth = $state(0)

  /** Measures text against the real font, or null when there is no canvas. */
  const measure: Measurer | null = canvasMeasurer("'Inter Variable', system-ui, sans-serif", 700)

  /**
   * Reproduces the column count `auto-fill` computes, so the name can be sized
   * against the width a tile will really have.
   */
  const contentWidthPx = $derived.by(() => {
    if (gridWidth <= 0) return 0
    const columns = Math.max(1, Math.floor((gridWidth + GAP_PX) / (TILE_MIN_PX + GAP_PX)))
    return (gridWidth - (columns - 1) * GAP_PX) / columns - TILE_PADDING_PX
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

<div class="grid-scroll" bind:clientWidth={gridWidth}>
  {#if products.length === 0}
    <p class="empty">{emptyMessage}</p>
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
    gap: var(--touch-gap);
    align-content: start;
  }

  /* Full ink: « Catalogue vide. En attente du fichier flv_2.csv depuis 4 min » is
     the most important sentence this screen ever shows, and --ink-muted on --bg
     reaches only 6,57:1 against the 7:1 §14.2 demands at 28 px. */
  .empty {
    margin: 2rem;
    font-size: 1.75rem;
  }
</style>
