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
    /** Code of the tier printed large — which of `product.prices` is primary. */
    primaryCode?: string
    /** Abbreviation by tier code, e.g. `{ A: 'A', S: 'S' }`. */
    tierAbbrev?: Record<string, string>
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
    primaryCode = '',
    tierAbbrev = {},
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

  <span class="name-box">
    <span class="name" style:font-size="{nameSizePx}px">{product.name}</span>
  </span>

  {#if showPrice}
    <span class="prices">
      {#each product.prices as price (price.code)}
        <span class="price" class:secondary={price.code !== primaryCode}>
          <span class="abbrev" class:hollow={price.code !== primaryCode}>
            {tierAbbrev[price.code] ?? ''}
          </span>
          <span class="amount">{price.text}</span>
          <span class="unit">{product.price_suffix.trim()}</span>
        </span>
      {/each}
    </span>
  {/if}
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
    /*
     * The two bodies of the price block: each follows the tile, and BOTH stop at
     * the same floor.
     *
     * MEASURED 01/08/2026: left constant while the tile shrank, the price ran out
     * of its tile from 10 columns on 1920 and 7 on 1366, and the client screen
     * grew a HORIZONTAL SCROLLBAR — 22 px at 12 columns on 1920, 66 px on 1366,
     * with « 20,09 €/kg » cropped to « 20,09 €/ » by the edge of the screen. A
     * kiosk has no horizontal scrollbar and nobody to drag it.
     *
     * `--text-min` on BOTH, and that is the point: the floor is the same nature
     * as the name's — a limit of legibility at 60 to 80 cm, never a proportion —
     * and it knows no exception for who is paying. Scaled by the ratio alone, the
     * second tier came out at 15,1 px at 7 columns on 1920, under a floor that
     * exists to say « below this, nobody reads it ». A floor that holds for one
     * price out of two is not a floor.
     *
     * What the two bodies meeting at that floor costs is ONE signal out of four:
     * the ink (`--ink` against `--ink-muted`), the weight (700 against 400) and
     * the badge (solid against hollow) carry the hierarchy of ADR-036 intact.
     * What it costs in HEIGHT has still to be measured — two prices at the floor
     * make a taller block than 18 and 12,8 px, so this may reopen some of the
     * overflow the scaling closes. The second campaign says; and if it does, the
     * upper bound of the setting is what moves, not this floor.
     *
     * Declared HERE, on the tile, and not on `.price`: a custom property resolves
     * its `var()` at the element that declares it, and `--tile-scale` is
     * inherited from the grid that deduced it.
     */
    --price-size: max(var(--text-min), 1.75rem * var(--tile-scale));
    --price-size-secondary: max(var(--text-min), 1.25rem * var(--tile-scale));
    padding: var(--tile-pad);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
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
   * The plate is now the FULL width of the tile — the grand format mockup gives
   * the photo the whole row rather than sharing it with the price, which moves
   * below the name into its own stacked block (§14.2, ADR-036).
   */
  .plate {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 100%;
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
    font-size: 2.5rem;
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
   * Le double tarif est empilé, primaire d'abord — gros badge plein,
   * secondaire ensuite — anneau creux, plus petit et plus clair : ce qui a la
   * plus grande surface est ce que le client paie s'il est adhérent (§14.2,
   * ADR-036).
   */
  .prices {
    display: flex;
    flex-direction: column;
    /* Scaled like the bodies they space out; the hairline above them is not — a
       0,4 px rule is not a rule, same argument as the borders of --tile-height. */
    gap: calc(0.25rem * var(--tile-scale));
    margin-top: auto;
    padding-top: calc(0.5rem * var(--tile-scale));
    border-top: 1px solid var(--border);
  }

  .price {
    display: flex;
    align-items: baseline;
    gap: calc(0.5rem * var(--tile-scale));
    color: var(--ink);
    font-size: var(--price-size);
    font-weight: 700;
  }

  /*
   * Smaller than the primary as long as the floor allows it, and never smaller
   * than the floor. The two are declared side by side on the tile, so the rule
   * « both scale, both stop » is read in one place rather than deduced.
   */
  .price.secondary {
    color: var(--ink-muted);
    font-size: var(--price-size-secondary);
    font-weight: 400;
  }

  .abbrev {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 1.5em;
    height: 1.5em;
    border-radius: var(--radius-inner);
    background: var(--ink);
    color: var(--surface);
    font-size: 0.8em;
    font-weight: 700;
    line-height: 1;
  }

  .abbrev.hollow {
    background: none;
    color: var(--ink-muted);
    box-shadow: inset 0 0 0 2px var(--border);
  }

  .unit {
    color: var(--ink-muted);
    font-size: 0.7em;
    font-weight: 400;
  }
</style>
