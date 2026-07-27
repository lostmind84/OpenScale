<script lang="ts">
  import Icon from './Icon.svelte'

  /**
   * The tare pad, anchored UNDER the banner and not in front of it.
   *
   * No model in the fleet supports `Tare()` over its serial interface (§19), so a
   * figure typed in grams is imposed by the hardware and stays. Its FORM changes:
   * the scale stays visible during the whole entry, because a customer putting
   * their jar down has to see the weight move while they type (§14.3).
   */
  interface Props {
    /** What has been typed so far, in grams. */
    grams: string
    ondigit: (digit: string) => void
    onclear: () => void
    onconfirm: () => void
    oncancel: () => void
  }

  const { grams, ondigit, onclear, onconfirm, oncancel }: Props = $props()

  const DIGITS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '0'] as const
</script>

<section class="pad" aria-label="Tare en grammes">
  <p class="value">
    <span class="label">Tare</span>
    <span class="grams">{grams === '' ? '0' : grams}<span class="unit"> g</span></span>
  </p>

  <div class="digits">
    {#each DIGITS as digit (digit)}
      <button type="button" class="key touch-target" onclick={() => ondigit(digit)}>
        {digit}
      </button>
    {/each}
  </div>

  <div class="actions">
    <button type="button" class="key touch-target action" onclick={onclear}>Effacer</button>
    <button type="button" class="key touch-target action" onclick={oncancel}>Annuler</button>
    <button type="button" class="key touch-target action confirm" onclick={onconfirm}>
      <Icon name="check" size="1.5rem" />
      Valider
    </button>
  </div>
</section>

<style>
  .pad {
    display: flex;
    flex: 0 0 auto;
    align-items: center;
    gap: 1.25rem;
    padding: 0.75rem 1.75rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    box-shadow: var(--shadow-1);
    z-index: 1;
    animation: drop var(--slide) var(--ease);
  }

  /* It comes down from under the banner, where it belongs, in 170 ms. */
  @keyframes drop {
    from {
      transform: translateY(-1rem);
      opacity: 0;
    }
  }

  .value {
    display: flex;
    flex-direction: column;
    flex: 0 0 auto;
    min-width: 11rem;
    margin: 0;
    padding-right: 1.5rem;
    border-right: 1px solid var(--border-soft);
  }

  .label {
    color: var(--ink-muted);
    font-size: 1rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .grams {
    font-size: 2.5rem;
    font-weight: 700;
    line-height: 1.1;
  }

  .unit {
    font-size: 1.5rem;
    font-weight: 400;
    color: var(--ink-muted);
  }

  .digits,
  .actions {
    display: flex;
    gap: var(--touch-gap);
  }

  .actions {
    margin-left: auto;
  }

  .key {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    width: var(--touch-min);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    box-shadow: var(--shadow-1);
    font-size: 1.75rem;
    font-weight: 600;
  }

  .key:active {
    background: var(--bg);
  }

  .key.action {
    width: auto;
    padding: 0 1.5rem;
    font-size: 1.375rem;
    font-weight: 500;
  }

  /* Green ring and green wash, ink letters: --surface on --ready is 4,28:1, under
     the 4,5:1 a 22 px label owes, so the confirmation is a frame and not a fill. */
  .key.confirm {
    border: 2px solid var(--ready);
    background: var(--ready-wash);
    font-weight: 700;
  }
</style>
