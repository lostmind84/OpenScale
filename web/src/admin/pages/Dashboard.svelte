<script lang="ts">
  import Inventory from '../components/Inventory.svelte'
  import Light from '../components/Light.svelte'
  import Panel from '../components/Panel.svelte'
  import type { HealthDTO } from '../lib/dto'
  import { logSourceLabelOf } from '../lib/fields'
  import { frenchDate, frenchDateTime, frenchDuration, frenchInteger, frenchTime } from '../lib/format'
  import { lightsOf } from '../lib/lights'
  import { preferences } from '../lib/preferences.svelte'

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
    /** Ce qui se passe quand un bénévole touche la pastille de version. */
    onshowupdate?: () => void
  }

  const { health, onshowrows, onshowupdate }: Props = $props()

  /**
   * La version publiée, quand il en existe une plus récente que celle qui tourne.
   *
   * Elle vient du MÊME appel que tout le reste de cette page. Un second appel, même
   * libre, aurait élargi pour une courtoisie ce que fait un écran ouvert sans mot de
   * passe — et un test tient cette page à une seule route.
   *
   * Le service rend une chaîne vide quand il n'y a rien à dire, y compris quand il n'a
   * pas pu lire : la pastille est une courtoisie, son absence n'apprend rien de faux,
   * alors qu'une erreur sur le tableau de bord enverrait chercher une panne inexistante.
   */
  const newVersion = $derived(health.new_version)

  const lights = $derived(lightsOf(health))
  const scale = $derived(health.state.scale)
  const restart = $derived(health.unattended_restart)
  const decisions = $derived(health.decisions)

  /**
   * Combien de décisions locales ce tableau montre avant de renvoyer au Catalogue.
   *
   * La liste n'avait de plafond ni ici ni au service : §10.6 n'en borne pas le nombre, et
   * un magasin qui retire trente produits repoussait la ligne du redémarrage sans
   * intervention — que bloquant-7 veut visible — trois écrans plus bas.
   */
  const SHOWN_DECISIONS = 5

  const shownDecisions = $derived(decisions.slice(0, SHOWN_DECISIONS))
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
      {:else if !scale.connected}
        La balance ne répond pas : aucune cadence n’est mesurable tant qu’aucune trame
        n’arrive.
      {:else if scale.observations_count === 0}
        Aucun intervalle n’a encore été mesuré : la cadence apparaîtra dès les premières
        trames.
      {:else}
        Une mesure toutes les <strong>{frenchDuration(scale.median_ms)}</strong>, médiane
        observée sur {frenchInteger(scale.observations_count)} intervalles.
        {#if scale.provisional}
          C’est encore une valeur d’attente : moins de huit intervalles ont été observés.
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
    note="Ce qu’un humain a décidé de ce catalogue, avec son motif et sa date."
  >
    {#if decisions.length === 0}
      <p class="fact">Aucune décision locale : le catalogue est proposé tel qu’il arrive.</p>
    {:else}
      {#if decisions.length > SHOWN_DECISIONS}
        <p class="fact count">
          {frenchInteger(decisions.length)} décisions en vigueur — les
          {SHOWN_DECISIONS} plus récentes ci-dessous. La liste entière est sur la page
          Catalogue.
        </p>
      {/if}
      <ul class="decisions">
        {#each shownDecisions as decision (decision.product_id)}
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

  <!--
    §14.4 : « NON CONFIGURÉ s'affiche en orange ». Le mot est écrit depuis toujours ;
    l'orange, lui, n'existait pas — `data-configured` était posé sans qu'aucune règle CSS
    ne le lise, si bien que le seul état qui doive attirer l'œil ressemblait aux autres.
    Et `known` — « faux quand la question n'a pas pu être posée » — n'était jamais lu :
    un poste qui ne SAIT PAS était accusé de n'être pas configuré.
  -->
  <Panel title="Redémarrage sans intervention">
    <p class="fact restart" data-restart data-verdict={restartVerdict(restart)}>
      {#if restart === null || !restart.known}
        <strong>INCONNU</strong> — la question n’a pas pu être posée à ce système. Ce
        n’est pas « non configuré » : personne ne sait encore.
      {:else if restart.configured}
        <strong>OK</strong> — après une coupure de courant, ce poste revient seul sur
        l’écran client. {restart.detail}
      {:else}
        <strong>NON CONFIGURÉ</strong> — {restart.detail}
      {/if}
    </p>
    {#if restart !== null && restart.known && !restart.configured && restart.remedy !== ''}
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
            <!--
              The origin is translated HERE TOO, and the reason is the one written just
              below about the code: the Journal reads the same technical log and says
              « catalogue » where this page said « catalog ». One log, two screens, one
              vocabulary — and this is the page that opens without a password.
            -->
            <span class="source">{logSourceLabelOf(event.source)}</span>
            <!--
              The technical code goes behind the switch, as on the Journal page: this is
              the SAME technical log, shown here in ten lines and there in fifty, and a
              screen that hid `ERR-CAT-05` on one side to show it on the other would hide
              nothing at all. The French message already says what happened.
            -->
            {#if event.code !== '' && preferences.showTechnicalNames}
              <span class="code">{event.code}</span>
            {/if}
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
    <!--
      DU TEXTE, ET NON UN FEU. Une version disponible n'est pas une panne : les six feux
      restent ceux des périphériques et des ressources, et un poste sain qui s'allumerait
      en orange parce qu'un correctif est sorti apprendrait aux bénévoles à ignorer
      l'orange.
    -->
    {#if newVersion !== ''}
      <p class="fact" data-update-available={newVersion}>
        Version {newVersion} disponible.
        {#if onshowupdate !== undefined}
          <button type="button" class="link" onclick={onshowupdate}>Voir la mise à jour</button>
        {/if}
      </p>
    {/if}
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
  /**
   * Le verdict du redémarrage, en un mot que le CSS sait colorer.
   *
   * Trois valeurs et non deux : « je ne sais pas » n'est pas « non configuré », et §14.4
   * ne demande l'orange que pour la seconde.
   */
  function restartVerdict(restart: { configured: boolean; known: boolean } | null): string {
    if (restart === null || !restart.known) return 'unknown'
    return restart.configured ? 'ok' : 'missing'
  }

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

  /* La pastille de version est un LIEN et non un acte : elle ne fait rien au poste, elle
     ouvre une page. Elle n'emprunte donc ni la couleur ni la cible de 44 px des boutons
     d'action, qui disent la nature d'un acte (ADR-037). */
  /* Le SOULIGNÉ et non la couleur. §14.2 n'inventorie que --ink et --ink-muted comme
     texte, et les deux fonds pleins n'ont été mesurés que comme fonds : peindre du texte
     avec --action poserait un contraste que personne n'a vérifié. */
  .link {
    padding: 0;
    font: inherit;
    color: inherit;
    text-decoration: underline;
    background: none;
    border: none;
    cursor: pointer;
  }

  /* `1fr` sur les rangées : les six feux ont la MÊME hauteur, comme les tuiles de la
     grille client (ADR-030). Ragés, ils se lisaient comme six objets différents. */
  .lights {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
    grid-auto-rows: 1fr;
    gap: var(--touch-gap);
  }

  .fact {
    margin: 0 0 0.5rem;
    font-size: 1.125rem;
    /* Un chemin de fichier Windows n'a pas d'espace où se couper : sans ceci il pousse
       la colonne de lecture et fait déborder la page. */
    overflow-wrap: break-word;
  }

  .remedy,
  .count {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  .restart strong {
    letter-spacing: 0.03em;
  }

  /* §14.4 : « NON CONFIGURÉ s'affiche en orange ». Le lavis porte la couleur, jamais les
     lettres — --warning plafonne à 3,97:1 sur --surface (§14.2). */
  .restart[data-verdict='missing'] {
    margin-bottom: 0.25rem;
    padding: 0.5rem 0.75rem;
    border-left: 0.25rem solid var(--warning);
    background: var(--warning-wash);
  }

  .restart[data-verdict='unknown'] {
    margin-bottom: 0.25rem;
    padding: 0.5rem 0.75rem;
    border-left: 0.25rem solid var(--waiting);
    background: var(--waiting-wash);
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
