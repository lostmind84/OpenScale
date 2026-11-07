package printing

// This file is the printer diagnostic of the admin screen, page Matériel: five steps
// a volunteer runs WITHOUT a password — testing the printer changes neither what the
// station sells nor how it weighs (decision 7, ADR-033) — the fifth of which asks THEM
// a question nothing in this binary can answer: did the label come out? (decision 3).
//
// It orders the five steps and writes every sentence a volunteer reads; nothing else
// does. Steps 1 to 3 are the SAME probe this package already makes for the dashboard
// (Service.Interrogate), read exactly once: a second Status or Observe call would open
// the transport a second time, racing the supervisor that holds it the rest of the
// time (ADR-065, Review Focus). Step 4 is Service.SelfTest("label"), which hands back
// the receipt ADR-065 added for exactly this. Step 5 is never measured — AnswerStep
// turns a volunteer's own answer into the step, and nothing here collects it from a
// sensor that does not exist. Its remedy names what the bytes of step 4 actually
// reached — a device node, a socket, the Windows spooler, a directory — because a
// « Non » means a different first thing to check on each (final review, I2).
//
// What classifies an opening failure (absent device, refused permission, connexion
// impossible) lives HERE and nowhere upstream: ports.PrinterStatus.Cause travels
// UNCHANGED from the transport that produced it (ADR-065), precisely so that this is
// the only place that has to recognise a *fs.PathError or a *net.OpError. A Cause this
// package cannot place — a closed driver, anything else — is reported "unknown", never
// "pass": Review Focus is explicit that steps 1 and 2 only succeed on POSITIVE proof.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// StepState is the verdict of one step of the printer diagnostic.
//
// The five values are the five cells of diag.Status.String() (internal/diag),
// rewritten here on purpose: this package imports neither diag nor diag's Status type,
// because the diagnostic of a printer has nothing to do with the device inventory of
// §15 and a shared type would make the two look related. StepUnanswered is the one
// value diag.Status has no use for — it names step 5 before a volunteer has looked.
type StepState string

const (
	// StepPass means the step found positive proof: the device answered, the write
	// succeeded, the bytes went out.
	StepPass StepState = "pass"
	// StepFail means the step found a NAMED, actionable problem: Remedy and Doc are set.
	StepFail StepState = "fail"
	// StepUnknown means the probe could not tell — a silence, an error this package
	// cannot place. It is deliberately NOT a failure (decision 2): a station that
	// cannot ask its printer still prints.
	StepUnknown StepState = "unknown"
	// StepNotApplicable means an earlier step already decided the rest, or this
	// transport has nothing to say about this step (a Windows queue has no file
	// permission to check, a disk directory has nothing a device-level probe can ask).
	// Observed always says which: a bare « sans objet » next to « inconnu » leaves a
	// volunteer guessing whether anything was even tried (spec, « Tranché en
	// relecture », winspool).
	StepNotApplicable StepState = "not_applicable"
	// StepUnanswered means step 5 has not been put to the volunteer yet.
	StepUnanswered StepState = "unanswered"
)

// DiagnosisStep is one of the five steps, written in the words a volunteer reads.
//
// Remedy and Doc are empty except on Fail or Unknown (Global Constraints): a step that
// passed has nothing to remedy, and a step the volunteer skipped reading Observed for
// has nothing to send them to either.
type DiagnosisStep struct {
	Number   int
	Title    string
	State    StepState
	Observed string
	Remedy   string
	Doc      string
}

// Subject names the printer this diagnostic runs against, as the station built it.
//
// It carries no secret and no live handle on purpose: Diagnose only ever touches the
// printer through DiagnosisProbes.Printer, so a Subject can be logged, copied into the
// report a volunteer pastes on the forum, or built in a test with none of the three
// fields answering anything real.
type Subject struct {
	// Transport is printer.options.transport IN SERVICE — one of the four values of
	// domain.Transport*, or "" for a driver that declares none (the preview driver).
	Transport string
	// Device is the queue name, the device node, the network address or the
	// directory the station actually writes to.
	Device string
	// Target is what the transport in service calls itself (Describe()), shown on the
	// copiable report so a forum reply does not have to ask which printer.
	Target string
}

