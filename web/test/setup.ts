/**
 * The one thing jsdom is missing to run this screen.
 *
 * `bind:clientWidth` compiles to a `ResizeObserver`, which jsdom does not
 * implement. The stub observes nothing and reports nothing, which is exactly
 * right here: jsdom does no layout, so every width is zero anyway and the grid
 * falls back to the nominal body — the same path a browser without a canvas takes.
 * Nothing that is asserted depends on a measured width.
 */
class NoLayoutResizeObserver implements ResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

globalThis.ResizeObserver ??= NoLayoutResizeObserver

// jsdom has no canvas, and logs a page of « Not implemented » if asked for one.
// Returning null is the answer canvasMeasurer already handles: no measurement, so
// every name renders at its nominal body.
HTMLCanvasElement.prototype.getContext = (() => null) as HTMLCanvasElement['getContext']
