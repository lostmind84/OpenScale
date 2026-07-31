<script lang="ts">
  import type { LabelDTO } from '../lib/dto'
  import { lastLabelSummary } from '../lib/format'
  import Icon from './Icon.svelte'

  /**
   * The bottom bar, and it is PERMANENT — three blocks, left to right.
   *
   * What the station KNOWS about itself (catalog date, weighable count, running
   * version) · what it just DID (the last label, and the key that prints it
   * again) · whether it still CAN (scale and printer), then the way into the
   * settings. The order is the reading order of a volunteer walking up to a
   * station that is behaving oddly, and each block is separated by a hairline
   * rather than by guesswork about spacing.
   *
   * It is not inside a success overlay, and the reason is physical: the customer
   * steps away from the plate precisely to look for their label, so an overlay
   * that closes « when the bag leaves the scale » would close at the exact moment
   * it is needed. Such an overlay does not exist here (§14.3, ADR-023).
   *
   * One reprint only, and the label goes out carrying the word RÉIMPRESSION.
   */
  interface Props {
    /** What came out last, or null when nothing has been printed yet. */
    label: LabelDTO | null
    /** The window of `reprint_window_s` is still open, and nothing was reprinted. */
    available: boolean
    /** When the catalog was imported, already formatted, or empty (§14.3, ADR-053). */
    catalogAt?: string
    /** How many weighable products the grid is drawing from. */
    productCount?: number
    /** The running application version, as the catalog payload states it. */
    appVersion?: string
    /** Whether the scale and printer are both answering right now. */
    healthy: boolean
    onreprint: () => void
    onadmin: () => void
  }

  const {
    label,
    available,
    catalogAt = '',
    productCount = 0,
    appVersion = '',
    healthy,
    onreprint,
    onadmin,
  }: Props = $props()

  /**
   * « 331 produits pesables · application 2.4.0 », and each half stands alone.
   *
   * A station whose catalog has not arrived has no count to state, and one built
   * from a binary without a version stamp has no version: neither absence is
   * allowed to leave a dangling separator on screen.
   */
  const inventory = $derived(
    [
      productCount > 0 ? `${productCount} produits pesables` : '',
      appVersion !== '' ? `application ${appVersion}` : '',
    ]
      .filter((part) => part !== '')
      .join(' · '),
  )
</script>