// Prober is what Diagnose asks the printer: the two probes of §8.5/§8.6 that ADR-065
// changed to carry what the diagnostic needs, Cause and the self-test's receipt.
//
// *printing.Service satisfies it. The interface is declared here, on the consumer's
// side (Go idiom, §5.2): the diagnostic needs exactly these two methods and nothing a
// test double has to fake the rest of Service to provide.
type Prober interface {
	Interrogate(ctx context.Context) ports.PrinterStatus
	SelfTest(ctx context.Context, what string) (ports.PrintReceipt, error)
}

var _ Prober = (*Service)(nil)

// DiagnosisProbes is everything Diagnose may ask, beyond the printer itself.
//
// Queues and Stat answer the two questions steps 1 and 2 ask of a transport that has
// no notion of "open" to classify: does this NAMED Windows queue exist, does this
// directory. Both are nil-safe — a diagnostic that cannot enumerate queues or stat a
// path says "unknown", not panic.
type DiagnosisProbes struct {
	Printer Prober
	Queues  func(ctx context.Context) ([]string, error)
	// Stat is os.Stat when nil.
	Stat func(name string) (fs.FileInfo, error)
}

// DiagnosisGuide is the handbook page every Doc of a failing or unknown step points
// into, one anchor per step (handbook/imprimante.md, mkdocs.yml's site_url).
const DiagnosisGuide = "https://lostmind84.github.io/OpenScale/imprimante/"

// anchor builds the Doc of step n.
func anchor(n int) string { return fmt.Sprintf("%s#etape-%d", DiagnosisGuide, n) }

// The five titles, in the words the design maquette validated. They are constants and
// not computed, because the screen, the copiable report and this file must read the
// exact same five sentences.
const (
	title1 = "Le périphérique existe"
	title2 = "Le poste a le droit d'y écrire"
	title3 = "L'imprimante répond quand on lui demande son état"
	title4 = "Une étiquette de test est envoyée en entier"
	title5 = "L'étiquette est-elle sortie ?"
)

// blankSteps builds the five steps, numbered and titled, state and the rest left for
// Diagnose to fill in.
func blankSteps() []DiagnosisStep {
	return []DiagnosisStep{
		{Number: 1, Title: title1},
		{Number: 2, Title: title2},
		{Number: 3, Title: title3},
		{Number: 4, Title: title4},
		{Number: 5, Title: title5},
	}
}

// Diagnose runs the five steps of the printer diagnostic against the printer in
// service (§ design doc), and NEVER opens it more than the probes below ask for:
// exactly one Interrogate when Transport names one of the four transports of
// domain.Transport* (zero for "", the neutral profile's printer), and SelfTest only
// when steps 1 and 2 did not FAIL — an unknown does not block it, because an unknown
// means "this station cannot tell", not "this station cannot print" (decision 2).
func Diagnose(ctx context.Context, s Subject, p DiagnosisProbes) []DiagnosisStep {
	steps := blankSteps()

	var (
		status       ports.PrinterStatus
		interrogated bool
	)
	switch s.Transport {
	case domain.TransportDevfile:
		status = p.Printer.Interrogate(ctx)
		classifyDevfile(steps, s.Device, status)
		interrogated = true
	case domain.TransportTCP:
		status = p.Printer.Interrogate(ctx)
		classifyTCP(steps, s.Device, status)
		interrogated = true
	case domain.TransportWinspool:
		status = p.Printer.Interrogate(ctx)
		classifyWinspool(ctx, steps, s.Device, p.Queues)
		interrogated = true
	case domain.TransportFile:
		status = p.Printer.Interrogate(ctx)
		classifyFile(steps, s.Device, p.Stat)
		interrogated = true
	default:
		// "" is the neutral profile's printer, which declares no transport at all
		// (§8.1): there is nothing to open, so nothing is — zero Interrogate calls,
		// not a probe that would have nothing to ask (decision, Task 3).
		for i := range 3 {
			steps[i].State, steps[i].Observed = StepNotApplicable, observedNoPrinter
		}
	}

	// Step 3 reads the SAME probe steps 1 and 2 already read, but only devfile and tcp
	// read step 1/2 from that probe's Cause: when that Cause could not be placed,
	// step 3 cannot trust it either, hence fillUnknownStep3. winspool and file decide
	// step 1 from Queues/Stat instead, a call this probe never made, so an unknown
	// there says nothing about step 3: classifyStep3 reads the probe's own verdict
	// directly, which answers "not_applicable" for the ErrUnsupported every spooler
	// and file sink returns (spec, "Tranché en relecture", winspool).
	spreadsUnknown := s.Transport == domain.TransportDevfile || s.Transport == domain.TransportTCP
	switch {
	case !interrogated:
		// steps[2] was already set to not_applicable above.
	case steps[0].State == StepFail || steps[1].State == StepFail:
		haltAfterFailure(steps)
		return steps
	case spreadsUnknown && (steps[0].State == StepUnknown || steps[1].State == StepUnknown):
		fillUnknownStep3(&steps[2])
	default:
		classifyStep3(&steps[2], s.Transport, status)
	}

	runSelfTest(ctx, &steps[3], &steps[4], p.Printer)
	if s.Transport == "" {
		// Step 4 wrote a file, not a label: there is nothing for a volunteer to look
		// at, and asking would invite an answer about a printer this run never
		// reached (anti-objectif 3, final review I2).
		steps[4].State, steps[4].Observed = StepNotApplicable, observedNoPrinter
	}
	return steps
}

