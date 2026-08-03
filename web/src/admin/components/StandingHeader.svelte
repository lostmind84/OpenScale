<script lang="ts">
  import type { Standing } from '../lib/standing'

  /**
   * The heading of a hardware panel: its name, and what the station OBSERVES of it.
   *
   * The colour carries the meaning and the letters repeat it — §14.2 forbids entrusting a
   * piece of information to a hue alone, and neither `--warning` nor `--fault` reaches the
   * 7:1 a text is held to: they are a dot and a wash here, never ink.
   */
  interface Props {
    title: string
    /** The name this panel answers to in the DOM, for the tests and for nothing else. */
    name: string
    /** What the station observes of it: a level, a French word, and a sentence. */
    standing: Standing
  }

  const { title, name, standing }: Props = $props()
</script>

<header class="head">
  <h2>{title}</h2>
  <span class="standing" data-standing={name} data-level={standing.level}>
    <span class="dot"></span>{standing.word}
  </span>
</header>

<style>
  .head {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
    justify-content: space-between;
  }

  h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 700;
  }

  .standing {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    height: 2rem;
    padding: 0 0.75rem;
    border-radius: var(--radius-pill);
    background: var(--waiting-wash);
    font-size: 1rem;
    font-weight: 700;
  }

  .standing[data-level='ok'] {
    background: var(--ready-wash);
  }

  .standing[data-level='warn'] {
    background: var(--warning-wash);
  }

  .standing[data-level='fault'] {
    background: var(--fault-wash);
  }

  .dot {
    width: 0.625rem;
    height: 0.625rem;
    border-radius: var(--radius-pill);
    background: var(--waiting);
  }

  .standing[data-level='ok'] .dot {
    background: var(--ready);
  }

  .standing[data-level='warn'] .dot {
    background: var(--warning);
  }

  .standing[data-level='fault'] .dot {
    background: var(--fault);
  }
</style>
