package diag

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// This file carries the five controls about the machine UNDER the station: does it come
// back on its own after a power cut, is its clock believable, does it stay awake, may it
// restart itself, and can its client screen be taken out of the application. They share a
// property none of the other controls has — a station can fail every one of them and still
// look perfectly healthy, right up to the morning the answer is needed.

// elevatedPromptRemedy is the one gesture two of these controls ask for when the READ
// itself failed. Neither the registry of the automatic logon nor the power plan is legible
// without elevation, and « je n'ai pas pu regarder » has the same answer in both cases.
const elevatedPromptRemedy = "Relancez cette commande depuis une invite administrateur."

// --- 3. Unattended restart --------------------------------------------------

// codeUnattendedRestart is ERR-SYS-08, and §14.4 allocates it to exactly this fact.
const codeUnattendedRestart = "ERR-SYS-08"

func (d *Doctor) checkUnattendedRestart(ctx context.Context) Control {
	return UnattendedRestartControl(ctx, d.o.Machine)
}

// UnattendedRestartControl is control 3, and it is EXPORTED because §14.4 puts the same
// verdict on the administration dashboard (bloquant-7).
//
// One function for the two readers. A volunteer reading « redémarrage sans intervention :
// NON CONFIGURÉ » on the screen and whoever reads `doctor.txt` an hour later are looking
// at the same registry key, and two implementations of the same three conditions would
// eventually tell them two different things about it.
func UnattendedRestartControl(ctx context.Context, machine Machine) Control {
	control := Control{ID: ControlUnattendedRestart,
		Checked: "Redémarrage sans intervention configuré (OUI / NON)"}
	state, err := machine.AutoLogon(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "la configuration du redémarrage n'a pas pu être lue : "+err.Error()
		control.Remedy = elevatedPromptRemedy
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "ce système ne dit pas si la session s'ouvre seule"
		control.Remedy = "Faites la recette de §15.5 : redémarrez la machine et vérifiez que le poste " +
			"revient SEUL sur l'écran client, sans que personne tape de mot de passe."
	case !state.Enabled:
		control.Status, control.Code = StatusFail, codeUnattendedRestart
		control.Observed = "NON : après une coupure de courant, ce poste restera sur l'écran de " +
			"connexion et personne dans l'équipe du samedi n'a le mot de passe. " + state.Detail
		control.Remedy = unattendedRestartRemedy()
	case state.Expected != "" && !strings.EqualFold(state.Account, state.Expected):
		control.Status, control.Code = StatusFail, codeUnattendedRestart
		control.Observed = fmt.Sprintf("la session s'ouvre seule pour le compte « %s », alors que le "+
			"kiosque tourne sous « %s » : ce n'est pas la session qui lance l'écran client",
			state.Account, state.Expected)
		control.Remedy = unattendedRestartRemedy()
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("OUI, pour le compte « %s »", or(state.Account, "non nommé"))
	}
	return control
}

// unattendedRestartRemedy is the instruction of bloquant-7, recipe included.
//
// The recipe is part of the remedy and not an extra: the previous plan wrote the registry
// key and told a human to finish the job, which was done once and NEVER VERIFIED AGAIN.
func unattendedRestartRemedy() string {
	if runtime.GOOS == "windows" {
		return "Relancez install.ps1 en administrateur — c'est son étape 3 (§15.2) — puis refaites " +
			"la recette obligatoire de §15.5 : REDÉMARREZ la machine et cochez « le poste est " +
			"revenu seul sur l'écran client »."
	}
	return "Activez les deux unités (systemctl enable openscale.service openscale-kiosk.service), " +
		"puis refaites la recette de §15.5 : redémarrez la machine et vérifiez que le poste " +
		"revient seul sur l'écran client."
}

// --- 14. The system clock ---------------------------------------------------

const codeClockJump = "ERR-SYS-07"

func (d *Doctor) checkSystemClock(loaded loadedConfig) Control {
	control := Control{ID: ControlSystemClock, Checked: "Horloge système cohérente"}
	now := d.o.Clock.Now()
	control.Observed = "heure du poste : " + now.Format(clockLayout)

	built, builtKnown := parseBuildDate(d.o.BuildDate)
	written := loaded.Config.ModifiedAt

	switch {
	case builtKnown && now.Before(built):
		control.Status, control.Code = StatusFail, codeClockJump
		control.Observed += fmt.Sprintf(" — antérieure à la date de compilation du binaire (%s) : "+
			"l'horloge de ce poste est fausse", built.Format(clockLayout))
		control.Remedy = clockRemedy()
	case !written.IsZero() && now.Before(written):
		control.Status, control.Code = StatusFail, codeClockJump
		control.Observed += fmt.Sprintf(" — antérieure à la date d'écriture de la configuration (%s) : "+
			"l'horloge a reculé", written.Format(clockLayout))
		control.Remedy = clockRemedy()
	case !builtKnown:
		control.Status = StatusUnknown
		control.Observed += " — ce binaire ne porte pas sa date de compilation, il n'y a donc rien à comparer"
		control.Remedy = "Rien à faire sur le poste. Ce binaire a été construit sans le Makefile, " +
			"qui injecte la version, le commit et la date : reconstruisez-le avec `make build` " +
			"pour que ce contrôle puisse conclure."
	default:
		control.Status = StatusPass
		control.Observed += fmt.Sprintf(", postérieure à la compilation du binaire (%s)", built.Format(clockLayout))
	}
	return control
}

