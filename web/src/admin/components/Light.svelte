<script lang="ts">
  import type { Light } from '../lib/lights'

  /**
   * Un des six feux du tableau de bord (§14.4).
   *
   * La couleur est un LISERÉ et une pastille, jamais de l'encre : sur `--surface`,
   * `--warning` plafonne à 3,97:1 et `--fault` à 6,54:1, donc les employer comme couleur
   * de lettres violerait la règle AAA que §14.2 énonce dans la même phrase qu'eux.
   *
   * La consigne est affichée en toutes lettres et non cachée derrière un dépliant : un
   * bénévole debout devant un poste en panne ne cherche pas où cliquer pour savoir quoi
   * faire.
   */
  interface Props {
    light: Light
  }

  const { light }: Props = $props()
</script>

<article class="light" data-light={light.id} data-level={light.level}>
  <header>
    <span class="dot" data-level={light.level} aria-hidden="true"></span>
    <h3>{light.label}</h3>
    <span class="verdict">{verdictOf(light.level)}</span>
  </header>
  <p class="value">{light.value}</p>
  {#if light.remedy !== ''}
    <p class="remedy">{light.remedy}</p>
  {/if}
</article>

<script lang="ts" module>
  /** Le mot que porte un feu, pour qui ne distingue pas les couleurs. */
  function verdictOf(level: string): string {
    switch (level) {
      case 'ok':
        return 'OK'
      case 'warn':
        return 'À SURVEILLER'
      case 'fault':
        return 'EN PANNE'
      case 'off':
        return 'SANS OBJET'
      default:
        return 'INCONNU'
    }
  }
</script>

<style>
  .light {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0.875rem 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 0.5rem solid var(--waiting);
    border-radius: var(--radius);
  }

  .light[data-level='ok'] {
    border-left-color: var(--ready);
  }

  .light[data-level='warn'] {
    border-left-color: var(--warning);
  }

  .light[data-level='fault'] {
    border-left-color: var(--fault);
  }

  header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  h3 {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 700;
  }

  .dot {
    width: 1rem;
    height: 1rem;
    border-radius: 50%;
    background: var(--waiting);
    flex: none;
  }

  .dot[data-level='ok'] {
    background: var(--ready);
  }

  .dot[data-level='warn'] {
    background: var(--warning);
  }

  .dot[data-level='fault'] {
    background: var(--fault);
  }

  /* « Je ne peux pas le savoir d'ici » n'est pas « en panne » : l'anneau creux le dit
     sans couleur, comme les cinq verdicts de `diag.Status`. */
  .dot[data-level='unknown'],
  .dot[data-level='off'] {
    background: transparent;
    box-shadow: inset 0 0 0 3px var(--waiting);
  }

  .verdict {
    margin-left: auto;
    font-size: 1rem;
    font-weight: 700;
    color: var(--ink-muted);
    letter-spacing: 0.03em;
  }

  .value {
    margin: 0;
    font-size: 1.125rem;
  }

  .remedy {
    margin: 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }
</style>
