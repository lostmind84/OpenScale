<script lang="ts">
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import type { Draft } from '../lib/draft.svelte'
  import type { HealthDTO } from '../lib/dto'
  import { frenchDate, frenchInteger } from '../lib/format'

  /**
   * La page Règles de §14.4 : la grille de tarifs, les deux arrondis, les garde-fous.
   *
   * Deux choses n'y sont PAS, et c'est délibéré. La sévérité d'un garde-fou est en lecture
   * seule : elle dit si le poste refuse ou avertit, et c'est une décision de conception et
   * non un réglage de magasin (§6.4). Et l'écran ne calcule aucun verdict : « à 8 g, ce
   * produit serait refusé » appartient au noyau, et une deuxième implémentation dans le
   * navigateur finirait par dire le contraire de l'étiquette qui sort. Ce qui est montré à
   * la place, c'est ce que les garde-fous ont VRAIMENT dit de la dernière pesée.
   */
  interface Props {
    draft: Draft
    health: HealthDTO
  }

  const { draft, health }: Props = $props()

  const tiers = $derived(tiersOf(draft))
  const diagnostics = $derived(health.state.diagnostics)
  const waivers = $derived(health.decisions.filter((decision) => decision.min_weight_g !== null))

  /** Un tarif de la grille, tel que le document le porte. */
  interface Tier {
    code: string
    label: string
    abbrev: string
    coefNum: number
    coefDen: number
    rank: number
  }

  /**
   * Lit la grille de tarifs du brouillon.
   *
   * Elle est lue depuis le document et non depuis un type : la configuration voyage TELLE
   * QUE LE FICHIER l'écrit (§11.4), et un écran qui exigerait une forme figée refuserait
   * un fichier qu'un poste accepte.
   */
  function tiersOf(source: Draft): Tier[] {
    const value = source.value('pricing.tiers')
    if (!Array.isArray(value)) return []
    return value.map((raw) => {
      const row = (raw ?? {}) as Record<string, unknown>
      return {
        code: String(row.code ?? ''),
        label: String(row.label ?? ''),
        abbrev: String(row.abbrev ?? ''),
        coefNum: Number(row.coef_num ?? 0),
        coefDen: Number(row.coef_den ?? 1),
        rank: Number(row.rank ?? 0),
      }
    })
  }

  /** Les seuils de §6.4, avec le chemin de leur clé et ce qu'ils protègent. */
  const LIMITS: { path: string; label: string; hint: string }[] = [
    {
      path: 'limits.empty_max_g',
      label: 'Plateau considéré vide',
      hint: 'En dessous, le poste considère qu’il n’y a rien sur le plateau.',
    },
    {
      path: 'limits.min_weight_g',
      label: 'Poids minimum',
      hint: 'Une dérogation par produit existe, dans l’onglet Catalogue (§10.6).',
    },
    {
      path: 'limits.max_weight_g',
      label: 'Poids maximum',
      hint: 'Au-delà, c’est la portée de la balance ou une erreur de trame.',
    },
    {
      path: 'limits.max_tare_g',
      label: 'Tare maximum',
      hint: 'Une tare plus lourde que le maximum est une faute de frappe.',
    },
    { path: 'limits.min_units', label: 'Unités minimum', hint: '' },
    { path: 'limits.max_units', label: 'Unités maximum', hint: '' },
    {
      path: 'limits.max_amount_cents',
      label: 'Montant maximum',
      hint: 'Un montant au-delà arrête la pesée : c’est le garde-fou du prix, en centimes.',
    },
  ]
</script>