// observedNoPrinter is what every step a driver without a transport skips says: the
// preview driver (§8.1) renders an image and a PDF, and nothing reaches a printer.
const observedNoPrinter = "le driver d'impression en service écrit l'étiquette dans un fichier " +
	"(aperçu) et n'envoie rien à une imprimante : il n'y a rien à vérifier ici."

// haltAfterFailure closes every step after the first failing one, and says so in each
// of them: a step left « sans objet » without a word reads as one nobody tried.
func haltAfterFailure(steps []DiagnosisStep) {
	first := 0
	for i := range steps {
		if steps[i].State == StepFail {
			first = i
			break
		}
	}
	for i := first + 1; i < len(steps); i++ {
		if steps[i].State == StepNotApplicable && steps[i].Observed != "" {
			continue // a transport that already said why this step does not apply
		}
		steps[i].State = StepNotApplicable
		steps[i].Observed = fmt.Sprintf("non menée : l'étape %d a échoué, c'est elle qu'il faut corriger d'abord.",
			steps[first].Number)
	}
}

// classifyDevfile fills steps 1 and 2 for the devfile transport (§8.4, Linux default).
//
// It is the ONE place this package recognises *fs.PathError, and it only acts on the
// two shapes that mean something specific: ErrNotExist on an "open" says the node is
// absent, ErrPermission on an "open" says it exists but the poste cannot write to it.
// Every other error — including an "open" failure of neither shape, which Review Focus
// is explicit must not read as a success — is reported unknown for BOTH steps: a
// positive verdict needs positive proof (Cause nil, or an error that happened AFTER
// the open, named by any Op other than "open"), never the absence of a recognised
// negative.
func classifyDevfile(steps []DiagnosisStep, device string, status ports.PrinterStatus) {
	cause := status.Cause
	if cause == nil {
		steps[0].State, steps[1].State = StepPass, StepPass
		steps[0].Observed = fmt.Sprintf("« %s » existe et a pu être ouvert.", device)
		steps[1].Observed = "l'ouverture en écriture a réussi."
		return
	}

	var pe *fs.PathError
	if errors.As(cause, &pe) {
		switch {
		case pe.Op == "open" && errors.Is(cause, fs.ErrNotExist):
			steps[0].State = StepFail
			steps[1].State = StepNotApplicable
			steps[0].Observed = fmt.Sprintf("« %s » n'existe pas sur ce poste : %v.", device, cause)
			steps[0].Remedy = "Vérifier que le câble USB est bien branché et que l'imprimante est sous " +
				"tension, puis comparer le nœud réellement présent (« ls /dev/usb/ ») avec celui que " +
				"printer.options.path déclare : la règle udev de l'imprimante n'est pas activée sur ce " +
				"parc, et le nœud change de nom après un rebranchement."
			steps[0].Doc = anchor(1)
			return
		case pe.Op == "open" && errors.Is(cause, fs.ErrPermission):
			steps[0].State = StepPass
			steps[0].Observed = fmt.Sprintf("« %s » existe.", device)
			steps[1].State = StepFail
			steps[1].Observed = fmt.Sprintf("le poste n'a pas le droit d'écrire sur « %s » : %v.", device, cause)
			steps[1].Remedy = "Ajouter le compte du poste au groupe lp, puis redémarrer le service " +
				"openscale (ou le poste) : c'est ce groupe qui donne le droit d'écrire sur le nœud " +
				"d'impression, et le service doit relire ses groupes pour en profiter."
			steps[1].Doc = anchor(2)
			return
		case pe.Op != "open":
			// The node opened; whatever failed happened AFTER — reading the status
			// frame back, typically. That is positive proof for steps 1 and 2; step 3
			// reads the failure itself.
			steps[0].State, steps[1].State = StepPass, StepPass
			steps[0].Observed = fmt.Sprintf("« %s » existe et a pu être ouvert.", device)
			steps[1].Observed = "l'ouverture en écriture a réussi."
			return
		}
	}

	steps[0].State, steps[1].State = StepUnknown, StepUnknown
	steps[0].Observed = fmt.Sprintf("« %s » n'a pas pu être vérifié : %v.", device, cause)
	steps[1].Observed = steps[0].Observed
}

