package diag

import (
	"context"
	"fmt"
	"strings"

	"openscale/internal/domain"
)

// This file carries the three controls about the devices a station weighs and prints
// with: the serial port, the print queue and the cadence the scale is actually observed
// at. Two of them ask the RUNNING SERVICE rather than the host — a queue « installée pour
// l'utilisateur » is invisible from the service while being perfectly visible from here,
// and a cadence is a measurement nothing on disk carries (important-11, §15.4).

// --- 10. The serial port ----------------------------------------------------

const codePortUnavailable = "ERR-SCL-03"

// optionPort is the key a SERIAL scale.options carries the port name under (§11.2).
//
// It is a literal and it is allowed to be one, now that the control runs only for a
// protocol that declares itself on a serial port: `port` is the key
// internal/scale/serial declares in its own option schema, and it exists exactly when
// this control applies. What it may no longer do — and did — is assume that every scale
// of every protocol is reached through it.
const optionPort = "port"

// scaleEndpoint reports what kind of access point the protocol id names is reached on,
// as the driver itself declared it, and whether this binary knows that protocol at all.
//
// An UNKNOWN protocol is not answered with a guess: control 8 already reports a
// scale.type no driver of this binary carries, and this control then does what it always
// did rather than adding a second, differently worded verdict on the same fault.
func (d *Doctor) scaleEndpoint(id string) (string, bool) {
	for _, descriptor := range d.o.Registries.Scales {
		if descriptor.ID == id {
			return descriptor.Endpoint, true
		}
	}
	return "", false
}

func (d *Doctor) checkSerialPort(ctx context.Context, loaded loadedConfig) Control {
	control := Control{ID: ControlSerialPort, Checked: "Port série présent et ouvrable"}
	if !loaded.Config.Scale.Present {
		// The explicit declaration of §11.2, which turns the light OFF instead of leaving
		// it red. It is not a fault and must not be reported as one.
		control.Status = StatusNotApplicable
		control.Observed = "ce poste est déclaré sans balance (scale.present = false) : la saisie du " +
			"poids à la main est le mode nominal"
		return control
	}
	if endpoint, known := d.scaleEndpoint(loaded.Config.Scale.Type); known &&
		endpoint != domain.EndpointSerialPort {
		// A protocol that is not reached through a serial port has no scale.options.port,
		// and this control would report a missing key as a fault on a station that is
		// perfectly configured. The light goes OFF, like the one of a station with no
		// scale, and says why.
		control.Status = StatusNotApplicable
		control.Observed = fmt.Sprintf("le protocole %s ne passe pas par un port série : "+
			"il n'y a pas de scale.options.port à vérifier sur ce poste", loaded.Config.Scale.Type)
		return control
	}
	declared, _ := loaded.Config.Scale.Options.Text(optionPort)
	if declared == "" {
		control.Status, control.Code = StatusFail, codePortUnavailable
		control.Observed = "aucun port n'est déclaré (scale.options.port) alors que ce poste annonce une balance"
		control.Remedy = "Ouvrez la page Matériel et lancez « Détecter automatiquement » : " +
			"la détection ouvre chaque port, applique les parseurs et annonce celui qui répond. " +
			"Ou déclarez scale.present = false si ce poste n'a réellement pas de balance."
		return control
	}

	list, err := d.o.Machine.SerialPorts(ctx)
	if err != nil {
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("le port %s est déclaré, et les ports du poste n'ont pas pu "+
			"être énumérés : %v", declared, err)
		control.Remedy = "Relancez la commande depuis une invite administrateur, puis vérifiez le " +
			"câble de la balance."
		return control
	}
	if !containsPort(list, declared) {
		control.Status, control.Code = StatusFail, codePortUnavailable
		control.Observed = fmt.Sprintf("le port %s est déclaré et n'existe pas sur ce poste. %s",
			declared, portListSentence(list))
		control.Remedy = "Rebranchez le câble de la balance, puis relancez la commande. Si le port a " +
			"changé de nom — c'est ce qui arrive après un rebranchement — corrigez " +
			"scale.options.port, ou lancez « Détecter automatiquement » depuis Réglages " +
			"avancés → Matériel. Vérifiez aussi le contrôle 15 : la suspension USB sélective " +
			"fait disparaître un adaptateur USB-série."
		return control
	}

	if err := d.o.Machine.OpenSerialPort(ctx, declared); err != nil {
		// A port that is enumerated but refuses to open is EXCLUSIVE and held — which is
		// what a running service looks like from here, and a success rather than a fault.
		if live, liveErr := d.liveness(ctx); liveErr == nil && live.IsOpenScale() {
			control.Status = StatusPass
			control.Observed = fmt.Sprintf("le port %s existe et il est tenu par le service en cours : "+
				"un port série est exclusif, c'est le résultat attendu quand le poste tourne", declared)
			return control
		}
		control.Status, control.Code = StatusFail, codePortUnavailable
		control.Observed = fmt.Sprintf("le port %s existe et ne s'ouvre pas : %v", declared, err)
		control.Remedy = "Un port série est exclusif. Fermez ce qui le tient — un autre programme, " +
			"une fenêtre de terminal série restée ouverte — puis relancez la commande. Si " +
			"personne ne le tient, c'est un droit qui manque : le compte du service doit " +
			"appartenir au groupe dialout sous Linux (§15.3)."
		return control
	}
	control.Status = StatusPass
	control.Observed = fmt.Sprintf("le port %s existe et s'ouvre", declared)
	return control
}