// clockRemedy is the instruction for a clock that is wrong.
//
// A timestamped journal is only worth anything for reconciliation with the till if the
// hour is right, and no NTP dependency is guaranteed on an offline station (§15.4).
func clockRemedy() string {
	return "Remettez l'heure du poste à la bonne date : un journal de pesées horodaté ne vaut " +
		"rien pour le rapprochement avec la caisse si l'heure est fausse, et le poste n'a " +
		"aucune garantie de serveur de temps puisqu'il est hors ligne. Vérifiez aussi la pile " +
		"de la carte mère : une heure qui revient toujours à la même date après une coupure, " +
		"c'est elle."
}

// parseBuildDate reads the instant the linker injected.
//
// The Makefile injects `git log -1 --format=%cI`, which is RFC 3339. A plain `go build`
// injects "unknown", and saying so is the honest answer — a control that treated an
// unparsable date as the zero instant would report every station's clock as being in the
// future.
func parseBuildDate(value string) (time.Time, bool) {
	if value == "" || value == "unknown" {
		return time.Time{}, false
	}
	built, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return built, true
}

// --- 15. Sleep and USB selective suspend ------------------------------------

func (d *Doctor) checkPowerSettings(ctx context.Context) Control {
	control := Control{ID: ControlPowerSettings,
		Checked: "Veille et suspension USB sélective désactivées"}
	state, err := d.o.Machine.Power(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "les réglages d'énergie n'ont pas pu être lus : "+err.Error()
		control.Remedy = elevatedPromptRemedy
	case !state.Applicable:
		control.Status = StatusNotApplicable
		control.Observed = "la procédure d'installation de ce système n'écrit aucun réglage " +
			"d'énergie (§15.3), il n'y a donc rien à comparer"
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "les réglages d'énergie n'ont pas pu être établis : "+state.Detail
		control.Remedy = "Vérifiez à la main, dans le plan d'alimentation actif, que la mise en " +
			"veille, l'extinction de l'écran et la « suspension sélective USB » sont toutes sur " +
			"« jamais » ou « désactivé » (§15.2, étape 5)."
	case !state.USBSelectiveSuspendDisabled:
		control.Status = StatusFail
		control.Observed = "la suspension USB sélective est ACTIVE. " + state.Detail
		control.Remedy = "C'est la cause de la moitié des « la balance ne répond plus » sur un " +
			"adaptateur USB-série, et elle ne figure dans aucune procédure d'installation " +
			"standard. En administrateur :\n" + usbSuspendCommand() +
			"\nOu relancez install.ps1, dont c'est l'étape 5 (§15.2)."
	case !state.SleepDisabled:
		control.Status = StatusFail
		control.Observed = "la mise en veille ou l'extinction de l'écran est ACTIVE. " + state.Detail
		control.Remedy = "Un poste en libre-service ne doit ni s'endormir ni éteindre son écran. " +
			"En administrateur :\npowercfg /change monitor-timeout-ac 0\n" +
			"powercfg /change standby-timeout-ac 0\npowercfg /change hibernate-timeout-ac 0"
	default:
		control.Status = StatusPass
		control.Observed = "veille, extinction d'écran et suspension USB sélective sont toutes désactivées"
	}
	return control
}

// usbSuspendCommand is the command of §15.2, GUIDs included, quoted from the document.
//
// The two GUIDs are NOT derived and NOT guessed: they are copied from install.ps1 in
// §15.2, which is the only place this project has them from.
func usbSuspendCommand() string {
	return "powercfg /setacvalueindex SCHEME_CURRENT " + usbSubgroupGUID + " " + usbSuspendGUID + " 0\n" +
		"powercfg /setactive SCHEME_CURRENT"
}

// --- 16. The right to restart the machine -----------------------------------

