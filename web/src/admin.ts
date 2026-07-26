import { mountAdmin } from './admin/mount'
import './app.css'

/**
 * Entry point of the administration document.
 *
 * It exists so that the administration is also reachable as its own page, which
 * is what makes its weight measurable separately from the client screen. The way
 * in that a volunteer actually uses is the three second press of §14.3, which
 * loads {@link mountAdmin} into the window that is already open.
 */
mountAdmin(document.getElementById('app') as HTMLElement)