// containsPort reports whether the declared name was enumerated.
//
// The comparison is case-insensitive because Windows spells the same port COM8 and com8,
// and a control that refused com8 would send somebody looking for a cable that is plugged
// in.
func containsPort(list []PortInfo, name string) bool {
	for _, port := range list {
		if strings.EqualFold(port.Name, name) {
			return true
		}
	}
	return false
}

// portListSentence names what WAS enumerated, which is the half of the remedy a volunteer
// can act on.
func portListSentence(list []PortInfo) string {
	if len(list) == 0 {
		return "Aucun port série n'est visible sur ce poste."
	}
	names := make([]string, 0, len(list))
	for _, port := range list {
		names = append(names, port.String())
	}
	return "Ports visibles : " + strings.Join(names, " · ") + "."
}

// --- 11. The print queue, from the service's context ------------------------

const codePrinterUnreachable = "ERR-PRN-01"

func (d *Doctor) checkPrintQueue(ctx context.Context, loaded loadedConfig, health Health, healthErr error) Control {
	control := Control{ID: ControlPrintQueue,
		Checked: "File d'impression visible depuis le contexte du service"}
	if healthErr != nil {
		control.Status = StatusUnknown
		control.Observed = "le service ne répond pas, et lui seul peut répondre : une file " +
			"« installée pour l'utilisateur » est invisible du service tout en étant parfaitement " +
			"visible d'ici. " + d.localQueues(ctx)
		control.Remedy = "Démarrez le service (contrôle 1), puis relancez openscale doctor. Ce " +
			"contrôle interroge le service exprès : le tester avec les droits de l'opérateur " +
			"répondrait à une autre question (§15.2, important-11)."
		return control
	}

	configured, _ := loaded.Config.Printer.Options.Text("queue")
	switch health.State.Printer.Health {
	case "faulted":
		control.Status, control.Code = StatusFail, codePrinterUnreachable
		control.Observed = fmt.Sprintf("le service ne peut pas imprimer : %s. %s",
			or(health.State.Printer.Detail, "aucun détail"), configuredQueueSentence(configured))
		control.Remedy = "Sous Windows, la file doit être installée en imprimante LOCALE MACHINE : " +
			"une file « installée pour l'utilisateur » est invisible depuis le service, et c'est " +
			"la panne la plus fréquente à l'installation (§15.2). " + d.localQueues(ctx) +
			"\nEn attendant, l'écran de dépannage propose « Imprimer sur l'imprimante du poste N »."
	case "consumable":
		control.Status = StatusWarn
		control.Observed = "le service imprime, et le rouleau arrive en fin de vie : " +
			or(health.State.Printer.Detail, "aucun détail")
		control.Remedy = "Changez le rouleau, puis touchez « J'ai changé le rouleau » sur l'écran " +
			"de dépannage — c'est ce bouton qui remet le compteur à zéro (§8.5)."
	case "unknown":
		control.Status = StatusPass
		control.Observed = "le service atteint l'imprimante ; celle-ci ne sait pas dire ce qu'elle a " +
			"— les octets partent, rien ne revient. C'est la réponse honnête d'un transport " +
			"unidirectionnel, pas une panne. " + configuredQueueSentence(configured)
	case "ready":
		control.Status = StatusPass
		control.Observed = "le service voit l'imprimante et elle n'a rien à signaler. " +
			configuredQueueSentence(configured)
	default:
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("le service annonce un état d'imprimante que cette version ne "+
			"connaît pas : %q", health.State.Printer.Health)
		control.Remedy = "Les deux binaires ne sont pas de la même version. Mettez ce poste à jour, " +
			"puis relancez la commande."
	}
	return control
}

