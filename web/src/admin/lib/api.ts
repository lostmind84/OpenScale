import type {
  ActionDTO,
  ConfigDTO,
  ConfigVersionDTO,
  DetectionDTO,
  FaultDTO,
  FindingDTO,
  HealthDTO,
  ImportDTO,
  PortDTO,
  PrinterDeviceDTO,
  ProblemDTO,
  SessionDTO,
  TechnicalLineDTO,
  WeighingDTO,
} from './dto'

/**
 * Les appels des écrans d'administration, et leurs routes exactes (§14.5).
 *
 * Deux familles, et la frontière entre les deux est la seule chose qui compte ici :
 * les routes de DÉPANNAGE ne demandent pas de mot de passe (ADR-018), les routes qui
 * ÉCRIVENT LA CONFIGURATION en demandent un. Le mot de passe voyage dans un cookie
 * `HttpOnly` que ce fichier ne peut pas lire — d'où {@link AdminError} et son `status` :
 * un 401 n'est pas une panne, c'est une session à rouvrir.
 */

/** Le refus d'une route, avec la phrase française que le service a écrite. */
export class AdminError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly code = '',
    /** Les 45 contrôles de §11.3 quand le refus en porte : TOUS, jamais le premier. */
    readonly faults: FaultDTO[] = [],
  ) {
    super(message)
    this.name = 'AdminError'
  }

  /** Vrai quand la session manque ou a expiré : l'écran redemande le mot de passe. */
  get needsPassword(): boolean {
    return this.status === 401
  }
}

// --- Le tableau de bord, sans mot de passe (ADR-018) ------------------------

/** Lit le tableau de bord de §14.4. C'est la seule route que la page bénévole lit. */
export function fetchHealth(): Promise<HealthDTO> {
  return getJSON<HealthDTO>('/admin/api/health')
}

// --- Les neuf boutons de la page Dépannage, sans mot de passe ---------------

/** « Réimprimer la dernière ». Aucune clé d'idempotence : deux appuis = deux étiquettes. */
export function reprintLast(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/troubleshooting/reprint', {})
}

/** « Recharger le catalogue » : la veille refait le MÊME poll (§14.4). */
export function reloadCatalog(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/troubleshooting/reload-catalog', {})
}

/** « Basculer en saisie manuelle ». Un état du poste, jamais une configuration écrite. */
export function setManualEntry(on: boolean): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/troubleshooting/manual-entry', { on })
}

/** « J'ai changé le rouleau » : le seul geste qui dit quelque chose de vrai du papier. */
export function rollChanged(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/troubleshooting/roll-changed', {})
}

/** « Imprimer sur l'imprimante du poste N », et le retour (§8.4, bloquant-8). */
export function useFallbackPrinter(on: boolean): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/troubleshooting/fallback-printer', { on })
}

/** Ce que « Tester la balance » répond : la cadence OBSERVÉE, pas un nouveau sondage. */
export interface ScaleTestDTO {
  connected: boolean
  median_ms: number
  observations_count: number
  provisional: boolean
  too_slow: boolean
  last_weight_g: number
  age_ms: number
  message: string
}

/** « Tester la balance ». */
export function testScale(): Promise<ScaleTestDTO> {
  return postJSON<ScaleTestDTO>('/admin/api/troubleshooting/test-scale', {})
}

/** Ce que « Tester l'imprimante » répond : ce que le superviseur a vu en dernier. */
export interface PrinterTestDTO {
  health: string
  detail: string
  pending_jobs_count: number
  observed_at: string
  message: string
}

/** « Tester l'imprimante ». */
export function testPrinter(): Promise<PrinterTestDTO> {
  return postJSON<PrinterTestDTO>('/admin/api/troubleshooting/test-printer', {})
}

/** « Imprimer une étiquette de test ». */
export function testLabel(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/troubleshooting/test-label', {})
}

/**
 * « Importer un catalogue » : le CSV glissé sur l'écran (A4, ADR-011).
 *
 * Le fichier est écrit là où la veille ordinaire le trouvera : même parseur, même
 * qualification, même acquittement qu'un fichier déposé par le producteur.
 */
export async function importCatalog(file: File): Promise<ImportDTO> {
  const form = new FormData()
  form.append('file', file, file.name)
  const response = await fetch('/admin/api/catalog/import', { method: 'POST', body: form })
  return read<ImportDTO>(response, 'POST /admin/api/catalog/import')
}