// classifyTCP fills steps 1 and 2 for the tcp transport (bloquant-8, the standby
// printer of the neighbouring station).
//
// Step 2 is ALWAYS not_applicable here, never pass: a socket has no file permission to
// prove, and claiming one would tell a volunteer to go check a thing this transport
// does not have. Only *net.OpError named "dial" is read as "connexion impossible" —
// the same positive-proof rule as classifyDevfile, with "dial" standing in for "open".
func classifyTCP(steps []DiagnosisStep, device string, status ports.PrinterStatus) {
	steps[1].State = StepNotApplicable
	steps[1].Observed = "une connexion réseau n'a pas de droit de fichier à vérifier : l'étape 1 dit " +
		"déjà si le poste peut s'y connecter."

	cause := status.Cause
	if cause == nil {
		steps[0].State = StepPass
		steps[0].Observed = fmt.Sprintf("connexion établie vers %s.", device)
		return
	}

	var oe *net.OpError
	if errors.As(cause, &oe) {
		if oe.Op == "dial" {
			steps[0].State = StepFail
			steps[0].Observed = fmt.Sprintf("connexion impossible vers %s : %v.", device, cause)
			steps[0].Remedy = "Vérifier que l'imprimante est allumée, que son adresse réseau n'a pas " +
				"changé (§8.4 : l'adresse IP fixe est posée sur le panneau de l'imprimante) et que le " +
				"poste et l'imprimante sont sur le même réseau."
			steps[0].Doc = anchor(1)
			return
		}
		steps[0].State = StepPass
		steps[0].Observed = fmt.Sprintf("connexion établie vers %s.", device)
		return
	}

	steps[0].State = StepUnknown
	steps[0].Observed = fmt.Sprintf("%s n'a pas pu être vérifié : %v.", device, cause)
}

// classifyWinspool fills step 1 for the winspool transport from the Windows queue
// list, NOT from the probe's Cause: a spooler is one-way by construction (§8.4), so
// its own Status always answers ErrUnsupported, and that says nothing about whether
// the NAMED queue exists. Step 2 is always not_applicable: the spooler, not this
// station, owns who may write to it.
func classifyWinspool(ctx context.Context, steps []DiagnosisStep, device string, queues func(context.Context) ([]string, error)) {
	steps[1].State = StepNotApplicable
	steps[1].Observed = "c'est le spouleur Windows, pas ce poste, qui décide qui peut écrire dans une " +
		"file : l'étape 4 y écrit réellement."

	if queues == nil {
		steps[0].State = StepUnknown
		steps[0].Observed = "les files d'impression Windows n'ont pas pu être énumérées."
		return
	}

	names, err := queues(ctx)
	switch {
	case err != nil:
		steps[0].State = StepUnknown
		steps[0].Observed = fmt.Sprintf("les files d'impression Windows n'ont pas pu être énumérées : %v.", err)
	case containsName(names, device):
		steps[0].State = StepPass
		steps[0].Observed = fmt.Sprintf("la file « %s » existe parmi les files d'impression Windows.", device)
	default:
		steps[0].State = StepFail
		steps[0].Observed = fmt.Sprintf("la file « %s » n'apparaît pas parmi les files vues : %s.",
			device, strings.Join(names, ", "))
		steps[0].Remedy = "Vérifier le nom de la file dans printer.options.queue, et qu'elle existe " +
			"bien dans les files d'impression Windows du poste (Panneau de configuration → " +
			"Périphériques et imprimantes)."
		steps[0].Doc = anchor(1)
	}
}

