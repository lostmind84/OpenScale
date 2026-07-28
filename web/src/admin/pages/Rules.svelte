<script lang="ts">
  import { fetchCatalog } from '../../lib/api'
  import Field from '../components/Field.svelte'
  import Panel from '../components/Panel.svelte'
  import type { Draft } from '../lib/draft.svelte'
  import type { HealthDTO } from '../lib/dto'
  import { frenchDate, frenchInteger } from '../lib/format'

  /**
   * The Rules page of §14.4: the tier grid, the two roundings, the FOURTEEN safeguards,
   * the barcode block and the waivers of §10.6.
   *
   * ONE thing here is a design decision, and TWO are gaps to §14.4 that this file writes
   * down rather than dresses up.
   *
   * The design decision is the severity: it is read-only because it says whether a
   * station refuses or warns, which is not a shop setting (§6.4, ADR-025).
   *
   * The first gap is the MESSAGE, quoted here and not editable. §6.4 says the opposite --
   * the messages "sont éditables depuis l'écran Règles" -- and what is missing is a
   * configuration key per message and the route that writes it, NOT the validation: the
   * service already carries `domain.CheckMessage` and `domain.MessagePlaceholders`,
   * written "so that the Rules screen can list them next to the field being edited".
   *
   * The second gap is the live preview §14.4 asks for by name ("à 8 g, ce produit serait
   * refusé"). `domain.Evaluate` returns EVERY diagnostic expressly so this screen can
   * show it, but no route evaluates a weighing that has not happened -- and the answer
   * must come from the kernel, never from a second implementation in the browser that
   * would end up contradicting the label coming out of the printer. What is shown
   * instead is what the safeguards ACTUALLY said about the cycle in flight.
   */
  interface Props {
    draft: Draft
    health: HealthDTO
  }

  const { draft, health }: Props = $props()

  /** How many waivers are drawn before the list defers to the Catalogue page. */
  const WAIVERS_SHOWN = 20

  const tiers = $derived(tiersOf(draft))
  const diagnostics = $derived(health.state.diagnostics)
  const waivers = $derived(health.decisions.filter((decision) => decision.min_weight_g !== null))
  /**
   * The waivers that will never be reached, because the product is withdrawn.
   *
   * One row of `local_decisions` carries BOTH decisions (§10.6): a product may be pulled
   * from the grid and still hold a waiver. Rule 14 then refuses the product before rule 8
   * has any meaning, so calling that waiver « en vigueur » would be false -- it is in
   * force for nothing.
   */
  const deadWaivers = $derived(waivers.filter((decision) => !decision.offered))
  const shownWaivers = $derived(waivers.slice(0, WAIVERS_SHOWN))

  /** Product names by Odoo id, read from the catalog in service. */
  let names = $state<Record<string, string>>({})
  /**
   * Where the reading of the catalog stands.
   *
   * Three states and not a boolean, because an unknown id means three different things.
   * While it is `loading` it means « not read yet », for the length of a round trip; when
   * it is `unread` it means « the station did not answer », which says nothing about the
   * product; only `read` allows the screen to say « absent from the catalog in service »,
   * which is an accusation and must be earned by an answer.
   */
  let namesState = $state<'loading' | 'read' | 'unread'>('loading')

  void loadNames()

  /** One tier of the grid, as the document carries it. */
  interface Tier {
    code: string
    label: string
    abbrev: string
    /** The raw value of `discount_percent` as the document carries it, or null when the tier declares none. */
    written: string | null
    /** The discount in percent when this field can show it exactly, null otherwise. */
    discount: number | null
    rank: number
  }

  /**
   * Reads the tier grid from the draft.
   *
   * It is read from the DOCUMENT rather than from a type: the configuration travels
   * exactly as the file writes it (§11.4), and a screen demanding a fixed shape would
   * refuse a file that a station accepts.
   */
  function tiersOf(source: Draft): Tier[] {
    const value = source.value('pricing.tiers')
    if (!Array.isArray(value)) return []
    return value.map((raw) => {
      const row = (raw ?? {}) as Record<string, unknown>
      const discountValue = row.discount_percent
      return {
        code: String(row.code ?? ''),
        label: String(row.label ?? ''),
        abbrev: String(row.abbrev ?? ''),
        written: discountValue === undefined ? null : String(discountValue),
        discount: showable(discountValue) ? (discountValue as number) : null,
        rank: Number(row.rank ?? 0),
      }
    })
  }

  /**
   * Whether a value read from the document is a discount this field can show.
   *
   * The draft holds whatever a file carries, including what a hand edit put there.
   * Showing 33.333 as « 33,3 » would display a figure nobody declared, and one arrow
   * key would then save it -- so the line falls back to read-only instead.
   *
   * The tenth is tested with a tolerance and not with `Number.isInteger(value * 10)`,
   * because `10.2 * 10` is 101.99999999999999 in binary floating point. That is the
   * very reason the kernel stores tenths as an integer.
   */
  function showable(value: unknown): boolean {
    if (typeof value !== 'number' || !Number.isFinite(value)) return false
    if (value < 0 || value > 100) return false
    return Math.abs(value * 10 - Math.round(value * 10)) < 1e-9
  }

  /** The code of the tier that IS the catalog price, and carries no discount. */
  const referenceCode = $derived(String(draft.value('pricing.reference_code') ?? ''))

  /** A discount as a volunteer writes it: a French comma, no trailing zero. */
  function discountText(discount: number | null): string {
    return discount === null ? '' : String(discount).replace('.', ',')
  }

  /**
   * What the discount does to a price, on a round ten euros.
   *
   * Ten euros is not decoration: `1000 c x (100 - d) / 100` falls exactly on a cent
   * for every discount at a tenth of a point, so this preview needs NO rounding and
   * cannot contradict the label coming out of the printer. It reads no product and
   * calls no route.
   */
  function previewOf(discount: number): string {
    const cents = 1000 - Math.round(discount * 10)
    return `${String(Math.trunc(cents / 100))},${String(cents % 100).padStart(2, '0')}`
  }

  /**
   * Writes a number the operator typed, and writes NOTHING when the field is empty.
   *
   * `Number('')` is 0. Clearing a threshold used to write `0` -- saved by a keystroke
   * that looked like an erasure rather than an edit. An emptied field keeps what the
   * file holds; the way to change a threshold is to type another one.
   */
  function writeNumber(path: string, raw: string): void {
    const value = Number(raw)
    if (raw.trim() === '' || Number.isNaN(value)) return
    draft.set(path, value)
  }

  /**
   * Writes a discount typed with a comma or a dot, and writes nothing otherwise.
   *
   * The second decimal is refused AT THE KEYSTROKE and not at the save: the kernel
   * rejects `10,25` when it decodes, and this screen must not build a file the station
   * will throw back. Same silence as {@link writeNumber} on an empty box, and for the
   * same reason -- erasing « Remise » would drop the member discount on every product.
   */
  function writeDiscount(path: string, raw: string): void {
    const text = raw.trim().replace(',', '.')
    if (!/^\d{1,3}(\.\d)?$/u.test(text)) return
    const value = Number(text)
    if (value > 100) return
    draft.set(path, value)
  }

  /**
   * Puts back in a box the value the draft actually holds.
   *
   * The writers above stay silent on an empty or malformed box, so the draft keeps what
   * the file holds -- but every box here is driven by `value=`, and an edit that changes
   * no state renders nothing: the box STAYED wrong on screen while the configuration
   * held something else. Restoring on the way OUT rather than on each keystroke is what
   * lets « effacer puis retaper » still work.
   */
  function restoreBox(target: EventTarget | null, stored: string): void {
    if (!(target instanceof HTMLInputElement) || target.value === stored) return
    target.value = stored
  }

  /**
   * Reads the catalog in service, to NAME the products a waiver applies to.
   *
   * §14.4 asks for "un produit nommé par ligne", and the health payload carries only an
   * Odoo id. The catalog comes from the CLIENT route, which asks for no password and
   * which the browser may already hold in cache: it is the only way to name a product
   * here without inventing a second search route. The Catalogue page does the same.
   */
  async function loadNames(): Promise<void> {
    try {
      const catalog = await fetchCatalog()
      names = Object.fromEntries(
        catalog.products.map((product): [string, string] => [product.id, product.name]),
      )
      namesState = 'read'
    } catch {
      // A station that cannot answer must not empty the list of waivers: the id alone is
      // still enough to find the product in Odoo, and a blank line would say « none ».
      namesState = 'unread'
    }
  }

  /**
   * The name of a product, or an honest sentence when the catalog does not carry it.
   *
   * The three states are told apart, and each one says a DIFFERENT thing. « Absent du
   * catalogue » is an accusation -- it says the shop sells a product its own catalog does
   * not know -- and it may only be said once the catalog has actually answered. While it
   * is being read, and when the read failed, the screen says it does not know.
   */
  function nameOf(id: string): string {
    const name = names[id]
    if (name !== undefined) return name
    if (namesState === 'loading') return 'Lecture du nom…'
    return namesState === 'read' ? 'Produit absent du catalogue en service' : 'Nom non lu'
  }

  /** A French plural the screen must not get wrong. */
  function tierCount(count: number): string {
    return count <= 1 ? `${String(count)} tarif déclaré` : `${String(count)} tarifs déclarés`
  }

  /** « 1 verdict » / « 3 verdicts », and what they are counted against. */
  const verdictCount = $derived(
    `${frenchInteger(diagnostics.length)} ` +
      `${diagnostics.length > 1 ? 'verdicts' : 'verdict'} sur les quatorze garde-fous.`,
  )

  /**
   * How many waivers are drawn, out of how many there are, and how many are in force.
   *
   * A waiver is counted in PRODUCTS: nothing bounds this list but the shop itself, so it
   * is capped when drawn — and a cap that does not say what it hides is a lie by
   * omission. The sentence therefore always carries the total, and it stops saying « en
   * vigueur » of the whole set as soon as one of them sits on a withdrawn product.
   */
  const waiverTotal = $derived(
    `${frenchInteger(shownWaivers.length)} ` +
      `${shownWaivers.length > 1 ? 'lignes affichées' : 'ligne affichée'} sur ` +
      `${frenchInteger(waivers.length)} ` +
      (deadWaivers.length === 0
        ? `${waivers.length > 1 ? 'dérogations en vigueur' : 'dérogation en vigueur'}.`
        : `${waivers.length > 1 ? 'dérogations enregistrées' : 'dérogation enregistrée'}, ` +
          `dont ${frenchInteger(deadWaivers.length)} sur un produit retiré, sans effet : ` +
          'le garde-fou 14 refuse le produit avant que le 8 ait un sens.') +
      (waivers.length > shownWaivers.length
        ? ' Les autres se lisent produit par produit depuis l’onglet Catalogue.'
        : ''),
  )

  /**
   * The floor of a waiver, said at the BOUND the kernel actually applies.
   *
   * Rule 8 fires on `net <= floor` (`internal/domain/safeguard.go`), so a net weight
   * EQUAL to the floor is refused. « À partir de 8 g » said the opposite about the very
   * gram the waiver was posed for.
   */
  function waiverFloor(grams: number): string {
    return `plancher ${frenchInteger(grams)} g : refusé à ${frenchInteger(grams)} g et en dessous`
  }

  /** One threshold of §11.2 that a safeguard reads, edited here and nowhere else. */
  interface Threshold {
    path: string
    label: string
    hint: string
  }

  /** One of the fourteen safeguards of §6.4. */
  interface Safeguard {
    /** Its rank in the EVALUATION ORDER, which is normative (§6.4). */
    rank: number
    /** The identifier the journal and the telephone use. English, and secondary. */
    code: string
    /** What a volunteer reads. */
    label: string
    /** What makes it fire, in one sentence. */
    when: string
    /** French, read-only: it says who has to act, and it is not a shop setting. */
    severity: string
    /** True when the rule stops the label: it draws the row, and it is never editable. */
    blocking: boolean
    /** The wording the customer reads, QUOTED from `internal/domain/safeguard.go`. */
    message: string
    /** The keys this rule owns. A key belongs to exactly one rule, and is edited once. */
    thresholds: Threshold[]
    /** The only rule that a configuration can switch off, empty for the other thirteen. */
    switchPath: string
    switchLabel: string
    /** Where the threshold lives when it is not a key this rule owns. */
    note: string
  }

  /**
   * The fourteen safeguards of §6.4, in EVALUATION ORDER, with their thresholds.
   *
   * The list is the one of `internal/domain/safeguard.go` and nothing else -- a test
   * reads that file and refuses a code this table invents or forgets. The messages are
   * QUOTED from the same file, straight apostrophes included: they are what the customer
   * reads, not prose written for this screen. That they are only quoted is a GAP to §6.4,
   * which makes them editable from here: what is missing is a configuration key per
   * message and the route that writes it -- the validation of a submitted message already
   * exists in Go (`domain.CheckMessage`).
   */
  const SAFEGUARDS: Safeguard[] = [
    {
      rank: 1,
      code: 'OVERLOAD',
      label: 'Surcharge',
      when: 'La balance annonce elle-même OL, ou le poids brut dépasse la capacité.',
      severity: 'Bloquant',
      blocking: true,
      message: "La balance est en surcharge. Retirez votre article.",
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note: 'Seuil : la capacité limits.max_weight_g, réglée au garde-fou 9.',
    },
    {
      rank: 2,
      code: 'MEASUREMENT_EXPIRED',
      label: 'Poids périmé',
      when: 'La mesure est plus vieille que la péremption, dans les deux modes de stabilité.',
      severity: 'Bloquant',
      blocking: true,
      message: "Poids indisponible. Patientez ou appelez un bénévole.",
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note: 'Aucun seuil à régler : le poste calcule lui-même à partir du rythme de la balance.',
    },
    {
      rank: 3,
      code: 'BASKET_MISSING',
      label: 'Panier absent',
      when: 'Le poids brut tombe dans la fenêtre négative du panier : il a été soulevé.',
      severity: 'Bloquant',
      blocking: true,
      message: "Le panier n'est pas sur la balance. Reposez-le.",
      thresholds: [
        {
          path: 'limits.basket_min_g',
          label: 'Bas de la fenêtre du panier',
          hint: 'En grammes, NÉGATIF : c’est le poids du panier que la balance a perdu.',
        },
        {
          path: 'limits.basket_max_g',
          label: 'Haut de la fenêtre du panier',
          hint: 'Négatif lui aussi, et plus proche de zéro que le bas.',
        },
      ],
      switchPath: 'limits.basket_check_enabled',
      switchLabel: 'Ce poste travaille avec un panier taré',
      note: 'La règle s’active ou non, en bloc : il n’y a pas de demi-mesure à régler.',
    },
    {
      rank: 4,
      code: 'SCALE_EMPTY',
      label: 'Plateau vide',
      when: 'Le poids brut ne sort pas de la bande « il n’y a rien sur le plateau ».',
      severity: 'Bloquant — un filet, hors parcours nominal',
      blocking: true,
      message: "Posez votre produit.",
      thresholds: [
        {
          path: 'limits.empty_max_g',
          label: 'Plateau considéré vide',
          hint: 'En dessous, le poste considère qu’il n’y a rien sur le plateau.',
        },
      ],
      switchPath: '',
      switchLabel: '',
      note:
        'Toucher une tuile sur un plateau vide ARME la sélection au lieu d’être refusé : ' +
        'la règle reste évaluée pour la saisie manuelle et les chemins dérivés.',
    },
    {
      rank: 5,
      code: 'TARE_REQUIRED',
      label: 'Remise à zéro nécessaire',
      when: 'Le brut est sous la bande du plateau vide, et hors de la fenêtre du panier.',
      severity: 'Bloquant',
      blocking: true,
      message: "La balance doit être remise à zéro.",
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note: 'Seuil : moins limits.empty_max_g, réglé au garde-fou 4.',
    },
    {
      rank: 6,
      code: 'WEIGHT_UNSTABLE',
      label: 'Pesée instable',
      when: 'La trame déclare la mesure instable.',
      severity: 'Information par défaut (A3)',
      blocking: false,
      message: "Pesée en cours…",
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note:
        'La sévérité suit l’exigence de stabilité : elle passe à Bloquant quand celle-ci ' +
        'est réglée sur « blocking ». L’impression n’est jamais bloquée par défaut.',
    },
    {
      rank: 7,
      code: 'TARE_INVALID',
      label: 'Emballage incohérent',
      when: 'Une tare a été saisie, et elle atteint la pesée ou dépasse le maximum.',
      severity: 'Bloquant',
      blocking: true,
      message: "Le poids de l'emballage est supérieur ou égal à la pesée.",
      thresholds: [
        {
          path: 'limits.max_tare_g',
          label: 'Tare maximum',
          hint: 'Une tare plus lourde que le maximum est une faute de frappe.',
        },
      ],
      switchPath: '',
      switchLabel: '',
      note: '',
    },
    {
      rank: 8,
      code: 'WEIGHT_TOO_LOW',
      label: 'Poids trop faible',
      // « ne dépasse pas » and not « n'atteint pas »: the kernel fires on `net <= floor`,
      // so a net weight EQUAL to the floor is refused. The wording of this table is
      // uniform on that point -- « atteint » is >=, « dépasse » is > (see rule 7).
      when: 'Vente au poids : le NET est strictement positif et ne dépasse pas le plancher.',
      severity: 'Bloquant',
      blocking: true,
      message: "La balance doit être retarée, ou l'emballage est trop lourd.",
      thresholds: [
        {
          path: 'limits.min_weight_g',
          label: 'Poids minimum',
          hint: 'Une dérogation par produit existe, dans l’onglet Catalogue.',
        },
      ],
      switchPath: '',
      switchLabel: '',
      note: '',
    },
    {
      rank: 9,
      code: 'WEIGHT_TOO_HIGH',
      label: 'Poids trop élevé',
      when: 'Le NET dépasse la capacité — strictement, pour que la capacité reste atteignable.',
      severity: 'Bloquant',
      blocking: true,
      message: "{{.Weight}} kg, ça paraît un peu lourd !",
      thresholds: [
        {
          path: 'limits.max_weight_g',
          label: 'Poids maximum',
          hint: 'C’est la capacité du champ NNDDD du code-barres, pas un seuil de vraisemblance.',
        },
      ],
      switchPath: '',
      switchLabel: '',
      note: '',
    },
    {
      rank: 10,
      code: 'UNITS_OUT_OF_RANGE',
      label: 'Nombre d’unités hors plage',
      when: 'Vente à l’unité : la quantité sort de la plage.',
      severity: 'Bloquant',
      blocking: true,
      message: "{{.Quantity}} unités, ça paraît un peu beaucoup !",
      thresholds: [
        { path: 'limits.min_units', label: 'Unités minimum', hint: '' },
        { path: 'limits.max_units', label: 'Unités maximum', hint: '' },
      ],
      switchPath: '',
      switchLabel: '',
      note: '',
    },
    {
      rank: 11,
      code: 'AMOUNT_OUT_OF_CAPACITY',
      label: 'Montant hors capacité du code-barres',
      when: 'La charge utile encode un PRIX, et il dépasse ce que le champ peut porter.',
      severity: 'Bloquant',
      blocking: true,
      message: "Prix trop élevé pour le code-barres.",
      thresholds: [
        {
          path: 'limits.max_amount_cents',
          label: 'Montant maximum',
          hint: 'En centimes. Aucun préfixe du plan livré n’encode un prix : la règle est ' +
            'éprouvée sans qu’aucun produit puisse l’atteindre.',
        },
      ],
      switchPath: '',
      switchLabel: '',
      note: '',
    },
    {
      rank: 12,
      code: 'ZERO_PRICE',
      label: 'Prix nul',
      when: 'Le montant du tarif imprimé en grand vaut zéro.',
      severity: 'Bloquant',
      blocking: true,
      message: "Prix nul. Appelez un bénévole.",
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note: 'Aucun seuil : un produit à 0 € est une anomalie sans nuance.',
    },
    {
      rank: 13,
      code: 'LIGHT_PRODUCT_ALLOWED',
      label: 'Produit léger autorisé',
      when: 'Le garde-fou 8 n’a pas déclenché grâce à la dérogation du produit.',
      severity: 'Information',
      blocking: false,
      message: '',
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note:
        'Aucun seuil général : c’est la dérogation par produit, listée plus bas et posée ' +
        'depuis l’onglet Catalogue. Rien ne s’affiche au client ; l’id du produit ' +
        'est journalisé.',
    },
    {
      rank: 14,
      code: 'PRODUCT_WITHDRAWN',
      label: 'Produit retiré',
      when: 'Quelqu’un a décidé de ne plus proposer ce produit.',
      severity: 'Bloquant',
      blocking: true,
      message: "Ce produit n'est pas disponible.",
      thresholds: [],
      switchPath: '',
      switchLabel: '',
      note:
        'Aucun seuil : c’est une décision humaine, prise depuis l’onglet Catalogue. ' +
        'Aucune règle d’import ne peut la déduire.',
    },
  ]

  /**
   * How a verdict names itself to a volunteer.
   *
   * The stream spells the code in English -- `WEIGHT_TOO_LOW` -- and a volunteer at the
   * counter must not have to translate it. An unknown code says so instead of being
   * printed as a label: this screen can be older than the binary it talks to.
   */
  function labelOfCode(code: string): string {
    return SAFEGUARDS.find((rule) => rule.code === code)?.label ?? 'Garde-fou inconnu de cet écran'
  }

  /**
   * The markers the shipped messages carry, out of the CLOSED list of `safeguard.go`.
   *
   * They are named on screen because they are visible in the quotations above: a reader
   * who does not know what `{{.Weight}}` is takes it for a defect of the message.
   */
  const PLACEHOLDERS = ['{{.Weight}}', '{{.Quantity}}']

  /** How a severity reads. The service spells it `blocking` or `info`. */
  function frenchSeverity(severity: string): string {
    if (severity === 'blocking') return 'Bloquant'
    if (severity === 'info') return 'Information'
    return 'Sévérité inconnue de cet écran'
  }
