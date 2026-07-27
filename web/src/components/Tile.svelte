<script lang="ts">
  import type { Product } from '../lib/catalog'
  import { readable, wash } from '../lib/color'
  import { NAME_SIZE_MAX_PX } from '../lib/typography'

  /** One tile of the grid: a name, optionally a photo, optionally a price. */
  interface Props {
    product: Product
    /** Body the name is rendered at, in px, decided once per grid width. */
    nameSizePx?: number
    /** Colour of the category, as the station configuration spells it. */
    categoryColor?: string
    /** The tile the customer has chosen — armed, being validated or printing. */
    selected?: boolean
    /** The tile a safeguard refused: an orange ring, and the grid stays visible. */
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
    categoryColor = '#8a867c',
    selected = false,
    rejected = false,
    busy = false,
    showPrice = true,
    onpick,
  }: Props = $props()

  /**
   * The photo that failed to load, if one did.
   *
   * A tile is drawn from a catalog served minutes ago; between the two, an image
   * can 404 — a purge, an import that dropped a file, a disk that went read-only.
   * The browser answers a broken `<img>` with its own torn-page glyph, which is
   * the one drawing this screen must never show. Recording the URL rather than a
   * boolean is what lets a tile recover when a later import gives it a new photo.
   */
  let failedURL = $state('')

  const hasPhoto = $derived(product.image_url !== '' && product.image_url !== failedURL)

  /**
   * The letter drawn when a product has no photo.
   *
   * There is neither a hole nor a grey frame in that case: 154 of the 331 real
   * tiles have no image, so a tile without one is not a degraded case to paper
   * over, it is one product in two (§14.2). Photo or letter, the plate is the
   * same size, the same colour and in the same place.
   */
  const initial = $derived(firstLetterOf(product.name))

  /** The shelf colour, washed for the plate and darkened for the letter. */
  const plate = $derived(wash(categoryColor))
  const ink = $derived(readable(categoryColor))

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
  <span class="head">
    <span class="plate" style:background={plate}>
      {#if hasPhoto}
        <img
          class="photo"
          src={product.image_url}
          alt=""
          loading="lazy"
          decoding="async"
          onerror={() => (failedURL = product.image_url)}
        />
      {:else}
        <span class="initial" style:color={ink} aria-hidden="true">{initial}</span>
      {/if}
    </span>

    {#if showPrice}
      <span class="price">
        <span class="amount">{product.unit_price_text}</span>
        <span class="unit">{product.price_suffix.trim()}</span>
      </span>
    {/if}
  </span>

  <span class="name-box">
    <span class="name" style:font-size="{nameSizePx}px">{product.name}</span>
  </span>
</button>

<style>
  .tile {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 0.5rem;
    /*
     * A tile is the size of its cell, and it has to be SAID.
     *
     * A button element keeps the shrink-to-fit sizing of a form control even
     * when made a flex container: given « CRANBERRY/CANNEBERGES », the tile sized
     * itself to 407 px inside a 231 px column and drew over its neighbour. Every
     * length below this line depends on this one.
     */
    width: 100%;
    /* Full height of its grid row: 331 products draw one rhythm and not 331
       heights, whatever the length of a name or the shape of a photo (ADR-030). */
    height: 100%;
    padding: var(--tile-pad);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--shadow-1);
    text-align: left;
    /* The ring SLIDES, it never blinks (§14.2). Nothing here exceeds 200 ms. */
    transition:
      box-shadow var(--slide) var(--ease),
      border-color var(--slide) var(--ease),
      background-color var(--slide) var(--ease),
      transform var(--tap) var(--ease);
  }

  /* Sous une souris, la tuile se soulève : c'est le seul retour qu'un pointeur
     obtient avant d'appuyer, et son absence fait lire une grille comme une image. */
  @media (hover: hover) {
    .tile:hover:not(:disabled) {
      border-color: var(--ink-muted);
      box-shadow: var(--shadow-2);
      transform: translateY(-1px);
    }
  }

  /*
   * Touched, and the station has not answered yet.
   *
   * The tile is disabled from the pointerdown until the stream replies, so that a
   * double tap prints once (§14.3) — but a customer must read that as « taken »
   * and not as « unavailable », which is what fading it out says. A grey ring
   * that turns green the moment the station answers is the same drawing as the
   * selected state, one colour earlier.
   */
  .tile:disabled {
    border-color: var(--waiting);
    box-shadow: 0 0 0 3px var(--waiting) inset, var(--shadow-1);
    transform: scale(0.98);
    cursor: default;
  }

  /*
   * The chosen tile and the refused one are the same drawing in two colours: a
   * ring around the whole card, not a mark in a corner. A customer standing back
   * from the screen sees a card change, which is a shape, before they see a
   * colour, which at 80 cm on a busy grid is a detail.
   */
  .tile.selected {
    border-color: var(--ready);
    background: var(--ready-wash);
    box-shadow: 0 0 0 3px var(--ready) inset, var(--shadow-2);
  }

  .tile.rejected {
    border-color: var(--warning);
    background: var(--warning-wash);
    box-shadow: 0 0 0 3px var(--warning) inset, var(--shadow-2);
  }

  /*
   * The plate and the price share one row, and that is what buys the fourth row
   * of the grid: a photo band the width of the tile left two thirds of itself
   * empty on every one of the 331 tiles.
   */
  .head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex: 0 0 auto;
    height: var(--tile-media);
  }

  .plate {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: var(--tile-media);
    height: var(--tile-media);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .photo {
    max-width: 100%;
    height: 100%;
    object-fit: contain;
    /*
     * The photos of the real catalog are 64 px thumbnails cut out on white. Over
     * the shelf wash, `multiply` makes that white disappear and leaves the
     * product standing on the colour of its shelf; a photo that is not cut out
     * merely deepens by the 10 % the wash carries. One line for the one thing
     * that made two neighbouring tiles look like two different designs.
     */
    mix-blend-mode: multiply;
  }

  .initial {
    font-size: 2rem;
    font-weight: 800;
    line-height: 1;
  }

  /*
   * The name lives in a block of FIXED height, and the body gives way inside it.
   *
   * This is the whole geometry of the grid: `fitNameSize` picks the largest body
   * whose wrapped lines fit here, so eight characters and sixty-nine come out
   * occupying the same space. Centred, because a one-line name and a four-line
   * name must both look posed rather than dropped.
   */
  .name-box {
    display: flex;
    align-items: center;
    flex: 1 1 auto;
    min-height: var(--tile-name);
  }

  .name {
    /*
     * The width is IMPOSED, and that is what makes a long word wrap.
     *
     * Left to itself, a flex item is laid out at its max-content width, and
     * `overflow-wrap: break-word` — unlike `anywhere` — does not shrink that:
     * « CRANBERRY/CANNEBERGES » sized its own box to 381 px and ran out of a
     * 205 px tile sideways. Given the width of its block, the same declaration
     * wraps it, and only it — every other name still moves whole words down.
     */
    width: 100%;
    min-width: 0;
    font-weight: 700;
    line-height: 1.15;
    /*
     * A name is prose, not a readout, and this is what makes it MEASURABLE.
     *
     * `tabular-nums` is set on `body` so the weight does not jump sideways on
     * every decimal (§14.2), and it is inherited here. It widens far more than
     * digits: on Inter it widens the hyphen and the per-cent sign too, so
     * « Arc-en-Ciel » lays out 6 % wider than a canvas — which has no such
     * setting — says it will. That 6 % is a whole extra line in a block sized for
     * two. The figures a customer reads on a tile are in the price, which keeps
     * its tabular ones.
     */
    font-variant-numeric: normal;
    /* No ellipsis, no clamp, no truncation: the body gives way instead (§14.2).
       `break-word` and not `anywhere`: a word that fits on a line of its own goes
       down whole, and only a word wider than the tile is ever cut. */
    overflow-wrap: break-word;
  }

  /*
   * Le montant et son unité sont EMPILÉS, et toujours.
   *
   * Sur une ligne, « 4,50 €/kg » tenait à côté de la plaque et « 17,63 €/kg » non :
   * la moitié des tuiles repliaient leur prix et l'autre pas, ce qui redonnait à la
   * grille le désordre qu'ADR-030 lui a retiré. Empilé, le bloc a la même forme sur
   * les 331 tuiles, quel que soit le tarif — « € le litre » compris.
   */
  .price {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    flex: 1 1 auto;
    min-width: 0;
    color: var(--ink);
    font-size: 1.5rem;
    font-weight: 600;
    line-height: 1.15;
    text-align: right;
  }

  .unit {
    color: var(--ink-muted);
    font-size: 1.125rem;
    font-weight: 400;
  }
</style>