// classifyFile fills step 1 for the file transport from the directory it writes to.
// Step 2 is not_applicable, and NOT because the directory is known to be writable: it
// is justified by step 4, which actually writes there and is the one that would say
// so (plan, écart 3) — a weaker claim than "pass", on purpose.
func classifyFile(steps []DiagnosisStep, device string, stat func(string) (fs.FileInfo, error)) {
	steps[1].State = StepNotApplicable
	steps[1].Observed = "l'étape 4 écrit réellement dans ce dossier : c'est elle qui dira si le poste " +
		"peut y écrire."
	if stat == nil {
		stat = os.Stat
	}

	_, err := stat(device)
	switch {
	case err == nil:
		steps[0].State = StepPass
		steps[0].Observed = fmt.Sprintf("le dossier « %s » existe.", device)
	case errors.Is(err, fs.ErrNotExist):
		steps[0].State = StepFail
		steps[0].Observed = fmt.Sprintf("le dossier « %s » n'existe pas sur ce poste : %v.", device, err)
		steps[0].Remedy = "Vérifier printer.options.path : c'est le dossier où le transport « file » " +
			"dépose une copie de chaque étiquette, et il doit exister avant que le poste imprime."
		steps[0].Doc = anchor(1)
	default:
		steps[0].State = StepUnknown
		steps[0].Observed = fmt.Sprintf("« %s » n'a pas pu être vérifié : %v.", device, err)
	}
}

// containsName reports whether name is one of names, byte for byte — a print queue
// name is not a path, and nothing here normalises case or trims it.
func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// fillUnknownStep3 propagates an unresolved step 1 or 2 into step 3: if this package
// cannot say whether the device exists or is writable, it cannot trust what the SAME
// probe said about the device's health either — Interrogate ran once and its Cause was
// already unreadable, which is exactly why steps 1 and 2 landed here.
func fillUnknownStep3(step *DiagnosisStep) {
	step.State = StepUnknown
	step.Observed = "l'état n'a pas pu être vérifié : les étapes précédentes n'ont pas pu dire si " +
		"le périphérique existe ou si le poste peut y écrire."
}

// classifyStep3 reads the SAME probe steps 1 and 2 already read, this time for what it
// says about the printer's health rather than about the transport that carries it.
//
// It never turns a silence into a failure (§8.5, decision 2): PrinterUnknown with no
// Raw and no Cause is the honest "nous ne savons pas", reported unknown and NOT fail —
// neither Observed nor Remedy may say « erreur » here, because nothing failed.
//
// Nor does it explain the silence away. docs/04-parametrage-sato.md measured that a
// SATO at rest in standard protocol DOES answer the ENQ; a silence is therefore a clue
// about the interface's settings, and the remedy is the validated maquette's, which
// says exactly that without claiming to know which setting (final review, I1).
func classifyStep3(step *DiagnosisStep, transport string, status ports.PrinterStatus) {
	switch {
	case errors.Is(status.Cause, ports.ErrUnsupported):
		step.State = StepNotApplicable
		step.Observed = unsupportedStep3(transport)
	case status.Cause == nil && status.Health == ports.PrinterUnknown && len(status.Raw) == 0:
		step.State = StepUnknown
		step.Observed = "aucune réponse à la sonde de statut en 500 ms."
		step.Remedy = "Ce n'est pas une panne en soi : certains réglages de l'interface ne renvoient " +
			"pas d'état. On continue."
	case status.Health == ports.PrinterFaulted:
		step.State = StepFail
		step.Observed = status.Detail
		step.Remedy = "Lire ce que l'imprimante signale (bourrage, capot ouvert, rouleau épuisé) et " +
			"le lever avant de continuer."
		step.Doc = anchor(3)
	default:
		step.State = StepPass
		step.Observed = status.Detail
	}
}

// unsupportedStep3 says why step 3 does not apply to a transport that cannot ask the
// printer anything — which is not a silence, and must not read like one: « sans
// objet » means nothing was asked, « inconnu » that nothing came back.
func unsupportedStep3(transport string) string {
	switch transport {
	case domain.TransportWinspool:
		return "une file Windows ne renvoie pas l'état de l'imprimante : cette étape ne s'applique " +
			"pas ici, ce n'est pas un silence."
	case domain.TransportFile:
		return "un dossier n'a pas d'état à demander : cette étape ne s'applique pas ici."
	}
	return "ce transport ne sait pas demander son état à l'imprimante : cette étape ne s'applique pas ici."
}