/** L'adresse du fichier de diagnostic. Un seul bouton, sans mot de passe (§15.4). */
export const DIAGNOSTIC_URL = '/admin/api/diagnostic.zip'

// --- Les trois routes de session --------------------------------------------

/** Ouvre une session d'administration. 5 essais par minute, puis 5 minutes (§14.4). */
export function openSession(password: string): Promise<SessionDTO> {
  return postJSON<SessionDTO>('/admin/api/session', { password })
}

/** Ferme la session : sur un poste en kiosque, il n'y a pas de navigateur à fermer. */
export async function closeSession(): Promise<void> {
  await fetch('/admin/api/session', { method: 'DELETE' })
}

/** Réinitialise le mot de passe avec le code de secours de la fiche d'installation. */
export function recoverSession(code: string, password: string): Promise<SessionDTO> {
  return postJSON<SessionDTO>('/admin/api/session/recovery', { code, password })
}

// --- Les pages expert, derrière le mot de passe -----------------------------

/** Lit la configuration en service, sans ses deux empreintes. */
export function fetchConfig(): Promise<ConfigDTO> {
  return getJSON<ConfigDTO>('/admin/api/config')
}

/** Écrit la configuration : les cinq étapes de §11.4, dans l'ordre. */
export function saveConfig(config: Record<string, unknown>): Promise<ConfigDTO> {
  return sendJSON<ConfigDTO>('PUT', '/admin/api/config', config)
}

/** Confirme la configuration en service et arrête le compte à rebours. */
export function confirmConfig(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/config/confirm', {})
}

/** Les cinq versions restaurables. */
export async function fetchVersions(): Promise<ConfigVersionDTO[]> {
  const body = await getJSON<{ versions: ConfigVersionDTO[] }>('/admin/api/config/versions')
  return body.versions
}

/** Remet une des cinq versions en service, par le même chemin qu'un enregistrement. */
export function restoreVersion(version: number): Promise<ConfigDTO> {
  return postJSON<ConfigDTO>('/admin/api/config/restore', { version })
}

/** Les ports série détectés, avec leur description USB. */
export async function fetchPorts(): Promise<PortDTO[]> {
  const body = await getJSON<{ ports: PortDTO[] }>('/admin/api/ports')
  return body.ports
}

/** « Lister les files » d'impression. */
export async function fetchPrinters(): Promise<PrinterDeviceDTO[]> {
  const body = await getJSON<{ printers: PrinterDeviceDTO[] }>('/admin/api/printers')
  return body.printers
}

/** « Rechercher l'imprimante » : le balayage réseau, qui prend des secondes. */
export async function discoverPrinters(): Promise<PrinterDeviceDTO[]> {
  const body = await postJSON<{ printers: PrinterDeviceDTO[] }>(
    '/admin/api/printers/discover',
    {},
  )
  return body.printers
}

/**
 * « Détecter automatiquement » : ouvre un port, applique les parseurs, dit ce qui a
 * répondu — « COM8 : 12 trames valides, GRAM XFOC ».
 *
 * C'est la détection qui répond à « y a-t-il une balance ? », pas l'exploitant (§14.4).
 */
export function detectScale(port: string): Promise<DetectionDTO> {
  return postJSON<DetectionDTO>('/admin/api/scale/detect', { port })
}

/** Le visualiseur des dernières trames brutes, toujours actif (§14.4). */
export async function captureFrames(port: string, seconds: number): Promise<string[]> {
  const body = await postJSON<{ frames: string[] }>('/admin/api/scale/capture', {
    port,
    seconds,
  })
  return body.frames
}

/** Les trois auto-tests d'impression de §8.6. */
export function printerSelfTest(what: 'label' | 'alignment' | 'ruler'): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/printer/test?what=' + what, {})
}

/**
 * L'adresse de l'aperçu d'étiquette.
 *
 * L'aperçu est un PNG rendu par le MÊME moteur que l'impression (A2) : c'est ce qui rend
 * le réglage du décalage possible sans imprimer, parce que le décalage est cuit dans le
 * bitmap et se VOIT. Le paramètre `t` n'est pas dans le contrat : il force le navigateur
 * à redemander l'image à chaque frappe, ce que `Cache-Control: no-store` obtient déjà du
 * cache HTTP mais pas d'une balise `<img>` dont l'URL n'a pas changé.
 */
export function previewURL(template: string, demo: boolean, dual: boolean, nonce: number): string {
  const query = new URLSearchParams({ template, demo: demo ? '1' : '0', dual: dual ? '1' : '0' })
  query.set('t', String(nonce))
  return '/admin/api/label/preview.png?' + query.toString()
}

