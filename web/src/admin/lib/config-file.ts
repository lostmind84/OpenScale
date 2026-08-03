import { AdminError } from './api'
import type { FaultDTO, ProblemDTO } from './dto'

/**
 * The two exchanges of a configuration FILE, and the download that follows one of them.
 *
 * They live apart from `lib/api.ts` because they are the only calls of the contract that
 * do not read a body and parse it as JSON: one answers a document somebody is about to
 * save, the other sends one somebody just picked. Doing to those what the rest of the
 * module does to an answer is exactly what must not happen.
 */

/** One exported configuration, and the name it is saved under. */
export interface ExportedFile {
  name: string
  blob: Blob
}

/** What `POST /admin/api/config/import` answers of the file it was given. */
export interface InspectedConfig {
  config: Record<string, unknown>
  faults: FaultDTO[]
}

/**
 * Fetches one export, and turns a refusal into what {@link Admin.protect} answers.
 *
 * `GET /admin/api/config/export` is the one read the station keeps behind the password,
 * because it is the one payload that still carries the password hash (§11.5). Two bare
 * `<a download>` used to fetch it, and an anchor cannot see a refusal: on an expired
 * session the browser saved a file named like an export and holding « Session expirée ».
 *
 * @param station - the number of this station, which names the file.
 * @param withHardware - what the `hardware` parameter of §11.5 selects.
 */
export async function readExport(
  station: number,
  withHardware: boolean,
): Promise<ExportedFile> {
  const route = `/admin/api/config/export?hardware=${withHardware ? '1' : '0'}`
  const response = await fetch(route, { headers: { accept: 'application/json' } })
  if (!response.ok) {
    throw new AdminError(response.status, refusalOf(await response.text(), 'L’export'))
  }
  return { name: exportName(station, withHardware), blob: await response.blob() }
}

/**
 * Has THE STATION read the file, and turns a refusal into what `protect` answers.
 *
 * `changed_blocks` travels in the same body and is deliberately not read: the twelve
 * block names are English tokens of `internal/web/config.go`, and the field-by-field diff
 * §14.4 asks for says strictly more than « le bloc printer a changé » — which does not
 * tell whether it is the print queue or the darkness.
 *
 * @param contents - the parsed file.
 */
export async function submitCandidate(
  contents: Record<string, unknown>,
): Promise<InspectedConfig> {
  const response = await fetch('/admin/api/config/import', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(contents),
  })
  const raw = await response.text()
  if (!response.ok) throw new AdminError(response.status, refusalOf(raw, 'L’import'))
  return JSON.parse(raw) as InspectedConfig
}

/**
 * The name an export is saved under, and why the screen chooses it.
 *
 * The station names every export `config-export.json`, so the clone of §11.5 — one
 * operator, one file, three other stations — ends with four identically named files in
 * one folder, two of which do not carry the hardware and must not be applied as if they
 * did. §11.5 names the file after the station it came from and the day it was taken.
 *
 * @param station - the number of this station.
 * @param withHardware - whether this is the complete export.
 */
export function exportName(station: number, withHardware: boolean): string {
  const variant = withHardware ? '' : '-sans-materiel'
  return `config-poste${String(station)}${variant}-${isoDay()}.json`
}

/** Today as `2026-07-27`: the one date form that sorts right in a folder listing. */
function isoDay(): string {
  const now = new Date()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${String(now.getFullYear())}-${month}-${day}`
}

/**
 * Hands one file to the browser's own download.
 *
 * The anchor is created, clicked and dropped: it exists for the length of one gesture and
 * never sits in the page — a permanent `<a download>` is precisely what could not see the
 * station refuse.
 *
 * The object URL is released ON THE NEXT TURN of the loop, never on this one. A click
 * only QUEUES the download; a browser that reads the address afterwards finds nothing and
 * cancels it, and the sentence on screen would have announced an export nobody received.
 * One turn is enough, and it is what keeps the URL from leaking either.
 *
 * @param name - the name the file is saved under.
 * @param blob - the bytes the station answered.
 */
export function handToBrowser(name: string, blob: Blob): void {
  const address = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = address
  link.download = name
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => {
    URL.revokeObjectURL(address)
  }, 0)
}

/**
 * The French sentence of a refusal: the station's own, when it wrote one.
 *
 * @param raw - the body of the refused answer.
 * @param what - the act, for the sentence of last resort.
 */
function refusalOf(raw: string, what: string): string {
  try {
    const problem = JSON.parse(raw) as ProblemDTO | null
    if (typeof problem?.message === 'string' && problem.message !== '') return problem.message
  } catch {
    // The station answered something that is not a problem document.
  }
  return `${what} a été refusé par le poste.`
}