<div class="pages">
  <Panel
    title="Grille de tarifs"
    note="Le double tarif n’est pas un booléen : c’est la cardinalité de cette grille (§6.3, ADR-009)."
  >
    {#if tiers.length === 0}
      <p class="fact">Aucun tarif déclaré dans la configuration lue.</p>
    {:else}
      <div class="scroll">
        <table>
          <thead>
            <tr>
              <th>Code</th>
              <th>Libellé</th>
              <th>Abrégé</th>
              <th>Numérateur</th>
              <th>Dénominateur</th>
              <th>Ordre</th>
            </tr>
          </thead>
          <tbody>
            {#each tiers as tier, index (tier.code)}
              <tr>
                <td>{tier.code}</td>
                <td>
                  <input
                    value={tier.label}
                    oninput={(event) =>
                      draft.set(`pricing.tiers.${String(index)}.label`, event.currentTarget.value)}
                  />
                </td>
                <td>
                  <input
                    value={tier.abbrev}
                    oninput={(event) =>
                      draft.set(`pricing.tiers.${String(index)}.abbrev`, event.currentTarget.value)}
                  />
                </td>
                <td>
                  <input
                    type="number"
                    value={tier.coefNum}
                    oninput={(event) =>
                      draft.set(
                        `pricing.tiers.${String(index)}.coef_num`,
                        Number(event.currentTarget.value),
                      )}
                  />
                </td>
                <td>
                  <input
                    type="number"
                    value={tier.coefDen}
                    oninput={(event) =>
                      draft.set(
                        `pricing.tiers.${String(index)}.coef_den`,
                        Number(event.currentTarget.value),
                      )}
                  />
                </td>
                <td>{tier.rank}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
    <Field
      label="Tarif imprimé en grand"
      path="pricing.primary_code"
      value={draft.text('pricing.primary_code')}
      hint="Le prix que le client lit sur l’étiquette (A7)."
      onchange={(value) => draft.set('pricing.primary_code', value)}
    />
    <Field
      label="Tarif encodé dans le code-barres"
      path="pricing.reference_code"
      value={draft.text('pricing.reference_code')}
      hint="Celui que la caisse lit. La caisse ne doit jamais sous-facturer."
      onchange={(value) => draft.set('pricing.reference_code', value)}
    />
  </Panel>

  <Panel title="Les deux arrondis">
    <Field
      label="Arrondi du prix unitaire dérivé"
      path="pricing.unit_price_rounding"
      value={draft.text('pricing.unit_price_rounding')}
      hint="Il s’applique au prix au kilo calculé par un coefficient."
      onchange={(value) => draft.set('pricing.unit_price_rounding', value)}
    />
    <Field
      label="Arrondi du montant"
      path="pricing.amount_rounding"
      value={draft.text('pricing.amount_rounding')}
      hint="Il s’applique au montant de l’étiquette."
      onchange={(value) => draft.set('pricing.amount_rounding', value)}
    />
    <p class="fact muted">
      Les deux arrondis sont distincts parce qu’ils tombent à des endroits différents du
      calcul : arrondir le prix au kilo puis le multiplier, ou multiplier puis arrondir, ne
      donne pas le même centime sur une étiquette. Le poste applique l’arrondi commercial
      (ADR-008) et l’écart se voit au centime près sur la même pesée.
    </p>
  </Panel>

  <Panel
    title="Garde-fous"
    note="Le seuil et le message sont modifiables ; la sévérité est en lecture seule (§6.4)."
  >
    {#each LIMITS as limit (limit.path)}
      <Field
        label={limit.label}
        path={limit.path}
        kind="number"
        value={draft.text(limit.path)}
        hint={limit.hint}
        onchange={(value) => draft.set(limit.path, Number(value))}
      />
    {/each}

    <h3>Ce que les garde-fous ont dit de la dernière pesée</h3>
    {#if diagnostics.length === 0}
      <p class="fact">Aucun verdict : aucune pesée n’a encore été soumise depuis le démarrage.</p>
    {:else}
      <ul class="verdicts">
        {#each diagnostics as verdict (verdict.code)}
          <li data-blocking={String(verdict.blocking)}>
            <span class="what">{verdict.code}</span>
            <span class="detail">{verdict.severity}{verdict.blocking ? ' — bloquant' : ''}</span>
            <span class="message">{verdict.message}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel
    title="Dérogations de poids minimum en vigueur"
    note="En lecture ici ; elles se modifient depuis l’onglet Catalogue, là où se trouve le produit (§10.6)."
  >
    {#if waivers.length === 0}
      <p class="fact">Aucune dérogation : la limite générale s’applique à tous les produits.</p>
    {:else}
      <ul class="verdicts">
        {#each waivers as waiver (waiver.product_id)}
          <li>
            <span class="what">{waiver.product_id}</span>
            <span class="detail">à partir de {frenchInteger(waiver.min_weight_g ?? 0)} g</span>
            <span class="message">{waiver.reason}</span>
            <span class="detail">{frenchDate(waiver.decided_at)}, {waiver.decided_by}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>
</div>

<style>
  .pages {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .fact {
    margin: 0.5rem 0;
    font-size: 1.125rem;
  }

  .muted {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  h3 {
    margin: 1rem 0 0.5rem;
    font-size: 1.25rem;
  }

  /* Un tableau large défile DANS son cadre : la page, elle, ne défile jamais
     horizontalement. */
  .scroll {
    overflow-x: auto;
  }

  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 1.0625rem;
  }

  th,
  td {
    padding: 0.375rem 0.5rem;
    text-align: left;
    border-bottom: 1px solid var(--border);
  }

  th {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  input {
    min-height: 2.5rem;
    width: 100%;
    min-width: 6rem;
    padding: 0 0.5rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .verdicts {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .verdicts li {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.375rem 0;
    border-top: 1px solid var(--border);
    font-size: 1.0625rem;
  }

  .verdicts li[data-blocking='true'] {
    border-left: 0.25rem solid var(--warning);
    padding-left: 0.5rem;
  }

  .what,
  .message {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }
</style>