// runSelfTest fills step 4 from the `label` self-test's receipt (ADR-065), and sets
// step 5 to "unanswered" unless step 4 failed outright — a self-test that never left
// has nothing for a volunteer to confirm.
func runSelfTest(ctx context.Context, step4, step5 *DiagnosisStep, printer Prober) {
	receipt, err := printer.SelfTest(ctx, string(SelfTestLabel))
	switch {
	case err != nil:
		step4.State = StepFail
		step4.Observed = fmt.Sprintf("l'étiquette de test n'a pas pu être envoyée : %v.", err)
		step4.Remedy = "Relire les étapes 1 à 3 ci-dessus : une étiquette de test échoue pour la " +
			"même raison qu'une étiquette de vente."
		step4.Doc = anchor(4)
		step5.State = StepNotApplicable
		step5.Observed = "non posée : l'étiquette de test n'est pas partie (étape 4), il n'y a rien à " +
			"regarder sortir."
		return
	case receipt.Bytes <= 0:
		step4.State = StepUnknown
		step4.Observed = "l'imprimante a accepté l'envoi, mais aucun octet n'a été compté : " +
			"impossible de dire si l'étiquette de test est vraiment partie."
	default:
		step4.State = StepPass
		step4.Observed = fmt.Sprintf("%s octets sur %s.", groupThousands(receipt.Bytes), groupThousands(receipt.Bytes))
	}
	step5.State = StepUnanswered
}

// AnswerStep turns a volunteer's own answer to step 5 into the step itself: nothing in
// this binary measures whether a label came out (decision 3), so the only verdict that
// can exist here is the one a person gives.
//
// transport is Subject.Transport of the printer in service. It picks the remedy of a
// « Non », because the receipt of step 4 counts bytes handed to the TRANSPORT, and what
// that proves differs on each: a device node and a socket reach the printer, a Windows
// queue only reaches the spooler — where a paused or offline queue is the first thing
// to look at (final review, I2). "" is a driver that sends nothing to a printer: there
// was no label to look at, so whatever the answer, the step does not apply.
func AnswerStep(transport string, printed bool, seen string) DiagnosisStep {
	step := DiagnosisStep{Number: 5, Title: title5}
	if transport == "" {
		step.State, step.Observed = StepNotApplicable, observedNoPrinter
		return step
	}
	if printed {
		step.State = StepPass
		return step
	}
	step.State = StepFail
	step.Observed = seen
	step.Remedy = notPrintedRemedy(transport)
	step.Doc = anchor(5)
	return step
}

// notPrintedRemedy is what to check first when a label did not come out, on the
// transport that carried it.
func notPrintedRemedy(transport string) string {
	switch transport {
	case domain.TransportDevfile:
		return "L'imprimante a tout reçu mais n'a pas fini le travail. À vérifier sur " +
			"l'imprimante : le protocole de l'interface USB, puis la calibration du rouleau."
	case domain.TransportWinspool:
		return "Le travail a été remis en entier à la file Windows, pas directement à " +
			"l'imprimante. Ouvrir la file d'attente de cette imprimante (Panneau de configuration → " +
			"Périphériques et imprimantes) et vérifier qu'elle n'est ni suspendue ni hors connexion ; " +
			"puis, sur l'imprimante, la calibration du rouleau."
	case domain.TransportTCP:
		return "L'imprimante a reçu tout le travail par le réseau mais ne l'a pas fini. À vérifier " +
			"sur l'imprimante : son réglage de protocole, puis la calibration du rouleau."
	case domain.TransportFile:
		return "Le transport « file » n'imprime rien : il dépose chaque étiquette dans le dossier " +
			"de printer.options.path. Vérifier qu'un nouveau fichier y est apparu."
	}
	return "Le travail est parti en entier mais rien n'est sorti. À vérifier sur l'imprimante : " +
		"son réglage de protocole, puis la calibration du rouleau."
}

// groupThousands writes n grouped by thousands with the narrow no-break space of
// French typography (U+202F), the same character a report pasted on the forum must
// carry so it reads « 16 214 » and not a number that wraps mid-group on a phone.
func groupThousands(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var groups []string
	for end := len(digits); end > 0; end -= 3 {
		start := end - 3
		if start < 0 {
			start = 0
		}
		groups = append([]string{digits[start:end]}, groups...)
	}
	return sign + strings.Join(groups, " ")
}
