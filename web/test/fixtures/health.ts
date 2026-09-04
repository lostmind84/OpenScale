import type { HealthDTO, ImportDTO } from '../../src/admin/lib/dto'
import type { StateDTO } from '../../src/lib/dto'

/**
 * Le tableau de bord d'un poste, dans la forme exacte de `GET /admin/api/health`.
 *
 * Les chiffres du catalogue sont ceux de la pièce de référence, et ils viennent de deux
 * sources qui se recoupent : §14.4 les écrit à voix haute — « 355 produits reçus · 331
 * pesables (181 avec photo, 174 sans) · 8 non pesables · 16 anomalies · + 1 unité
 * divergente » — et `internal/catalog/csvodoo/fixtures_test.go` les gèle sur le vrai
 * `testdata/catalog/flv.csv`. Aucun chiffre de ce fichier n'est inventé.
 */

/** L'inventaire de `flv.csv`, les 355 produits réels (§14.4). */
export const FLV_IMPORT: ImportDTO = {
  id: 7,
  // Midi UTC : la date se lit en heure locale, et un instant de nuit ferait basculer le
  // 24/07 sur le 23 ou le 25 selon le fuseau de la machine de test.
  occurred_at: '2026-07-24T12:00:00.000Z',
  source: 'local_drop',
  file_name: 'flv_2.csv',
  result: 'applied',
  code: '',
  reason: '',
  rows_read_count: 355,
  unreadable_rows_count: 0,
  weighable_count: 331,
  not_weighable_count: 8,
  anomalies_count: 16,
  unit_mismatches_count: 1,
  images_decoded_count: 181,
  images_rejected_count: 0,
  products_withdrawn_count: 0,
  duration_ms: 420,
}

/**
 * L'inventaire de `flv_1.csv`, l'export de 2022 : « 153 reçus · 107 pesables · 39 non
 * pesables · 7 anomalies · 5 unités divergentes » (§14.4).
 *
 * Il est ici parce que §14.4 impose que LES DEUX tiennent sur une ligne et restent
 * lisibles : c'est la seule contrainte que cet écran fait peser sur le chiffre.
 */
export const FLV_1_IMPORT: ImportDTO = {
  ...FLV_IMPORT,
  id: 6,
  file_name: 'flv_1.csv',
  rows_read_count: 153,
  weighable_count: 107,
  not_weighable_count: 39,
  anomalies_count: 7,
  unit_mismatches_count: 5,
  images_decoded_count: 0,
}

/** Un instantané au repos, matériel nominal — la forme exacte de `stateDTO`. */
export function nominalState(overrides: Partial<StateDTO> = {}): StateDTO {
  return {
    revision: 12,
    at: '2026-07-24T12:00:00.000Z',
    state: 'idle',
    station: 2,
    weight: {
      available: true,
      expired: false,
      gross_g: 0,
      tare_g: 0,
      net_g: 0,
      quantity: 1,
      net_text: '0,000',
      stability: 'stable',
      latched: false,
      seq: 42,
      age_ms: 120,
      expiry_ms: 1200,
    },
    product: null,
    label: null,
    last_label: null,
    reprint: { available: false, job_id: '', printed_at: '' },
    message: null,
    sound: '',
    diagnostics: [],
    fault_code: '',
    arming_expires_at: '',
    scale: {
      connected: true,
      median_ms: 400,
      observations_count: 64,
      provisional: false,
      too_slow: false,
    },
    printer: {
      health: 'ready',
      detail: '',
      pending_jobs_count: 0,
      observed_at: '2026-07-24T12:00:00.000Z',
    },
    degraded: null,
    catalog_count: 331,
    // Figé, comme l'empreinte ci-dessous : seul son CHANGEMENT a un sens, et
    // `session.test.ts` est le banc qui le fait bouger.
    catalog_updated_at: '2026-07-27T08:06:48Z',
    // Opaque, et figée : seul son CHANGEMENT a un sens, et aucun de ces bancs n'en
    // décrit un. `session.test.ts` est celui qui la fait bouger.
    presentation_digest: 'p1',
    unlogged_weighings_count: 0,
    ...overrides,
  }
}

/** Un tableau de bord de poste nominal, que chaque test déforme sur un point. */
export function nominalHealth(overrides: Partial<HealthDTO> = {}): HealthDTO {
  return {
    version: '1.0.0',
    config_fingerprint: 'a1b2c3d4',
    station: 2,
    station_name: 'Poste 2 — fruits',
    coop: 'La Cagette',
    alive: true,
    state: nominalState(),
    scale_present: true,
    // Un poste nominal tourne sur `raster`, qui honore les trois auto-tests de §8.6.
    printer_self_tests: ['label', 'alignment', 'ruler'],
    // Les quatre transports de §8.4, chacun disant dans quelle clé de `printer.options` il
    // désigne son appareil. C'est là-dessus, et sur rien d'autre, que la page Matériel
    // décide où écrire ce qu'un bénévole saisit.
    printer_transports: [
      { id: 'winspool', label: 'File d’impression Windows (RAW)', key: 'queue' },
      { id: 'devfile', label: 'Nœud d’impression du système', key: 'path' },
      { id: 'tcp', label: 'Imprimante réseau, port 9100', key: 'address' },
      { id: 'file', label: 'Fichier — développement, tests, support à distance', key: 'path' },
    ],
    counters: { unlogged_weighings_count: 0, journal_rows_count: 1236 },
    events: [],
    catalog: FLV_IMPORT,
    // Un poste nominal vient d'appliquer ce fichier : les signalements en vigueur sont
    // les siens. Ils ne s'en séparent que sur une ligne « inchangé » ou « échec ».
    catalog_findings_id: FLV_IMPORT.id,
    catalog_motives: [
      { code: 'PREPACKAGED_PRODUCT', value: '', count: 7 },
      { code: 'INTERNAL_CODE_NOT_WEIGHABLE', value: '0490', count: 1 },
    ],
    catalog_source: {
      type: 'local_drop',
      label: 'dépôt local, flv_2.csv dans C:\\ProgramData\\OpenScale\\catalog\\incoming',
    },
    decisions: [],
    roll: {
      printed_count: 120,
      capacity_count: 1000,
      remaining_count: 880,
      level: 'info',
      message: 'environ 880 étiquettes restantes sur un rouleau de 1000.',
      known: true,
    },
    disk: {
      path: 'C:\\ProgramData\\OpenScale',
      free_bytes: 12_300_000_000,
      total_bytes: 128_000_000_000,
      alert_mb: 500,
    },
    unattended_restart: {
      configured: true,
      known: true,
      detail: 'OUI, pour le compte « kiosque »',
      remedy: '',
    },
    printing: {
      fallback_available: false,
      on_fallback: false,
      name: 'Étiqueteuse',
      banner: '',
    },
    // Un poste à jour : la pastille de version ne s'affiche que si un test la demande.
    new_version: '',
    ...overrides,
  }
}
