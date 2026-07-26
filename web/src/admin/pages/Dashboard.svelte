<script lang="ts">
  import Inventory from '../components/Inventory.svelte'
  import Light from '../components/Light.svelte'
  import Panel from '../components/Panel.svelte'
  import type { HealthDTO } from '../lib/dto'
  import { frenchDate, frenchDateTime, frenchDuration, frenchInteger, frenchTime } from '../lib/format'
  import { lightsOf } from '../lib/lights'

  /**
   * Le tableau de bord de §14.4, page ouverte par défaut et SANS mot de passe (ADR-018).
   *
   * Tout ce qui est ici répond à une question qu'un bénévole pose vraiment : est-ce que ça
   * marche (six feux), est-ce que la balance suit (cadence observée), est-ce que le
   * catalogue arrive (source, chemin surveillé, dernier essai, inventaire), est-ce que
   * quelqu'un a décidé quelque chose (décisions locales), est-ce que le poste revient seul
   * après une coupure (bloquant-7), et qu'est-ce qui vient de se passer (dix événements).
   *
   * Il n'y a pas de septième feu : « redémarrage sans intervention » est une LIGNE, parce
   * que les six feux restent ceux des périphériques et des ressources (§14.4).
   */
  interface Props {
    health: HealthDTO
    /**
     * Ce qui se passe quand un bénévole touche « voir les 16 lignes ».
     *
     * Cela mène à la page Catalogue, qui porte le rapport de correction d'Odoo — donc
     * derrière le mot de passe. Le tableau de bord annonce le chiffre à qui n'a pas de mot
     * de passe ; les lignes à corriger, elles, sont un travail d'expert (§14.4).
     */
    onshowrows?: () => void
  }

  const { health, onshowrows }: Props = $props()

  const lights = $derived(lightsOf(health))
  const scale = $derived(health.state.scale)
  const restart = $derived(health.unattended_restart)
  const decisions = $derived(health.decisions)
</script>

