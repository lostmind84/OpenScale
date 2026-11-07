package printing

// This file is the printer diagnostic of the admin screen, page Matériel: five steps
// a volunteer runs WITHOUT a password, because the fifth asks THEM a question nothing
// in this binary can answer — did the label come out? (decision 3, decision 7).
//
// It orders the five steps and writes every sentence a volunteer reads; nothing else
// does. Steps 1 to 3 are the SAME probe this package already makes for the dashboard
// (Service.Interrogate), read exactly once: a second Status or Observe call would open
// the transport a second time, racing the supervisor that holds it the rest of the
// time (ADR-065, Review Focus). Step 4 is Service.SelfTest("label"), which hands back
// the receipt ADR-065 added for exactly this. Step 5 is never measured — AnswerStep
// turns a volunteer's own answer into the step, and nothing here collects it from a
// sensor that does not exist.
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
		steps[0].State, steps[1].State, steps[2].State = StepNotApplicable, StepNotApplicable, StepNotApplicable
	}

	switch {
	case !interrogated:
		// steps[2] was already set to not_applicable above.
	case steps[0].State == StepFail || steps[1].State == StepFail:
		steps[2].State = StepNotApplicable
		steps[3].State = StepNotApplicable
		steps[4].State = StepNotApplicable
		return steps
	case steps[0].State == StepUnknown || steps[1].State == StepUnknown:
		fillUnknownStep3(&steps[2])
	default:
		classifyStep3(&steps[2], status)
	}

	runSelfTest(ctx, &steps[3], &steps[4], p.Printer)
	return steps
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
	if cause == nil || errors.Is(cause, ports.ErrUnsupported) {
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
			steps[0].Remedy = "Vérifier que le câble USB est bien branché, et que le poste a redémarré " +
				"depuis le branchement : la règle udev de l'installation donne un nom stable au " +
				"nœud d'impression, et ce nom n'apparaît qu'après ce redémarrage."
			steps[0].Doc = anchor(1)
			return
		case pe.Op == "open" && errors.Is(cause, fs.ErrPermission):
			steps[0].State = StepPass
			steps[0].Observed = fmt.Sprintf("« %s » existe.", device)
			steps[1].State = StepFail
			steps[1].Observed = fmt.Sprintf("le poste n'a pas le droit d'écrire sur « %s » : %v.", device, cause)
			steps[1].Remedy = "Ajouter le compte du poste au groupe lp, puis redémarrer la session : " +
				"c'est ce groupe qui donne le droit d'écrire sur le nœud d'impression (§15.3)."
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

	cause := status.Cause
	if cause == nil || errors.Is(cause, ports.ErrUnsupported) {
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
				"changé (§8.4 : l'adresse est fixe, écrite sur une étiquette collée sur l'imprimante) " +
				"et que le poste et l'imprimante sont sur le même réseau."
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
func classifyStep3(step *DiagnosisStep, status ports.PrinterStatus) {
	switch {
	case errors.Is(status.Cause, ports.ErrUnsupported):
		step.State = StepNotApplicable
	case status.Cause == nil && status.Health == ports.PrinterUnknown && len(status.Raw) == 0:
		step.State = StepUnknown
		step.Observed = "l'imprimante n'a rien renvoyé à la sonde de statut en 500 ms : une SATO " +
			"au repos ne dit rien de sa santé, ce silence est courant."
		step.Remedy = "On continue : ce silence n'empêche pas d'imprimer, l'étape suivante le vérifie."
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
func AnswerStep(printed bool, seen string) DiagnosisStep {
	step := DiagnosisStep{Number: 5, Title: title5}
	if printed {
		step.State = StepPass
		return step
	}
	step.State = StepFail
	step.Observed = seen
	step.Remedy = "Lire la page Dépannage : les voyants et les bips de la SATO WS408 y sont " +
		"décrits un par un, avec ce que chacun signifie."
	step.Doc = anchor(5)
	return step
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