// checkRebootPermission is the sixteenth control: may this station restart the computer?
//
// It exists because the answer is INVISIBLE until somebody needs it. Under Linux the
// service runs as `openscale` and polkit stands between it and the right, so a station
// missing its rule works perfectly — right up to the evening a volunteer is facing a
// frozen kiosk, touches the one button that would have saved them, and watches a
// countdown expire on nothing.
func (d *Doctor) checkRebootPermission(ctx context.Context) Control {
	control := Control{ID: ControlRebootPermission,
		Checked: "Droit de redémarrer l'ordinateur depuis l'écran"}
	state, err := d.o.Machine.RebootPermission(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown,
			"le droit de redémarrer n'a pas pu être établi : "+err.Error()
		control.Remedy = "Vérifiez à la main que /etc/polkit-1/rules.d porte la règle " +
			"49-openscale-reboot.rules, ou relancez « sudo ./install.sh »."
	case !state.Applicable:
		control.Status = StatusNotApplicable
		control.Observed = "ce système ne sait pas redémarrer depuis l'écran (§15.3), " +
			"il n'y a donc aucun droit à vérifier"
	case state.Allowed:
		control.Status, control.Observed = StatusPass, state.Detail
	default:
		control.Status, control.Code = StatusFail, codeRebootRefused
		control.Observed = "NON : " + state.Detail + ". Le bouton « Redémarrer l'ordinateur » " +
			"répondra « accès refusé », et il ne le dira qu'au moment où quelqu'un en a besoin."
		control.Remedy = "Relancez « sudo ./install.sh » depuis deploy/linux : il pose la " +
			"règle polkit qui autorise le compte du poste à redémarrer l'ordinateur, et rien d'autre."
	}
	return control
}

// codeRebootRefused is ERR-SYS-12, and internal/web allocates it to the same fact: the
// machine was asked to restart and said no.
const codeRebootRefused = "ERR-SYS-12"

// --- 17. The client screen cannot leave the application ---------------------

// checkNavigationLock is the seventeenth control, and it is the only one that reports a
// station where EVERYTHING ELSE IS GREEN.
//
// The panne, in full: a right click on the administration screen — the one surface where
// the context menu is deliberately left alive, so that « Copier » works on an error a
// volunteer is reading over the telephone — offers « Rechercher sur le web ». One click,
// and the kiosk window is on a search engine. No address bar, no back button, and the
// browser is perfectly alive: the service answers, the task is running, the window is full
// screen, and the poste sells nothing. It happened on a real station on 31/07/2026.
//
// What it reads is the belt, not the guarantee. The braces are the supervisor's watch over
// the attached client screen, which brings the poste back inside AbsenceGrace whatever the
// browser did with these keys — which is why an unreadable answer here is amber and never
// red.
func (d *Doctor) checkNavigationLock(ctx context.Context) Control {
	control := Control{ID: ControlNavigationLock,
		Checked: "Écran client verrouillé sur l'application"}
	state, err := d.o.Machine.NavigationLock(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown,
			"les stratégies de navigation n'ont pas pu être lues : "+err.Error()
		control.Remedy = navigationLockRemedy
	case !state.Applicable:
		control.Status = StatusNotApplicable
		control.Observed = "sur ce système, l'écran client tourne sous un compositeur " +
			"mono-application (§15.3) et la stratégie du navigateur appartient à " +
			"l'installeur, pas au compte du poste"
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, state.Detail
		control.Remedy = navigationLockRemedy
	case state.Locked:
		control.Status = StatusPass
		control.Observed = "compte « " + state.Account + " » : " + state.Detail +
			" Un clic droit ne peut plus emmener le poste hors de l'application."
	default:
		control.Status, control.Code = StatusFail, codeNavigationOpen
		control.Observed = "compte « " + state.Account + " » : " + state.Detail +
			" Le navigateur peut être emmené hors de l'application — un clic droit, " +
			"« Rechercher sur le web », et il n'y a ni barre d'adresse ni bouton retour " +
			"pour revenir."
		control.Remedy = navigationLockRemedy
	}
	return control
}

// navigationLockRemedy is one gesture, and it is the same one for the three branches that
// carry it: the policies are posed by the kiosk at every logon, so making them exist again
// is making the kiosk start again.
const navigationLockRemedy = "Fermez puis rouvrez la session du poste — le kiosque pose " +
	"ses stratégies à chaque ouverture. Si le contrôle reste rouge, le journal " +
	"kiosk.log dit sur quelle clé il a échoué."

// codeNavigationOpen is ERR-KSK-03: the kiosk window can be taken out of the application.
//
// A code of its own, and the third of the kiosk family: ERR-KSK-02 says « l'affichage
// n'arrive pas à rester ouvert », which is the opposite failure, and reading one for the
// other over the telephone sends a volunteer to look at a browser that crashes when the
// browser is doing fine.
const codeNavigationOpen = "ERR-KSK-03"
