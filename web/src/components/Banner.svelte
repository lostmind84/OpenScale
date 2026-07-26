<script lang="ts">
  import type { MessageDTO, StateDTO } from '../lib/dto'
  
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
    ontare: () => void
  }

  const { snapshot, showWeight, linkBanner, ontare }: Props = $props()

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

  const level = $derived(snapshot?.message?.level ?? 'info')
</script>

<header class="banner" data-state={snapshot?.state ?? 'initializing'}>
  <div class="weight-block">
    {#if weightVisible && snapshot}
      <p class="weight">{snapshot.weight.net_text}<span class="unit"> kg</span></p>
      <p class="tare">tare {snapshot.weight.tare_g} g</p>
    {:else}
      <p class="weight unavailable">—<span class="unit"> kg</span></p>
      <p class="tare">Poids indisponible</p>
    {/if}
    <div class="ribbon" style:background={ribbon}></div>
  </div>

  <p class="instruction" class:warn={level === 'warn'} class:error={level === 'error'}>
    {instruction}
  </p>

  <button type="button" class="tare-key touch-target" onclick={ontare}>
    <span class="tare-icon" aria-hidden="true">🫙</span>
    <span>TARE</span>
  </button>
</header>

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 2rem;
    height: var(--banner-height);
    padding: 0 1.5rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  .weight-block {
    flex: 0 0 auto;
    min-width: 22rem;
  }

  .weight {
    margin: 0;
    /* 96 px / 700 / tabular: readable at 2,5 m (§14.2). */
    font-size: 6rem;
    font-weight: 700;
    line-height: 1;
  }

  /* --ink-muted and not --waiting: 7,10:1 against the surface, where --waiting
     reaches 3,63:1 and would fail the rule of §14.2 at 96 px. */
  .weight.unavailable {
    color: var(--ink-muted);
  }

  .unit {
    font-size: 2.5rem;
    font-weight: 400;
  }

  .tare {
    margin: 0.25rem 0 0;
    color: var(--ink-muted);
    font-size: 1.125rem;
  }

  .ribbon {
    height: 0.5rem;
    margin-top: 0.5rem;
    border-radius: 0.25rem;
    /* It SLIDES, it never blinks: a 3 Hz flash is a photosensitive trigger. */
    transition: background-color 160ms ease-out;
  }

  .instruction {
    flex: 1 1 auto;
    margin: 0;
    padding-left: 0.75rem;
    border-left: 0.375rem solid transparent;
    font-size: 1.75rem;
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
    gap: 0.25rem;
    padding: 0 1.5rem;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    font-size: 1.25rem;
    font-weight: 700;
  }

  .tare-icon {
    font-size: 2rem;
  }
</style>
