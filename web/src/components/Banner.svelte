<script lang="ts">
  import type { MessageDTO, StateDTO } from '../lib/dto'
  import Icon from './Icon.svelte'

  /**
   * The top banner: the weight, the instruction, the tare.
   *
   * This is where everything that used to be a full-screen overlay now lives. The
   * screen is only taken over when the station CANNOT SERVE — `Faulted`,
   * `OutOfService`, invalid configuration. A success acknowledgement, a refusal, a
   * stability wait and a tare entry all happen here, with the grid still visible
   * behind (§14.3, ADR-023).
   */
  interface Props {
    snapshot: StateDTO | null
    /** False while the link is silent: the weight is hidden rather than stale. */
    showWeight: boolean
    /** French line about the link — « Reconnexion… », « Poids indisponible ». */
    linkBanner: string
    /** True while the pad below is open: the key shows where the customer is. */
    taring?: boolean
    ontare: () => void
  }

  const { snapshot, showWeight, linkBanner, taring = false, ontare }: Props = $props()

  /**
   * The instruction, which is the only thing most customers ever read.
   *
   * It is derived from the state rather than pushed by the server for the states
   * that carry no server message: a station that says nothing must still say what
   * to do.
   */
  const instruction = $derived.by(() => {
    if (linkBanner !== '') return linkBanner
    const message: MessageDTO | null = snapshot?.message ?? null
    if (message !== null) return message.text
    switch (snapshot?.state) {
      case 'product_armed':
        return 'Posez votre produit'
      case 'awaiting_stability':
        return 'Pesée en cours…'
      case 'printing':
        return 'Étiquette en cours…'
      case 'entering_tare':
        return 'Tapez la tare en grammes'
      case 'scale_lost':
        return 'Le poids n’est plus disponible.'
      default:
        return 'Touchez votre produit, l’étiquette sort'
    }
  })

  /** Colour of the ribbon: grey while waiting, green once latched, orange, red. */
  const ribbon = $derived.by(() => {
    if (snapshot === null) return 'var(--waiting)'
    if (snapshot.state === 'faulted' || snapshot.state === 'scale_lost') return 'var(--fault)'
    if (snapshot.state === 'rejected') return 'var(--warning)'
    if (snapshot.state === 'succeeded') return 'var(--ready)'
    return snapshot.weight.latched ? 'var(--ready)' : 'var(--waiting)'
  })

  /** The weight is shown when there is one, it has not expired and the link is alive. */
  const weightVisible = $derived(
    showWeight && snapshot !== null && snapshot.weight.available && !snapshot.weight.expired,
  )

  /**
   * The severity the instruction is drawn with.
   *
   * Derived from the STATE when the server sends no message, for the same reason
   * the instruction itself is: a station that says nothing must still say how
   * serious it is. Without this, `scale_lost` showed a red ribbon over a sentence
   * drawn like a piece of good news.
   */
  const level = $derived.by(() => {
    const message = snapshot?.message
    if (message !== null && message !== undefined) return message.level
    if (snapshot?.state === 'faulted' || snapshot?.state === 'scale_lost') return 'error'
    if (snapshot?.state === 'rejected') return 'warn'
    return 'info'
  })
</script>

