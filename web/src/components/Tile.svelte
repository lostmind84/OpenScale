<script lang="ts">
  import type { Product } from '../lib/catalog'
  import { unitPrice } from '../lib/format'
  import { NAME_SIZE_MAX_PX } from '../lib/typography'

  /** One tile of the grid: a name, optionally a photo, optionally a price. */
  interface Props {
    product: Product
    /** Body the name is rendered at, in px, decided once per grid width. */
    nameSizePx?: number
    /** Colour of the category, used by the initial when there is no photo. */
    categoryColor?: string
    /** The tile the customer has chosen — armed, being validated or printing. */
    selected?: boolean
    /** The tile a safeguard refused: an orange ribbon, and the grid stays visible. */
    rejected?: boolean
    /** True from the pointerdown until the stream answers: no double label (§14.3). */
    busy?: boolean
    /** Prices are shown when the station configuration says so (`ui.show_grid_prices`). */
    showPrice?: boolean
    onpick: (product: Product) => void
  }

  const {
    product,
    nameSizePx = NAME_SIZE_MAX_PX,
    categoryColor = 'var(--waiting)',
    selected = false,
    rejected = false,
    busy = false,
    showPrice = true,
    onpick,
  }: Props = $props()

  /**
   * The letter drawn when a product has no photo.
   *
   * There is neither a hole nor a grey frame in that case: 174 of the 355 real
   * products have no image, so a tile without one is not a degraded case to
   * paper over, it is one product in two (§14.2).
   */
  const initial = $derived(firstLetterOf(product.name))

  /** Returns the first letter of a name, skipping the `♥` that opens 85 of them. */
  function firstLetterOf(name: string): string {
    const letter = name.match(/\p{L}/u)
    return letter === null ? '·' : letter[0].toUpperCase()
  }
</script>

<button
  type="button"
  class="tile touch-target"
  class:selected
  class:rejected
  data-product-id={product.id}
  disabled={busy}
  onpointerdown={() => onpick(product)}
>
  {#if product.image_url}
    <img class="photo" src={product.image_url} alt="" loading="lazy" decoding="async" />
  {:else}
    <span class="initial" style:background={categoryColor} aria-hidden="true">{initial}</span>
  {/if}
  <span class="name" style:font-size="{nameSizePx}px">{product.name}</span>
  {#if showPrice}
    <span class="price">{unitPrice(product.unit_price_text, product.price_suffix)}</span>
  {/if}
</button>

<style>
  .tile {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 0.25rem;
    padding: 0.75rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-align: left;
    /* The ribbon SLIDES, it never blinks (§14.2). Nothing here exceeds 200 ms. */
    box-shadow: inset 0 0 0 0 var(--ready);
    transition: box-shadow 160ms ease-out;
  }

  .tile:disabled {
    opacity: 0.55;
  }

  .tile.selected {
    box-shadow: inset 0.25rem 0 0 0 var(--ready);
  }

  .tile.rejected {
    box-shadow: inset 0.25rem 0 0 0 var(--warning);
  }

  .photo {
    width: 100%;
    height: 5rem;
    object-fit: contain;
  }

  .initial {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 5rem;
    border-radius: calc(var(--radius) / 2);
    color: var(--surface);
    font-size: 2.5rem;
    font-weight: 700;
  }

  .name {
    font-weight: 700;
    line-height: 1.1;
    /* No ellipsis, no clamp, no truncation: the body gives way instead (§14.2). */
    overflow-wrap: anywhere;
  }

  .price {
    color: var(--ink-muted);
    font-size: 1.5rem;
  }
</style>