// configuredQueueSentence names what the configuration asks for.
func configuredQueueSentence(queue string) string {
	if queue == "" {
		return "Aucune file n'est nommée dans printer.options.queue."
	}
	return fmt.Sprintf("File configurée : « %s ».", queue)
}

// localQueues names the queues visible from THIS process, labelled as such.
//
// The label is not decoration: presenting the operator's list as the service's viewpoint
// is the exact mistake important-11 is about, and the list is only ever useful as the
// second half of a remedy.
func (d *Doctor) localQueues(ctx context.Context) string {
	list, err := d.o.Machine.PrintQueues(ctx)
	if err != nil {
		return "Les files visibles depuis cette session n'ont pas pu être énumérées : " + err.Error() + "."
	}
	if len(list) == 0 {
		return "Aucune file d'impression n'est visible depuis cette session."
	}
	names := make([]string, 0, len(list))
	for _, queue := range list {
		name := queue.Name
		if queue.Default {
			name += " (par défaut)"
		}
		names = append(names, name)
	}
	return "Files visibles depuis cette session — pas depuis celle du service : " +
		strings.Join(names, " · ") + "."
}

// --- 12. The observed scale cadence -----------------------------------------

const codeScaleLost = "ERR-SCL-02"

func (d *Doctor) checkScaleRate(loaded loadedConfig, health Health, healthErr error) Control {
	control := Control{ID: ControlScaleRate, Checked: "Cadence de la balance réellement observée"}
	if !loaded.Config.Scale.Present {
		control.Status = StatusNotApplicable
		control.Observed = "ce poste est déclaré sans balance (scale.present = false)"
		return control
	}
	if healthErr != nil {
		control.Status = StatusUnknown
		control.Observed = "le service ne répond pas : la cadence est ce que le poste a MESURÉ sur " +
			"les soixante-quatre derniers intervalles, elle ne se déduit d'aucun fichier"
		control.Remedy = "Démarrez le service (contrôle 1), laissez-le recevoir quelques trames, " +
			"puis relancez openscale doctor."
		return control
	}

	scale := health.State.Scale
	switch {
	case !scale.Connected:
		control.Status, control.Code = StatusFail, codeScaleLost
		control.Observed = "le service n'a plus de balance : le port était ouvert et il s'est tu"
		control.Remedy = "Vérifiez le câble et l'alimentation de la balance, puis rebranchez : le " +
			"poste revient à l'état nominal seul. En attendant, l'écran client propose la saisie " +
			"du poids à la main."
	case scale.Observations == 0:
		control.Status = StatusUnknown
		control.Observed = "le service tient le port et n'a encore reçu aucune trame"
		control.Remedy = "Posez quelque chose sur le plateau, attendez trois secondes, puis " +
			"relancez la commande. Si rien n'arrive, vérifiez le débit et la parité déclarés " +
			"dans scale.options contre ceux affichés sur la balance."
	case scale.TooSlow:
		// The alert condition itself is computed by the station, once, and read here:
		// expiry_factor × median above the ceiling (§6.5, ADR-005). Two implementations of
		// one rule is how the two of them come to disagree.
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("la balance émet une mesure toutes les %d ms, et le poids est "+
			"considéré périmé AVANT l'arrivée de la mesure suivante", scale.MedianMS)
		control.Remedy = "Le poids s'affichera puis disparaîtra sans raison visible. Vérifiez le " +
			"câble, puis la cadence d'émission réglée sur la balance elle-même : c'est un " +
			"réglage de l'appareil, pas du poste."
	case scale.Provisional:
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("cadence PROVISOIRE de %d ms sur %d intervalle(s) : moins de "+
			"huit ont été observés, la valeur affichée est celle que le driver déclare, pas une mesure",
			scale.MedianMS, scale.Observations)
		control.Remedy = "Laissez le poste recevoir des trames quelques secondes, puis relancez la " +
			"commande : le chiffre deviendra une mesure."
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("une mesure toutes les %d ms, médiane mesurée sur %d intervalles",
			scale.MedianMS, scale.Observations)
	}
	return control
}
