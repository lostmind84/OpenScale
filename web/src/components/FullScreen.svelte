<script lang="ts">
  import Icon from './Icon.svelte'

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
  <div class="card">
    <span class="glyph" aria-hidden="true">
      <Icon name="alert" size="3rem" />
    </span>

    <h1 id="fullscreen-title">{title}</h1>
    <p class="detail">{detail}</p>

    {#if action !== null}
      <button type="button" class="action touch-target" onclick={action.run}>{action.label}</button>
    {/if}

    {#if code !== ''}
      <p class="code">{code}</p>
    {/if}
  </div>
</div>

<style>
  .screen {
    position: fixed;
    inset: 0;
    z-index: 10;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 3rem;
    background: var(--bg);
  }

  .card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1.5rem;
    max-width: 54rem;
    padding: 3.5rem 4rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-2);
    text-align: center;
  }

  /* The fault colour is a disc and a ring, never the letters: --fault on
     --surface is 6,54:1, under the 7:1 §14.2 demands of text at 24 px and more. */
  .glyph {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 6rem;
    height: 6rem;
    border-radius: 50%;
    background: var(--fault-wash);
    box-shadow: 0 0 0 2px var(--fault) inset;
    color: var(--ink);
  }

  h1 {
    margin: 0;
    font-size: 3.5rem;
    line-height: 1.05;
    letter-spacing: -0.02em;
  }

  .detail {
    margin: 0;
    max-width: 40rem;
    font-size: 1.75rem;
    line-height: 1.35;
    text-wrap: balance;
  }

  .action {
    margin-top: 0.5rem;
    padding: 0 2.5rem;
    border: 2px solid var(--ink);
    border-radius: var(--radius);
    font-size: 1.75rem;
    font-weight: 700;
  }

  .code {
    margin: 0;
    padding-top: 0.5rem;
    /* 18 px, for the telephone: it is read out, not read (§14.3). */
    font-size: 1.125rem;
    letter-spacing: 0.12em;
    color: var(--ink-muted);
  }
</style>
