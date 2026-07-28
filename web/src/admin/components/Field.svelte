<script lang="ts">
  import { preferences } from '../lib/preferences.svelte'

  /**
   * Un champ de la configuration : son libellé, sa valeur, et le pourquoi à côté.
   *
   * Le chemin de la clé n'est montré que si on l'a demandé. Les contrôles de §11.3
   * nomment bien une CLÉ quand ils refusent, mais c'est désormais la barre de refus qui
   * la traduit en français : la répéter sous chaque champ de l'écran apprenait quelque
   * chose à une personne sur cent, et encombrait la lecture des quatre-vingt-dix-neuf
   * autres.
   */
  interface Props {
    label: string
    path: string
    value: string
    /** Ce que ce réglage décide, en une phrase. */
    hint?: string
    kind?: 'text' | 'number'
    disabled?: boolean
    /** Le message du contrôle qui a refusé cette clé, vide quand il n'y en a pas. */
    fault?: string
    /** Les valeurs qui MARCHERAIENT, quand le contrôle les connaît (§11.3). */
    allowed?: string[]
    onchange: (value: string) => void
  }

  const {
    label,
    path,
    value,
    hint = '',
    kind = 'text',
    disabled = false,
    fault = '',
    allowed = [],
    onchange,
  }: Props = $props()

  /** L'identifiant du champ, dérivé du chemin de sa clé : un `label` a besoin d'un `for`. */
  const id = $derived(`field-${path.replace(/\./gu, '-')}`)
</script>

<div class="field" class:refused={fault !== ''}>
  <label for={id}>
    <span class="name">{label}</span>
    {#if preferences.showTechnicalNames}<code>{path}</code>{/if}
  </label>
  <input
    {id}
    type={kind}
    {value}
    {disabled}
    list={allowed.length > 0 ? id + '-allowed' : undefined}
    oninput={(event) => onchange(event.currentTarget.value)}
  />
  {#if allowed.length > 0}
    <datalist id={id + '-allowed'}>
      {#each allowed as candidate (candidate)}
        <option value={candidate}></option>
      {/each}
    </datalist>
  {/if}
  {#if hint !== ''}<p class="hint">{hint}</p>{/if}
  {#if fault !== ''}
    <p class="fault">
      {fault}
      {#if allowed.length > 0}<span>Valeurs acceptées : {allowed.join(', ')}.</span>{/if}
    </p>
  {/if}
</div>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.5rem 0;
  }

  label {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem;
  }

  .name {
    font-size: 1.125rem;
    font-weight: 700;
  }

  code {
    font-size: 0.9375rem;
    color: var(--ink-muted);
  }

  input {
    min-height: 3rem;
    padding: 0 0.75rem;
    font: inherit;
    font-variant-numeric: inherit;
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .refused input {
    border-left: 0.5rem solid var(--fault);
  }

  .hint,
  .fault {
    margin: 0;
    font-size: 1rem;
    color: var(--ink-muted);
  }

  .fault {
    padding-left: 0.5rem;
    border-left: 0.25rem solid var(--fault);
  }
</style>
