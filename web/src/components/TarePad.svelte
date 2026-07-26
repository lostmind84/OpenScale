<script lang="ts">
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
  <p class="value">{grams === '' ? '0' : grams} g</p>
  <div class="keys">
    {#each DIGITS as digit (digit)}
      <button type="button" class="key touch-target" onclick={() => ondigit(digit)}>
        {digit}
      </button>
    {/each}
    <button type="button" class="key touch-target" onclick={onclear}>Effacer</button>
    <button type="button" class="key touch-target" onclick={oncancel}>Annuler</button>
    <button type="button" class="key touch-target confirm" onclick={onconfirm}>Valider</button>
  </div>
</section>

<style>
  .pad {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: var(--touch-gap);
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  .value {
    flex: 0 0 auto;
    min-width: 10rem;
    margin: 0;
    font-size: 2.5rem;
    font-weight: 700;
  }

  .keys {
    display: flex;
    flex-wrap: wrap;
    gap: var(--touch-gap);
  }

  .key {
    padding: 0 1rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
    font-size: 1.75rem;
  }

  .key.confirm {
    border-color: var(--ready);
    font-weight: 700;
  }
</style>