<div class="bar" class:live={label !== null}>
  <!--
    Ce que le poste sait de lui-même. « Ces prix datent de quand ? » est la
    question qu'un bénévole pose devant une grille ; une date qui cesse
    d'avancer est aussi la façon dont un poste dit qu'il ne reçoit plus rien.
    C'est la date du dernier import qui a appliqué des modifications, et
    aucun redémarrage ne la déplace (ADR-053).
  -->
  <div class="block station">
    {#if catalogAt !== ''}
      <span class="line">Catalogue du <strong>{catalogAt}</strong></span>
    {:else}
      <span class="line">Catalogue en attente</span>
    {/if}
    {#if inventory !== ''}
      <span class="line quiet">{inventory}</span>
    {/if}
  </div>

  <div class="spacer"></div>

  <!-- Ce que le poste vient de faire, et la touche qui le refait. -->
  <div class="block label">
    <span class="caption">Dernière étiquette</span>
    {#if label === null}
      <span class="line quiet">aucune pour le moment</span>
    {:else}
      <span class="line strong">{lastLabelSummary(label)}</span>
    {/if}
  </div>

  {#if label !== null}
    <button type="button" class="reprint touch-target" disabled={!available} onclick={onreprint}>
      Réimprimer
    </button>
  {/if}

  <!-- Ce que le poste peut encore faire : une pastille et sa phrase, parce
       qu'une couleur seule ne se lit pas au téléphone (§14.2). -->
  <div class="block health">
    <span
      class="dot"
      class:fault={!healthy}
      role="status"
      aria-label={healthy ? 'Matériel disponible' : 'Matériel indisponible'}
    ></span>
    <span class="line quiet">
      {healthy ? 'Balance et imprimante disponibles' : 'Matériel indisponible'}
    </span>
  </div>

  <!--
    Icône seule, sans texte — décision du 28/07/2026, qui revient sur la
    partie « touche nommée » d'ADR-032. Le bouton reste visible et bordé dans
    une barre permanente, ce qui reste l'essentiel de ce qu'ADR-032 corrigeait :
    ce n'est plus un coin muet.
  -->
  <button type="button" class="admin touch-target" aria-label="Réglages" onclick={onadmin}>
    <Icon name="settings" size="1.75rem" />
  </button>
</div>

<style>
  /*
   * No stacking context here, and that is load-bearing: the settings key escapes
   * the fault overlay by climbing into the ROOT context, which only works while
   * nothing between them opens a context of its own. A `z-index`, an `opacity`
   * below 1, a `transform`, a `filter` or an `isolation` on this bar would trap
   * the key underneath the overlay again — with every declared number still
   * reading exactly right.
   */
  .bar {
    display: flex;
    /* PERMANENT means it keeps its height whatever the grid above weighs. */
    flex: 0 0 auto;
    align-items: stretch;
    height: var(--status-height);
    padding: 0 1.0625rem;
    background: var(--bg);
    border-top: 1px solid var(--border);
    transition: background-color var(--slide) var(--ease);
  }

  /* A label has just come out: the strip lifts onto the surface colour, which is
     the whole acknowledgement §14.3 asks for — the real one is the paper. */
  .bar.live {
    background: var(--surface);
  }

  /*
   * Les trois blocs, séparés par un filet et non par du vide.
   *
   * Un filet dit « ces deux choses ne parlent pas de la même » ; un écart, lui,
   * se lit différemment selon la largeur de l'écran, et la barre en change.
   */
  .block {
    display: flex;
    flex-direction: column;
    justify-content: center;
    flex: 0 0 auto;
    min-width: 0;
    gap: 0.125rem;
    line-height: 1.25;
    white-space: nowrap;
  }

  .block.label,
  .block.health {
    padding-left: 1.75rem;
    border-left: 1px solid var(--border);
  }

  .block.label {
    /* Le seul bloc qui peut être long : il rétrécit, les deux autres jamais. */
    flex: 0 1 auto;
    padding-right: 1.75rem;
  }

  .block.health {
    flex-direction: row;
    align-items: center;
    gap: 0.75rem;
  }

  .spacer {
    flex: 1 1 auto;
    min-width: 1.5rem;
  }

  .line {
    font-size: 1.125rem;
    color: var(--ink-muted);
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .line.quiet {
    color: var(--ink-muted);
  }

  .line strong,
  .line.strong {
    color: var(--ink);
    font-weight: 600;
  }

  .caption {
    font-size: 1rem;
    letter-spacing: var(--tracking-caps, 0.03em);
    text-transform: uppercase;
    color: var(--ink-muted);
  }

  .reprint {
    flex: 0 0 auto;
    align-self: center;
    margin-right: 1.75rem;
    padding: 0 2rem;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow-1);
    font-size: 1.375rem;
    font-weight: 600;
    transition:
      border-color var(--slide) var(--ease),
      opacity var(--slide) var(--ease),
      transform var(--tap) var(--ease);
  }

  .reprint:disabled {
    opacity: 0.35;
    box-shadow: none;
    cursor: default;
  }

  .dot {
    flex: 0 0 auto;
    width: var(--dot-size-health, 1.25rem);
    height: var(--dot-size-health, 1.25rem);
    border-radius: 50%;
    background: var(--ready);
    transition: background-color var(--slide) var(--ease);
  }

  .dot.fault {
    background: var(--fault);
  }

  /*
   * ABOVE the fault overlay — and the only thing on this screen that is.
   *
   * The overlay of `FullScreen` sits at 10, opaque and edge to edge, so a station
   * that cannot serve buries its own bottom bar, this key included. ADR-032 makes
   * that key the one way into the administration from the screen: no URL to type,
   * no way out of the kiosk. A station fresh out of the installer starts
   * OutOfService, so left underneath it is unreachable in the exact state that has
   * to be repaired. 20 clears the overlay and stays well under the administration
   * itself (90).
   *
   * Everything else in the bar stays buried, deliberately: the overlay is there so
   * that nobody weighs on a station that cannot serve.
   */
  .admin {
    position: relative;
    z-index: 20;
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    align-self: center;
    width: var(--touch-min);
    height: var(--touch-min);
    margin-left: 1.75rem;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--ink-muted);
  }
</style>