<header class="banner" data-state={snapshot?.state ?? 'initializing'}>
  <div class="weight-block" style:border-left-color={ribbon}>
    {#if weightVisible && snapshot}
      <p class="weight">{snapshot.weight.net_text}<span class="unit"> kg</span></p>
      <p class="tare">tare {snapshot.weight.tare_g} g</p>
    {:else}
      <p class="weight unavailable">—<span class="unit"> kg</span></p>
      <p class="tare">Poids indisponible</p>
    {/if}
  </div>

  <p class="instruction" class:warn={level === 'warn'} class:error={level === 'error'}>
    {instruction}
  </p>

  <button type="button" class="tare-key touch-target" class:active={taring} onclick={ontare}>
    <Icon name="tare" size="2rem" />
    <span>TARE</span>
  </button>

  <!--
    The state of the station, drawn the full width of the screen.
    It SLIDES from one colour to the next and never blinks: a 3 Hz flash is a
    photosensitive trigger (§14.2). Full width because this is the one signal a
    volunteer reads from the other side of the shop.
  -->
  <div class="ribbon" style:background={ribbon}></div>
</header>

<style>
  .banner {
    position: relative;
    display: flex;
    /* Its height is a constant of the screen, never a variable of the grid. */
    flex: 0 0 auto;
    align-items: center;
    gap: 2rem;
    height: var(--banner-height);
    padding: 0 1.75rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    box-shadow: var(--shadow-1);
    /* Above the grid, so the shadow falls ON it rather than under it. */
    z-index: 2;
  }

  .weight-block {
    flex: 0 0 auto;
    min-width: 20rem;
    padding: 0.5rem 2rem 0.5rem 1.5rem;
    background: var(--bg);
    border-radius: var(--radius-lg);
    border-left: 0.5rem solid var(--waiting);
    transition: border-color var(--slide) var(--ease);
  }

  .weight {
    margin: 0;
    /*
     * Fluid, but bounded BY THE BANNER and by the reading distance (§14.2).
     *
     * The floor is 6rem and not less: 96 px is what makes the weight readable
     * at 2,5 m, which is the whole reason this figure is the biggest thing on
     * the screen. The ceiling is what keeps the card INSIDE its band — at
     * 6.5vw the block came to 171 px in a 160 px banner and spilled over both
     * edges on a 1920 x 1080 panel, measured in the browser.
     *
     * The arithmetic to preserve: 0.5rem x 2 of padding + this + the tare line
     * (~2rem with its margin) must stay under --banner-height, whose own floor
     * is 10rem.
     */
    font-size: clamp(6rem, 5.5vw, 6.75rem);
    font-weight: 700;
    line-height: 1;
    letter-spacing: -0.02em;
  }

  /* --ink-muted and not --waiting: 7,10:1 against the surface, where --waiting
     reaches 3,63:1 and would fail the rule of §14.2 at 96 px. The weight drops
     to a regular weight with it: an em dash at 96 px / 700 is a black slab that
     reads as a redaction rather than as an absence. */
  .weight.unavailable {
    color: var(--ink-muted);
    font-weight: 400;
  }

  .unit {
    font-size: 2.25rem;
    font-weight: 500;
    letter-spacing: 0;
  }

  .tare {
    margin: 0.5rem 0 0;
    color: var(--ink-muted);
    font-size: 1.125rem;
    letter-spacing: 0.02em;
  }

  .instruction {
    flex: 1 1 auto;
    margin: 0;
    padding-left: 1.25rem;
    border-left: 0.375rem solid transparent;
    border-radius: 0.1875rem;
    font-size: 1.75rem;
    line-height: 1.25;
    text-wrap: balance;
    transition: border-color var(--slide) var(--ease);
  }

  /*
   * A refusal and a fault are carried by the RULE, not by the letters.
   *
   * The palette of §14.2 makes that necessary: --warning on --surface is 3,97:1
   * and --fault 6,54:1, both below the 7:1 the same section demands of any text
   * at 24 px or more. Colouring 28 px letters with them would break the rule that
   * declares them. The ink stays at 17,21:1 and the colour moves to a surface
   * where a contrast ratio is not what matters.
   */
  .instruction.warn {
    border-left-color: var(--warning);
    font-weight: 700;
  }

  .instruction.error {
    border-left-color: var(--fault);
    font-weight: 700;
  }

  .tare-key {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.375rem;
    padding: 0 1.5rem;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
    font-size: 1.125rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    transition:
      border-color var(--slide) var(--ease),
      background-color var(--slide) var(--ease),
      transform var(--tap) var(--ease);
  }

  .tare-key.active {
    border-color: var(--ready);
    background: var(--ready-wash);
  }

  .ribbon {
    position: absolute;
    inset: auto 0 0;
    height: 0.375rem;
    transition: background-color var(--slide) var(--ease);
  }
</style>
