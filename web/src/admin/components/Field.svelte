<script lang="ts">
  import { preferences } from '../lib/preferences.svelte'

  /**
   * A field of the configuration: its label, its value, and the why beside it.
   *
   * The path of the key is only shown when it has been asked for. The controls of §11.3
   * do name a KEY when they refuse, but it is now the refusal bar that translates it into
   * French: repeating it under every field of the screen taught something to one person
   * in a hundred, and cluttered the reading of the ninety-nine others.
   */
  interface Props {
    label: string
    path: string
    value: string
    /** Ce que ce réglage décide, en une phrase. */
    hint?: string
    /**
     * `password` masks what is typed and only ever WRITES: the service never serves a
     * password to the browser, so a field opened on its value would open empty and read
     * as a secret that had been wiped.
     */
    kind?: 'text' | 'number' | 'password'
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
