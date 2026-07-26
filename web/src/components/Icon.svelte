<script lang="ts">
  /**
   * The six drawings of the client screen, as outlines and not as emoji.
   *
   * An emoji is a font the station does not ship. `🫙` — the jar the TARE key used
   * to carry — was added to Unicode in 2021 and is absent from the emoji font of a
   * Windows 10 machine that has not been updated: the key then draws a tofu box,
   * on the one screen a customer has to understand without being told. These are
   * drawn by the browser from two hundred bytes of path, at any size, in the
   * colour of the text around them.
   */
  interface Props {
    name: 'search' | 'tare' | 'printer' | 'alert' | 'backspace' | 'check'
    /** Edge of the square the drawing fills, in CSS units. */
    size?: string
  }

  const { name, size = '1.5rem' }: Props = $props()

  /** One path per drawing, on the 24 x 24 grid every outline here is drawn at. */
  const PATHS: Record<Props['name'], string> = {
    search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14M16.5 16.5 21 21',
    tare: 'M8 4h8M9 4v3.2a3 3 0 0 1-.5 1.7l-.8 1.2a4 4 0 0 0-.7 2.2V18a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2v-5.7a4 4 0 0 0-.7-2.2l-.8-1.2a3 3 0 0 1-.5-1.7V4M7 14h10',
    printer: 'M7 9V4h10v5M7 20h10v-6H7zM7 17H5a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2h-2',
    alert: 'M12 3.5 21.5 20h-19zM12 10v4M12 17.2v.1',
    backspace: 'M9 5h11a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-7-7zM17 9l-6 6M11 9l6 6',
    check: 'M4 12.5 9.5 18 20 7',
  }
</script>

<svg
  class="icon"
  style:width={size}
  style:height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  <path d={PATHS[name]} />
</svg>

<style>
  .icon {
    display: block;
    flex: 0 0 auto;
  }
</style>