/** Le journal des pesées : les 200 dernières, filtrées. */
export async function fetchJournal(filters: Record<string, string>): Promise<WeighingDTO[]> {
  const query = new URLSearchParams(filters)
  const body = await getJSON<{ weighings: WeighingDTO[] }>('/admin/api/journal?' + query)
  return body.weighings
}

/** L'adresse de l'export CSV du journal. */
export function journalCSVURL(filters: Record<string, string>): string {
  return '/admin/api/journal/export.csv?' + new URLSearchParams(filters).toString()
}

/** Le journal technique. */
export async function fetchTechnical(filters: Record<string, string>): Promise<TechnicalLineDTO[]> {
  const query = new URLSearchParams(filters)
  const body = await getJSON<{ entries: TechnicalLineDTO[] }>('/admin/api/technical?' + query)
  return body.entries
}

/** L'historique des imports, et les signalements de celui que `id` désigne. */
export function fetchImports(
  id?: number,
): Promise<{ imports: ImportDTO[]; findings: FindingDTO[] }> {
  const query = id === undefined ? '' : '?id=' + String(id)
  return getJSON<{ imports: ImportDTO[]; findings: FindingDTO[] }>('/admin/api/imports' + query)
}

/** « Recharger le catalogue », porte expert de la même action. */
export function reloadCatalogAsExpert(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/catalog/reload', {})
}

/** « Oublier la quarantaine » : le prochain fichier sera relu (§10.5). */
export function forgetQuarantine(): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/catalog/forget-quarantine', {})
}

/**
 * La seule route de la seule table de décisions humaines (§14.5).
 *
 * « Ne plus proposer ce produit » et la dérogation « ce produit peut peser moins de
 * 10 g » sont deux colonnes de `local_decisions`, pas deux mécanismes.
 */
export function saveDecision(
  productID: string,
  decision: { offered: boolean; min_weight_g: number | null; reason: string },
): Promise<ActionDTO> {
  return postJSON<ActionDTO>(
    '/admin/api/products/' + encodeURIComponent(productID) + '/decision',
    decision,
  )
}

/** « Rejouer cette trame » : ce qui fait d'un refus inexpliqué un test permanent. */
export function replayFrame(frame: string): Promise<ActionDTO> {
  return postJSON<ActionDTO>('/admin/api/replay', { frame })
}

// --- Le peu de plomberie commune -------------------------------------------

/** Lit une route JSON. */
async function getJSON<T>(route: string): Promise<T> {
  const response = await fetch(route, { headers: { accept: 'application/json' } })
  return read<T>(response, 'GET ' + route)
}

/** Poste un corps JSON. */
function postJSON<T>(route: string, body: unknown): Promise<T> {
  return sendJSON<T>('POST', route, body)
}

/** Envoie un corps JSON par la méthode donnée. */
async function sendJSON<T>(method: string, route: string, body: unknown): Promise<T> {
  const response = await fetch(route, {
    method,
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return read<T>(response, method + ' ' + route)
}

/**
 * Lit une réponse, et transforme un refus en {@link AdminError} PORTEUSE DE SA PHRASE.
 *
 * Le message vient du service et jamais d'ici : c'est lui qui sait pourquoi il a
 * refusé, il l'écrit en français, et un front qui réécrirait « une erreur est survenue »
 * par-dessus enlèverait au bénévole la seule chose qui lui dise quoi faire.
 */
async function read<T>(response: Response, what: string): Promise<T> {
  const raw = await response.text()
  if (!response.ok) {
    const problem = parseProblem(raw)
    throw new AdminError(
      response.status,
      refusalOf(raw, what, response.status),
      problem?.code ?? '',
      problem?.faults ?? [],
    )
  }
  if (raw === '') return undefined as T
  return JSON.parse(raw) as T
}

/** La phrase d'un refus, celle du service quand il en a écrit une. */
function refusalOf(raw: string, what: string, status: number): string {
  const problem = parseProblem(raw)
  if (problem?.message !== undefined && problem.message !== '') return problem.message
  return `${what} a répondu ${String(status)}.`
}

/** Lit un corps de refus, sans jamais échouer sur autre chose que du JSON. */
function parseProblem(raw: string): ProblemDTO | null {
  try {
    return JSON.parse(raw) as ProblemDTO
  } catch {
    return null
  }
}
