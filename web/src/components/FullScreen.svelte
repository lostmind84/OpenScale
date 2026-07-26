<script lang="ts">
  /**
   * The only thing that takes the whole screen — and there is nothing else.
   *
   * The criterion is sharp: the screen is taken over ONLY when the station cannot
   * serve. `Faulted`, `OutOfService`, and the invalid configuration screen
   * `ERR-CFG-01`. Everything else — success, refusal, tare, search, units — lives
   * in the banner or on the tile (§14.3, ADR-023).
   */
  interface Props {
    title: string
    /** French sentence a volunteer or a customer reads. */
    detail: string
    /** `ERR-xxx-nn`, shown small so it can be read out over the telephone. */
    code: string
    /** Present only when there is something the customer can actually do. */
    action?: { label: string; run: () => void } | null
  }

  const { title, detail, code, action = null }: Props = $props()
</script>

<div class="screen" role="alertdialog" aria-labelledby="fullscreen-title">
  <h1 id="fullscreen-title">{title}</h1>
  <p class="detail">{detail}</p>
  {#if action !== null}
    <button type="button" class="action touch-target" onclick={action.run}>{action.label}</button>
  {/if}
  {#if code !== ''}
    <p class="code">{code}</p>
  {/if}
</div>

<style>
  .screen {
    position: fixed;
    inset: 0;
    z-index: 10;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1.5rem;
    padding: 3rem;
    background: var(--bg);
    text-align: center;
  }

  /* The title is INK, and the fault colour is the rule above it: --fault on --bg
     is 6,05:1, below the 7:1 §14.2 demands of text at 24 px and more. */
  h1 {
    margin: 0;
    padding-top: 1rem;
    border-top: 0.5rem solid var(--fault);
    font-size: 4rem;
  }

  .detail {
    margin: 0;
    max-width: 50rem;
    font-size: 1.75rem;
  }

  .action {
    padding: 0 2rem;
    border: 2px solid var(--ink);
    border-radius: var(--radius);
    font-size: 1.75rem;
    font-weight: 700;
  }

  .code {
    margin: 0;
    /* 18 px, for the telephone: it is read out, not read (§14.3). */
    font-size: 1.125rem;
    color: var(--ink-muted);
  }
</style>
