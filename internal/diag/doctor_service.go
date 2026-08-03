package diag

import (
	"context"
	"fmt"
	"runtime"
)

// This file carries the three controls that answer « ce poste est-il debout ? » : the
// service, the scheduled task that opens the client screen, and the address the service
// listens on. They are the three things §15.2 and §15.3 install, and the three a station
// that sells nothing is missing one of.

// --- 1. The service ---------------------------------------------------------

func (d *Doctor) checkService(ctx context.Context) Control {
	control := Control{ID: ControlService, Checked: "Service OpenScale présent et démarré"}
	state, err := d.o.Machine.Service(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "le gestionnaire de services n'a pas répondu : "+err.Error()
		control.Remedy = "Relancez cette commande depuis une invite ADMINISTRATEUR : l'état d'un " +
			"service n'est pas lisible par tout le monde."
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "ce système n'expose pas de gestionnaire de services interrogeable"
		control.Remedy = "Vérifiez à la main que le poste est lancé, puis ouvrez http://" +
			"127.0.0.1:8080/ sur l'écran : si la page s'affiche, le service tourne."
	case !state.Known:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("aucun service « %s » n'est déclaré sur ce poste", state.Name)
		control.Remedy = serviceInstallRemedy()
	case !state.Running:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("le service « %s » est installé mais arrêté (%s)", state.Name, state.Detail)
		// THIS is the sentence the L8 criterion asks for: doctor diagnoses a service that
		// will not start AND SAYS WHY — by naming the four controls that carry the reason.
		control.Remedy = serviceStartRemedy()
	case !state.Automatic:
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("le service « %s » tourne, et son démarrage n'est pas automatique (%s)",
			state.Name, state.Detail)
		control.Remedy = "Après une coupure de courant, ce poste ne redémarrera pas seul. Passez-le " +
			"en démarrage automatique : sc config OpenScale start= auto\n" +
			"Si ce poste est le poste pilote, c'est voulu (§18, lot L9) et il n'y a rien à faire."
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("le service « %s » tourne, démarrage automatique (%s)",
			state.Name, state.Detail)
	}
	return control
}

// serviceInstallRemedy is the instruction for a service the manager has never heard of.
func serviceInstallRemedy() string {
	if runtime.GOOS == "windows" {
		return "Relancez install.ps1 en administrateur (§15.2), ou installez le service à la main :\n" +
			`"C:\Program Files\OpenScale\openscale.exe" service install` + "\n" +
			"puis sc config OpenScale start= auto"
	}
	return "Installez l'unité : systemctl enable --now openscale.service (§15.3)."
}

// serviceStartRemedy names WHERE the reason for a failed start is written.
func serviceStartRemedy() string {
	command := "systemctl start openscale.service"
	logs := "journalctl -u openscale.service -n 50"
	if runtime.GOOS == "windows" {
		command = "sc start OpenScale"
		logs = `le fichier C:\ProgramData\OpenScale\data\logs\openscale.log`
	}
	return "Démarrez-le : " + command + "\nS'il s'arrête aussitôt, la raison est dans l'un des " +
		"contrôles 6, 7, 8 ou 10 ci-dessous — adresse d'écoute déjà prise, configuration " +
		"illisible, base inutilisable, port série absent — et le détail est dans " + logs + "."
}

// --- 2. The kiosk task ------------------------------------------------------

func (d *Doctor) checkKioskTask(ctx context.Context) Control {
	control := Control{ID: ControlKioskTask, Checked: "Tâche du kiosque présente"}
	state, err := d.o.Machine.KioskTask(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "la tâche du kiosque n'a pas pu être interrogée : "+err.Error()
		control.Remedy = "Relancez cette commande depuis une invite ADMINISTRATEUR : le dossier " +
			"des tâches planifiées n'est pas lisible par tout le monde, et « je n'ai pas pu " +
			"regarder » n'est pas « la tâche est absente ».\n" +
			"Tant que ce contrôle est INCONNU, ne réinstallez rien."
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "ce système n'expose pas de planificateur interrogeable"
		control.Remedy = "Vérifiez à la main qu'un navigateur en plein écran s'ouvre à l'ouverture de session."
	case !state.Known:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("aucune tâche « %s » n'est déclarée", state.Name)
		control.Remedy = kioskInstallRemedy()
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("la tâche « %s » est déclarée (%s)", state.Name, or(state.Detail, "état non lu"))
	}
	return control
}

// kioskInstallRemedy is the instruction for a missing kiosk task.
//
// It says what the ABSENCE costs, because a volunteer reading « tâche absente » has no
// way of knowing that the service can be perfectly healthy while the screen stays black.
func kioskInstallRemedy() string {
	if runtime.GOOS == "windows" {
		return "Sans elle, le service tourne mais l'écran client ne s'ouvre jamais. Relancez " +
			"install.ps1 en administrateur (§15.2), ou recréez la tâche :\n" +
			`schtasks /create /tn "OpenScale-Kiosk" /xml openscale-kiosk.xml /f`
	}
	return "Sans elle, le service tourne mais l'écran client ne s'ouvre jamais. Activez l'unité " +
		"du kiosque : systemctl enable --now openscale-kiosk.service (§15.3)."
}

// --- 6. The listening address -----------------------------------------------

// The two codes §13.4 allocates to a listening address that cannot be taken.
const (
	codeAnotherInstance = "ERR-SYS-01"
	codeCannotListen    = "ERR-SYS-02"
)

func (d *Doctor) checkListenAddress(ctx context.Context, loaded loadedConfig) Control {
	control := Control{ID: ControlListenAddress,
		Checked: "Adresse d'écoute libre, ou tenue par ce poste"}
	address := loaded.Config.Network.Listen
	if address == "" {
		control.Status, control.Observed = StatusFail, "aucune adresse d'écoute n'est déclarée (network.listen)"
		control.Remedy = "Renseignez network.listen dans " + or(d.o.ConfigPath, "la configuration") +
			", par exemple 127.0.0.1:8080, puis redémarrez le service."
		return control
	}

	state, err := d.o.Machine.CanListen(ctx, address)
	if err != nil || !state.Determined {
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("l'adresse %s n'a pas pu être testée%s", address, detailSuffix(err))
		control.Remedy = "Vérifiez que network.listen s'écrit hôte:port, par exemple 127.0.0.1:8080."
		return control
	}
	if state.Bindable {
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("%s est libre : le service pourra la prendre", address)
		return control
	}

	// The socket IS the single-instance lock (§13.4). An address that refuses a bind AND
	// answers our own /healthz is held by this very station, which is the nominal case
	// when the service is running — and not a fault to report.
	if live, err := d.liveness(ctx); err == nil && live.IsOpenScale() {
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("%s est tenue par ce poste : /healthz répond (budget %d ms)",
			address, live.BudgetMS)
		return control
	}
	control.Status, control.Code = StatusFail, codeCannotListen
	control.Observed = fmt.Sprintf("%s est déjà prise, et ce qui la tient n'est pas OpenScale : %s",
		address, or(state.Detail, "le bind a été refusé"))
	control.Remedy = "Deux cas, et un seul geste pour les distinguer. Si un autre programme écoute " +
		"sur ce port, changez network.listen — 127.0.0.1:8081 par exemple. Si c'est une " +
		"instance d'OpenScale restée en vie (" + codeAnotherInstance + "), arrêtez le service " +
		"avant d'en lancer un second."
	return control
}