<div class="dashboard">
  <section class="lights" aria-label="État du poste">
    {#each lights as light (light.id)}
      <Light {light} />
    {/each}
  </section>

  <Panel title="Cadence de la balance">
    <p class="fact" data-cadence>
      {#if !health.scale_present}
        Ce poste est déclaré sans balance : le poids est saisi à la main.
      {:else}
        Une mesure toutes les <strong>{frenchDuration(scale.median_ms)}</strong>, médiane
        observée sur les dernières trames reçues
        {#if scale.observations_count > 0}
          ({frenchInteger(scale.observations_count)} intervalles mesurés){/if}.
        {#if scale.provisional}
          La cadence déclarée sert encore de valeur d’attente : moins de huit intervalles
          ont été observés.
        {/if}
        {#if scale.too_slow}
          À cette cadence, un poids serait périmé avant l’arrivée de la mesure suivante.
        {/if}
      {/if}
    </p>
  </Panel>

  <Panel title="Catalogue">
    <p class="fact" data-source>
      {#if health.catalog_source === null}
        Aucune source de catalogue n’est publiée par ce poste.
      {:else}
        Source : <strong>{health.catalog_source.label}</strong>
      {/if}
    </p>
    {#if health.catalog === null}
      <p class="fact">
        Aucun import enregistré sur ce poste : le catalogue n’est jamais arrivé, ou le
        journal ne le porte pas.
      </p>
    {:else}
      <p class="fact" data-attempt>
        Dernier essai : {frenchDateTime(health.catalog.occurred_at)} —
        {resultWord(health.catalog.result)}{health.catalog.file_name === ''
          ? ''
          : ' (' + health.catalog.file_name + ')'}{health.catalog.reason === ''
          ? ''
          : ' : ' + health.catalog.reason}
      </p>
      <Inventory
        record={health.catalog}
        motives={health.catalog_motives}
        onshowrows={onshowrows}
      />
    {/if}
  </Panel>

  <Panel
    title="Décisions locales en vigueur"
    note="Ce qu’un humain a décidé de ce catalogue, avec son motif et sa date (§10.6)."
  >
    {#if decisions.length === 0}
      <p class="fact">Aucune décision locale : le catalogue est proposé tel qu’il arrive.</p>
    {:else}
      <ul class="decisions">
        {#each decisions as decision (decision.product_id)}
          <li>
            <span class="what">
              {decision.offered ? 'Dérogation de poids' : 'Produit retiré'} — {decision.product_id}
            </span>
            {#if decision.min_weight_g !== null}
              <span class="detail">peut peser à partir de {frenchInteger(decision.min_weight_g)} g</span>
            {/if}
            <span class="detail">{decision.reason}</span>
            <span class="detail">
              {frenchDate(decision.decided_at)}, {decision.decided_by}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel title="Redémarrage sans intervention">
    <p class="fact restart" data-restart data-configured={String(restart?.configured ?? false)}>
      {#if restart === null}
        <strong>INCONNU</strong> — ce poste ne publie pas l’état du redémarrage automatique.
      {:else if restart.configured}
        <strong>OK</strong> — après une coupure de courant, ce poste revient seul sur
        l’écran client. {restart.detail}
      {:else}
        <strong>NON CONFIGURÉ</strong> — {restart.detail}
      {/if}
    </p>
    {#if restart !== null && !restart.configured && restart.remedy !== ''}
      <p class="fact remedy">{restart.remedy}</p>
    {/if}
  </Panel>

  <Panel title="Dix derniers événements">
    {#if health.events.length === 0}
      <p class="fact">Rien à signaler depuis le démarrage du poste.</p>
    {:else}
      <ul class="events">
        {#each health.events as event (event.id)}
          <li data-level={event.level}>
            <span class="time">{frenchTime(event.occurred_at)}</span>
            <span class="source">{event.source}</span>
            {#if event.code !== ''}<span class="code">{event.code}</span>{/if}
            <span class="message">{event.message}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel title="Ce poste">
    <dl class="identity">
      <dt>Numéro de poste</dt>
      <dd>{health.station}</dd>
      <dt>Nom</dt>
      <dd>{health.station_name === '' ? 'non renseigné' : health.station_name}</dd>
      <dt>Coopérative</dt>
      <dd>{health.coop}</dd>
      <dt>Version</dt>
      <dd data-version>{health.version}</dd>
      <dt>Empreinte de configuration</dt>
      <dd data-fingerprint>{health.config_fingerprint}</dd>
    </dl>
  </Panel>
</div>

<script lang="ts" module>
  /**
   * Le mot français d'un résultat d'import.
   *
   * « inchangé » est un résultat NOMINAL et non un échec : le producteur peut déposer
   * chaque nuit un export identique à l'octet, et une version antérieure de la conception
   * en faisait une violation de contrainte suivie d'un bannissement permanent (ADR-015).
   */
  function resultWord(result: string): string {
    switch (result) {
      case 'applied':
        return 'appliqué'
      case 'unchanged':
        return 'inchangé, déjà appliqué'
      case 'rejected':
        return 'refusé'
      case 'failed':
        return 'échec'
      default:
        return result
    }
  }
</script>

<style>
  .dashboard {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .lights {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
    gap: var(--touch-gap);
  }

  .fact {
    margin: 0 0 0.5rem;
    font-size: 1.125rem;
  }

  .remedy {
    color: var(--ink-muted);
  }

  .restart strong {
    letter-spacing: 0.03em;
  }

  .decisions,
  .events {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
  }

  .decisions li,
  .events li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .what,
  .message {
    font-weight: 700;
  }

  .detail,
  .time,
  .source,
  .code {
    color: var(--ink-muted);
  }

  /* Un événement d'erreur porte un liseré et jamais une encre rouge (§14.2). */
  .events li[data-level='error'] {
    border-left: 0.25rem solid var(--fault);
    padding-left: 0.5rem;
  }

  .events li[data-level='warn'] {
    border-left: 0.25rem solid var(--warning);
    padding-left: 0.5rem;
  }

  .identity {
    margin: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
    font-size: 1.125rem;
  }

  .identity dt {
    color: var(--ink-muted);
  }

  .identity dd {
    margin: 0;
    font-weight: 700;
  }
</style>