</script>

<!--
  A switch, drawn the same way wherever a configuration carries a boolean. `Field` has
  no boolean kind, and it should not grow one for the two flags of this page.
-->
{#snippet toggle(path: string, label: string, hint: string)}
  <label class="toggle" data-flag={path} data-on={String(draft.flag(path))}>
    <input
      type="checkbox"
      checked={draft.flag(path)}
      onchange={(event) => draft.set(path, event.currentTarget.checked)}
    />
    <span class="toggle-text">
      <span class="toggle-label">{label}</span>
      <code>{path}</code>
      {#if hint !== ''}<span class="hint">{hint}</span>{/if}
    </span>
  </label>
{/snippet}

<div class="pages">
  <Panel
    title="Grille de tarifs"
    note="Un second tarif n’est pas une case à cocher : c’est une ligne de plus dans cette grille."
  >
    {#if tiers.length === 0}
      <p class="fact">Aucun tarif déclaré dans la configuration lue.</p>
    {:else}
      <p class="fact muted" data-tier-count>{tierCount(tiers.length)}.</p>
      <div class="scroll">
        <table>
          <thead>
            <tr>
              <th>Code</th>
              <th>Libellé</th>
              <th>Abrégé</th>
              <th>Remise</th>
              <th>Ordre</th>
            </tr>
          </thead>
          <tbody>
            <!--
              Keyed by POSITION, and that is not a shortcut. `tiersOf` replaces a missing
              code by the empty string, so two tiers without one collided on the same key
              and Svelte drew a single row. The position is also what the edits below
              write -- `pricing.tiers.<n>.<field>` -- so the row and its key say the same
              thing, and a reordered grid can no longer write into the wrong tier.
            -->
            {#each tiers as tier, index (index)}
              <tr>
                <td>{tier.code}</td>
                <td>
                  <input
                    aria-label="Libellé du tarif {index + 1}"
                    value={tier.label}
                    oninput={(event) =>
                      draft.set(`pricing.tiers.${String(index)}.label`, event.currentTarget.value)}
                  />
                </td>
                <td>
                  <input
                    aria-label="Abrégé du tarif {index + 1}"
                    value={tier.abbrev}
                    oninput={(event) =>
                      draft.set(`pricing.tiers.${String(index)}.abbrev`, event.currentTarget.value)}
                  />
                </td>
                <td>
                  <!--
                    The reference tier is split in two by PRESENCE, not by legality. An
                    operator can retarget `pricing.reference_code` onto a tier that already
                    carries an ordinary, legal discount (the field just below does exactly
                    that, mid-session, with no file edit involved) -- and a screen that then
                    printed « pas de remise » over a document that says 20 % would be
                    hiding a declared value, which is as dishonest as inventing one. So the
                    reference tier with NO key gets the reassuring sentence, and the
                    reference tier WITH one gets told what saving will do to it.
                  -->
                  {#if tier.code === referenceCode && tier.written === null}
                    <span class="locked">Prix du catalogue Odoo — pas de remise</span>
                  {:else if tier.code === referenceCode}
                    <span class="locked">
                      {tier.written} — le tarif de référence est le prix du catalogue : il
                      ne peut pas porter de remise, et l’enregistrement la refusera.
                    </span>
                  {:else if tier.written !== null && tier.discount === null}
                    <span class="locked">
                      {tier.written} — une remise s’écrit au dixième de point ; celle-ci se
                      change dans le fichier de configuration.
                    </span>
                  {:else}
                    <!--
                      Catches BOTH the ordinary case (tier.discount holds a value) and a
                      non-reference tier that carries no `discount_percent` key at all
                      (tier.written === null): for a tier that is not the reference, an
                      absent key means exactly 0 % (the kernel's own rule), and an editable
                      field showing 0 is honest, not invented -- unlike the reference row
                      above, which has nothing to show because it has no discount to hold.
                    -->
                    <input
                      type="text"
                      inputmode="decimal"
                      aria-label="Remise du tarif {index + 1}"
                      value={discountText(tier.discount ?? 0)}
                      oninput={(event) =>
                        writeDiscount(
                          `pricing.tiers.${String(index)}.discount_percent`,
                          event.currentTarget.value,
                        )}
                      onfocusout={(event) =>
                        restoreBox(event.currentTarget, discountText(tier.discount ?? 0))}
                    /> %
                    <span class="hint">
                      un produit à 10,00 €/kg s’affiche {previewOf(tier.discount ?? 0)} €/kg
                    </span>
                  {/if}
                </td>
                <td>{tier.rank}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="fact muted">
        Un champ vidé garde la valeur du fichier : il n’écrit pas zéro, et la case la
        retrouve dès qu’on quitte le champ. Une remise effacée serait le plein tarif pour
        tous les adhérents.
      </p>
    {/if}
    <Field
      label="Tarif imprimé en grand"
      path="pricing.primary_code"
      value={draft.text('pricing.primary_code')}
      hint="Le prix que le client lit sur l’étiquette (A7)."
      onchange={(value) => draft.set('pricing.primary_code', value)}
    />
    <Field
      label="Tarif qui serait encodé si le code-barres portait un prix"
      path="pricing.reference_code"
      value={draft.text('pricing.reference_code')}
      hint="Le plan livré ne porte pas de prix, mais un poids ou un nombre d’unités : la caisse retrouve le prix par la référence, dans Odoo, et ne doit jamais sous-facturer."
      onchange={(value) => draft.set('pricing.reference_code', value)}
    />
  </Panel>

  <Panel title="Les deux arrondis">
    <Field
      label="Arrondi du prix unitaire dérivé"
      path="pricing.unit_price_rounding"
      value={draft.text('pricing.unit_price_rounding')}
      hint="Il s’applique au prix au kilo calculé par la remise."
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
      donne pas le même centime sur une étiquette. Le poste applique l’arrondi commercial,
      et l’écart se voit au centime près sur la même pesée.
    </p>
  </Panel>

  <Panel
    title="Les quatorze garde-fous, dans l’ordre d’évaluation"
    note="Le seuil se modifie ici. La sévérité ne se règle pas : elle dit si le poste refuse ou avertit, et cela ne dépend pas du magasin. Le message affiché au client n’est pas encore modifiable depuis cet écran."
  >
    <p class="fact muted">
      L’ordre compte : le premier verdict bloquant décide de ce que le client lit. Les
      garde-fous 1 à 7 portent sur l’état de la balance, c’est-à-dire le poids brut ; les
      garde-fous 8 à 14 portent sur la vente, c’est-à-dire le net. Le code en gris est
      celui du journal, celui qu’on lit au téléphone.
    </p>

    <ol class="rules">
      {#each SAFEGUARDS as rule (rule.code)}
        <li class="rule" data-code={rule.code}>
          <p class="rule-head">
            <span class="rank">{rule.rank}</span>
            <span class="rule-label">{rule.label}</span>
            <code class="token">{rule.code}</code>
            <span class="severity" data-blocking={String(rule.blocking)}>{rule.severity}</span>
          </p>
          <p class="when">{rule.when}</p>

          {#if rule.message === ''}
            <p class="quote silent">Rien ne s’affiche au client : c’est une information.</p>
          {:else}
            <p class="quote">« {rule.message} »</p>
          {/if}

          {#if rule.switchPath !== ''}
            {@render toggle(rule.switchPath, rule.switchLabel, '')}
          {/if}

          {#each rule.thresholds as threshold (threshold.path)}
            <!--
              The wrapper carries no layout — `display: contents` — and exists for the
              event alone: `focusout` bubbles to it, and that is where a box left empty
              gets the value of the file back. `Field` has no such hook, and it should not
              grow one for a rule that belongs to this page.
            -->
            <div
              class="box"
              onfocusout={(event) => restoreBox(event.target, draft.text(threshold.path))}
            >
              <Field
                label={threshold.label}
                path={threshold.path}
                kind="number"
                value={draft.text(threshold.path)}
                hint={threshold.hint}
                onchange={(value) => writeNumber(threshold.path, value)}
              />
            </div>
          {/each}

          {#if rule.note !== ''}<p class="note">{rule.note}</p>{/if}
        </li>
      {/each}
    </ol>

    <p class="fact muted">
      Un seuil vidé garde la valeur du fichier : il n’écrit pas zéro, et la case la retrouve
      dès qu’on quitte le champ. Pour changer un seuil, on tape l’autre valeur.
    </p>

    <p class="fact muted">
      Les marqueurs {PLACEHOLDERS.join(' et ')} sont remplacés par les valeurs de la pesée au
      moment où le message s’affiche.
    </p>

    <h3>Ce que les garde-fous disent de la pesée en cours</h3>
    {#if diagnostics.length === 0}
      <!--
        What an empty list says, and what it does not. These verdicts bear on the CYCLE
        IN FLIGHT: the machine clears them on its way back to rest. « No weighing has
        been submitted since start-up » was therefore false from the second second of
        operation, and cast doubt on a station that had just printed a label.
      -->
      <p class="fact" data-verdicts="0">
        Aucun verdict en ce moment : ces lignes portent sur la pesée EN COURS, et le poste
        est au repos. Elles apparaissent le temps d’un cycle, quand un client pose son sac
        et touche une tuile.
      </p>
    {:else}
      <p class="fact muted" data-verdicts={String(diagnostics.length)}>{verdictCount}</p>
      <ul class="verdicts">
        {#each diagnostics as verdict (verdict.code)}
          <li data-blocking={String(verdict.blocking)}>
            <span class="what">{labelOfCode(verdict.code)}</span>
            <code class="token">{verdict.code}</code>
            <span class="detail">{frenchSeverity(verdict.severity)}</span>
            {#if verdict.message !== ''}<span class="message">« {verdict.message} »</span>{/if}
            <!--
              The product id, when the verdict carries one. Rule 13 is the only one that
              fills it and its message is EMPTY: without this, the line read « Produit
              léger autorisé » without saying which product — that is, without the one
              piece of information the rule exists to record (§6.4, ADR-017).
            -->
            {#if verdict.product_id !== ''}
              <span class="detail">{nameOf(verdict.product_id)}</span>
              <code class="token">{verdict.product_id}</code>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </Panel>

  <Panel
    title="Code-barres"
    note="Un seul réglage, et c’est voulu : le reste du plan de numérotation n’est pas de la configuration."
  >
    {@render toggle(
      'barcode.verify_reference_check_digit',
      'Refuser une référence dont la clé de contrôle est fausse',
      'Décoché, le poste recalcule une clé juste sur une référence fausse, en silence — et la caisse encaisse un autre article.',
    )}
    <p class="fact muted">
      Le plan de numérotation n’est PAS ici, et c’est tout l’intérêt : préfixes, largeur de
      la référence, largeur de la charge utile, décimales et mode de vente sont une
      constante du binaire, indexée par préfixe et vérifiée au démarrage.
      Un champ qui change le SENS du code lu par la caisse n’est pas un réglage, c’est un
      contrat externe : il change avec une version du binaire, relue et testée, jamais
      depuis l’écran d’un poste.
    </p>
  </Panel>

  <!--
    « Dérogations de poids minimum », and no longer « en vigueur » in the title: one row of
    `local_decisions` carries both the waiver and the choice to withdraw the product, so
    part of this list may be in force for nothing. Which ones is said line by line, and in
    the total above them, rather than promised by a heading.
  -->
  <Panel
    title="Dérogations de poids minimum"
    note="En lecture ici ; elles se modifient depuis l’onglet Catalogue, là où se trouve le produit."
  >
    {#if waivers.length === 0}
      <p class="fact">Aucune dérogation : la limite générale s’applique à tous les produits.</p>
    {:else}
      <p class="fact muted" data-waiver-total>{waiverTotal}</p>
      {#if namesState === 'unread'}
        <p class="unread">
          Les noms de produits n’ont pas pu être lus : le catalogue en service n’a pas
          répondu. Les identifiants Odoo restent affichés.
        </p>
      {/if}
      <ul class="verdicts waivers">
        {#each shownWaivers as waiver (waiver.product_id)}
          <li data-withdrawn={String(!waiver.offered)}>
            <span class="what">{nameOf(waiver.product_id)}</span>
            <code class="token">{waiver.product_id}</code>
            <span class="detail">{waiverFloor(waiver.min_weight_g ?? 0)}</span>
            <span class="message">{waiver.reason}</span>
            <span class="detail">{frenchDate(waiver.decided_at)}, {waiver.decided_by}</span>
            {#if !waiver.offered}
              <span class="dead">
                Produit retiré : le garde-fou 14 refuse le produit avant que cette
                dérogation ait un sens.
              </span>
            {/if}
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

  /* A cell this screen must not let the operator edit: the catalog price, or a discount
     it cannot show without inventing a figure nobody declared. Text only, no field
     border -- there is nothing here to click into. */
  .locked {
    color: var(--ink-muted);
    font-size: 1rem;
  }

  h3 {
    margin: 1.5rem 0 0.5rem;
    font-size: 1.25rem;
  }

  /* A wide table scrolls INSIDE its frame: the body of the page never scrolls
     horizontally. */
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
    /* 44 px: the density of the settings pages, which are driven with a mouse (ADR-033).
       The 72 px of the client screen stay for destructive gestures, and this page has
       none. */
    min-height: 2.75rem;
    width: 100%;
    min-width: 6rem;
    padding: 0 0.5rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  /*
   * The fourteen rules, each in its own frame.
   *
   * A table would have put them side by side and forced a horizontal scroll as soon as a
   * rule carries two thresholds; stacked, each rule reads as one block -- what it
   * refuses, what the customer reads, and the number that decides it.
   */
  .rules {
    margin: 0.75rem 0 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .rule {
    padding: 0.75rem 1rem 1rem;
    background: var(--bg);
    border: 1px solid var(--border-soft);
    border-radius: var(--radius-sm);
  }

  .rule-head {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
    margin: 0;
  }

  .rank {
    flex: none;
    min-width: 1.75rem;
    color: var(--ink-muted);
    font-variant-numeric: tabular-nums;
    font-weight: 700;
  }

  .rule-label {
    font-size: 1.125rem;
    font-weight: 700;
  }

  /* The English token of the service is only good for the telephone and the journal: it
     stands second, never in the place of the French label. */
  .token {
    color: var(--ink-muted);
    font-size: 0.9375rem;
  }

  .severity {
    margin-left: auto;
    padding: 0.125rem 0.625rem;
    font-size: 0.9375rem;
    border-radius: var(--radius-pill);
    background: var(--waiting-wash);
  }

  .severity[data-blocking='true'] {
    background: var(--warning-wash);
  }

  .when {
    margin: 0.375rem 0 0;
    font-size: 1rem;
  }

  /*
   * What the customer reads, laid out as a small screen inside the screen: it is a
   * QUOTATION of the shipped message (`internal/domain/safeguard.go`), not a sentence
   * written here.
   */
  .quote {
    margin: 0.5rem 0 0;
    padding: 0.5rem 0.75rem;
    font-size: 1.0625rem;
    background: var(--surface);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
  }

  .quote.silent {
    color: var(--ink-muted);
    font-size: 1rem;
    box-shadow: none;
    background: var(--waiting-wash);
  }

  .note {
    margin: 0.5rem 0 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .toggle {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-height: 2.75rem;
    margin: 0.5rem 0 0;
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--waiting-wash);
    cursor: pointer;
    transition:
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease);
  }

  .toggle[data-on='true'] {
    background: var(--ready-wash);
  }

  /* What a mouse expects, and a finger never asked for (app.css). */
  @media (hover: hover) {
    .toggle:hover {
      border-color: var(--ink-muted);
    }
  }

  .toggle input {
    flex: none;
    width: 1.5rem;
    height: 1.5rem;
    min-height: 0;
    min-width: 0;
    padding: 0;
    accent-color: var(--focus);
  }

  .toggle-text {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: baseline;
  }

  .toggle-label {
    font-size: 1.0625rem;
    font-weight: 700;
  }

  .hint {
    flex: 1 1 20rem;
    color: var(--ink-muted);
    font-size: 1rem;
  }

  /* A frame for an EVENT and not for a layout: the field keeps the place it had. */
  .box {
    display: contents;
  }

  .verdicts {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  /*
   * A list of waivers is counted in products, not in configuration lines: it is capped
   * when drawn AND inside its frame, and its total is announced above it.
   */
  .waivers {
    max-height: 24rem;
    overflow-y: auto;
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
    background: var(--warning-wash);
    padding-left: 0.5rem;
  }

  .what,
  .message {
    font-weight: 700;
  }

  .detail {
    color: var(--ink-muted);
  }

  /* A waiver in force for nothing: the row stays, and it says why it decides nothing. */
  .dead {
    flex: 1 1 20rem;
    padding: 0.125rem 0.625rem;
    font-size: 1rem;
    background: var(--warning-wash);
    border-radius: var(--radius-sm);
  }

  /* A read that failed says so: a silent list would be read as « there are none », which
     is false. */
  .unread {
    margin: 0.5rem 0;
    padding: 0.5rem 0.75rem;
    font-size: 1rem;
    background: var(--fault-wash);
    border-left: 0.25rem solid var(--fault);
    border-radius: var(--radius-sm);
  }
</style>
