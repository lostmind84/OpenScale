package printing

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// fakeProber is the Prober Diagnose is tested against: it counts how many times each
// method ran, which is the proof Review Focus asks for — Interrogate at most once per
// Diagnose call, SelfTest only when steps 1 and 2 did not fail.
type fakeProber struct {
	status           ports.PrinterStatus
	interrogateCalls int

	selfTestReceipt ports.PrintReceipt
	selfTestErr     error
	selfTestCalls   int
}

func (f *fakeProber) Interrogate(context.Context) ports.PrinterStatus {
	f.interrogateCalls++
	return f.status
}

func (f *fakeProber) SelfTest(context.Context, string) (ports.PrintReceipt, error) {
	f.selfTestCalls++
	return f.selfTestReceipt, f.selfTestErr
}

// anchorsOfHandbook reads handbook/imprimante.md and returns the set of attr_list
// anchors it declares — {#etape-1} and so on — so that every non-empty Doc a test case
// produces can be checked against a real page, not an assumption about one.
func anchorsOfHandbook(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("../../handbook/imprimante.md")
	if err != nil {
		t.Fatalf("handbook/imprimante.md : %v", err)
	}
	anchors := make(map[string]bool)
	for i := 1; i <= 5; i++ {
		tag := "{#etape-" + string(rune('0'+i)) + "}"
		if strings.Contains(string(data), tag) {
			anchors[tag[2:len(tag)-1]] = true // "etape-N"
		}
	}
	return anchors
}

// docAnchor reports the "#etape-N" fragment of a Doc, or "" when Doc is empty.
func docAnchor(doc string) string {
	i := strings.LastIndex(doc, "#")
	if i < 0 {
		return ""
	}
	return doc[i+1:]
}

func TestDiagnoseFiveSteps(t *testing.T) {
	notExist := &fs.PathError{Op: "open", Path: "/dev/usb/lp0", Err: fs.ErrNotExist}
	permDenied := &fs.PathError{Op: "open", Path: "/dev/usb/lp0", Err: fs.ErrPermission}
	readFailed := &fs.PathError{Op: "read", Path: "/dev/usb/lp0", Err: errors.New("short read")}
	closedLike := errors.New("fermé")
	dialRefused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}

	type testCase struct {
		name    string
		subject Subject
		status  ports.PrinterStatus
		queues  func(context.Context) ([]string, error)
		stat    func(string) (fs.FileInfo, error)

		selfTestReceipt ports.PrintReceipt
		selfTestErr     error

		want              [5]StepState
		wantSelfTestCalls int
		check             func(t *testing.T, steps []DiagnosisStep)
	}

	cases := []testCase{
		{
			name:    "devfile : nœud absent",
			subject: Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:  ports.PrinterStatus{Health: ports.PrinterFaulted, Cause: notExist},
			want:    [5]StepState{StepFail, StepNotApplicable, StepNotApplicable, StepNotApplicable, StepNotApplicable},
			check: func(t *testing.T, steps []DiagnosisStep) {
				if !strings.Contains(steps[0].Observed, "/dev/usb/lp0") {
					t.Errorf("Observed = %q : doit nommer le nœud absent", steps[0].Observed)
				}
				if got := docAnchor(steps[0].Doc); got != "etape-1" {
					t.Errorf("Doc = %q : attendu une ancre etape-1", steps[0].Doc)
				}
			},
		},
		{
			name:    "devfile : droit d'écrire refusé",
			subject: Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:  ports.PrinterStatus{Health: ports.PrinterFaulted, Cause: permDenied},
			want:    [5]StepState{StepPass, StepFail, StepNotApplicable, StepNotApplicable, StepNotApplicable},
			check: func(t *testing.T, steps []DiagnosisStep) {
				if !strings.Contains(steps[1].Remedy, "groupe lp") {
					t.Errorf("Remedy de l'étape 2 = %q : doit nommer le groupe lp", steps[1].Remedy)
				}
			},
		},
		{
			name:              "devfile : silence, puis un auto-test qui réussit",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 16214},
			want:              [5]StepState{StepPass, StepPass, StepUnknown, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
			check: func(t *testing.T, steps []DiagnosisStep) {
				if strings.Contains(steps[2].Observed, "erreur") || strings.Contains(steps[2].Remedy, "erreur") {
					t.Errorf("étape 3, silence : ni Observed ni Remedy ne doivent dire « erreur » (%q / %q)",
						steps[2].Observed, steps[2].Remedy)
				}
				if !strings.Contains(steps[2].Remedy, "On continue") {
					t.Errorf("Remedy de l'étape 3 = %q : doit dire qu'on continue", steps[2].Remedy)
				}
				want := "16 214 octets sur 16 214."
				if steps[3].Observed != want {
					t.Errorf("Observed de l'étape 4 = %q, attendu %q", steps[3].Observed, want)
				}
			},
		},
		{
			name:              "devfile : cause non classée, jamais pass",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: closedLike},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepUnknown, StepUnknown, StepUnknown, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			// Un open qui échoue pour un errno autre qu'absent/permission (CUPS tient
			// déjà le nœud, imprimante éteinte…) n'est NI une preuve d'ouverture NI un
			// échec nommé : Review Focus interdit de lire ça comme "pass". C'est le cas
			// réaliste que les mutants m1/m3 de la revue font passer inaperçu.
			name:              "devfile : open échoue pour un autre errno, jamais pass",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterFaulted, Cause: &fs.PathError{Op: "open", Path: "/dev/usb/lp0", Err: syscall.EBUSY}},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepUnknown, StepUnknown, StepUnknown, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			// ErrUnsupported ne veut dire qu'une chose pour devfile : le transport n'a
			// RIEN ouvert ni demandé. Ce n'est donc pas la preuve positive que steps 1
			// et 2 exigent (mutant m3 de la revue).
			name:              "devfile : ErrUnsupported n'est pas une preuve d'ouverture",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: ports.ErrUnsupported},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepUnknown, StepUnknown, StepUnknown, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:              "devfile : échec après ouverture",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterFaulted, Cause: readFailed, Detail: "bourrage papier"},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepPass, StepPass, StepFail, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:    "tcp : connexion impossible",
			subject: Subject{Transport: domain.TransportTCP, Device: "192.168.1.50:9100"},
			status:  ports.PrinterStatus{Health: ports.PrinterFaulted, Cause: dialRefused},
			want:    [5]StepState{StepFail, StepNotApplicable, StepNotApplicable, StepNotApplicable, StepNotApplicable},
			check: func(t *testing.T, steps []DiagnosisStep) {
				if !strings.Contains(steps[0].Observed, "connexion impossible") {
					t.Errorf("Observed = %q : doit dire « connexion impossible »", steps[0].Observed)
				}
				if !strings.Contains(steps[0].Observed, "connection refused") {
					t.Errorf("Observed = %q : doit garder le texte brut du transport", steps[0].Observed)
				}
			},
		},
		{
			name:              "tcp : silence",
			subject:           Subject{Transport: domain.TransportTCP, Device: "192.168.1.50:9100"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepPass, StepNotApplicable, StepUnknown, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
			check: func(t *testing.T, steps []DiagnosisStep) {
				if !strings.Contains(steps[0].Observed, "connexion établie") {
					t.Errorf("Observed = %q : doit dire « connexion établie »", steps[0].Observed)
				}
			},
		},
		{
			// Une cause tcp que ce fichier ne sait pas classer (ni nil, ni
			// *net.OpError) n'est pas non plus une preuve de connexion (mutant m2).
			name:              "tcp : cause non classée, jamais pass",
			subject:           Subject{Transport: domain.TransportTCP, Device: "192.168.1.50:9100"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: closedLike},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepUnknown, StepNotApplicable, StepUnknown, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:              "winspool : la file nommée existe",
			subject:           Subject{Transport: domain.TransportWinspool, Device: "SATO WS408_2"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: ports.ErrUnsupported},
			queues:            func(context.Context) ([]string, error) { return []string{"SATO WS408_2"}, nil },
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepPass, StepNotApplicable, StepNotApplicable, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:    "winspool : la file nommée n'existe pas",
			subject: Subject{Transport: domain.TransportWinspool, Device: "SATO WS408_1"},
			status:  ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: ports.ErrUnsupported},
			queues:  func(context.Context) ([]string, error) { return []string{"SATO WS408_2"}, nil },
			want:    [5]StepState{StepFail, StepNotApplicable, StepNotApplicable, StepNotApplicable, StepNotApplicable},
			check: func(t *testing.T, steps []DiagnosisStep) {
				if !strings.Contains(steps[0].Observed, "SATO WS408_2") {
					t.Errorf("Observed = %q : doit nommer les files vues", steps[0].Observed)
				}
			},
		},
		{
			name:    "winspool : l'énumération échoue",
			subject: Subject{Transport: domain.TransportWinspool, Device: "SATO WS408_2"},
			status:  ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: ports.ErrUnsupported},
			queues:  func(context.Context) ([]string, error) { return nil, errors.New("WMI indisponible") },
			// L'étape 1 n'a rien pu dire (Queues en erreur), mais l'étape 3 lit la
			// SONDE elle-même, pas l'étape 1 : un spooler renvoie ErrUnsupported et
			// l'étape 3 est donc "sans objet", pas "inconnue" (spec, winspool).
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepUnknown, StepNotApplicable, StepNotApplicable, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:    "file : dossier absent",
			subject: Subject{Transport: domain.TransportFile, Device: "/var/lib/openscale/labels"},
			status:  ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: ports.ErrUnsupported},
			stat:    func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist },
			want:    [5]StepState{StepFail, StepNotApplicable, StepNotApplicable, StepNotApplicable, StepNotApplicable},
		},
		{
			name:              "file : dossier présent",
			subject:           Subject{Transport: domain.TransportFile, Device: "/var/lib/openscale/labels"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown, Cause: ports.ErrUnsupported},
			stat:              func(string) (fs.FileInfo, error) { return nil, nil },
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepPass, StepNotApplicable, StepNotApplicable, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:              "transport vide : le profil neutre",
			subject:           Subject{Transport: ""},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 100},
			want:              [5]StepState{StepNotApplicable, StepNotApplicable, StepNotApplicable, StepPass, StepUnanswered},
			wantSelfTestCalls: 1,
		},
		{
			name:              "étape 4 en échec",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown},
			selfTestErr:       errors.New("l'imprimante a refusé la trame"),
			want:              [5]StepState{StepPass, StepPass, StepUnknown, StepFail, StepNotApplicable},
			wantSelfTestCalls: 1,
		},
		{
			name:              "étape 4 : zéro octet compté",
			subject:           Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			status:            ports.PrinterStatus{Health: ports.PrinterUnknown},
			selfTestReceipt:   ports.PrintReceipt{Bytes: 0},
			want:              [5]StepState{StepPass, StepPass, StepUnknown, StepUnknown, StepUnanswered},
			wantSelfTestCalls: 1,
		},
	}

	anchors := anchorsOfHandbook(t)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prober := &fakeProber{status: c.status, selfTestReceipt: c.selfTestReceipt, selfTestErr: c.selfTestErr}
			probes := DiagnosisProbes{Printer: prober, Queues: c.queues, Stat: c.stat}

			steps := Diagnose(context.Background(), c.subject, probes)

			if len(steps) != 5 {
				t.Fatalf("Diagnose a rendu %d étapes, toujours cinq attendues", len(steps))
			}
			for i, step := range steps {
				if step.Number != i+1 {
					t.Errorf("étape %d : Number = %d", i+1, step.Number)
				}
				if step.State != c.want[i] {
					t.Errorf("étape %d : State = %q, attendu %q (Observed=%q)", i+1, step.State, c.want[i], step.Observed)
				}
				if (step.State == StepFail || step.State == StepUnknown) && step.Doc != "" {
					if a := docAnchor(step.Doc); !anchors[a] {
						t.Errorf("étape %d : Doc = %q, ancre absente de handbook/imprimante.md", i+1, step.Doc)
					}
				}
				if step.State == StepPass || step.State == StepNotApplicable || step.State == StepUnanswered {
					if step.Remedy != "" && step.State == StepNotApplicable {
						// un pas sans objet ne remédie à rien.
						t.Errorf("étape %d : Remedy = %q pour un état not_applicable", i+1, step.Remedy)
					}
				}
			}
			if prober.interrogateCalls > 1 {
				t.Errorf("Interrogate appelé %d fois, au plus une attendue", prober.interrogateCalls)
			}
			if prober.selfTestCalls != c.wantSelfTestCalls {
				t.Errorf("SelfTest appelé %d fois, %d attendues", prober.selfTestCalls, c.wantSelfTestCalls)
			}
			if c.check != nil {
				c.check(t, steps)
			}
		})
	}
}

func TestAnswerStep(t *testing.T) {
	printed := AnswerStep(true, "")
	if printed.Number != 5 || printed.State != StepPass {
		t.Errorf("AnswerStep(true, \"\") = %+v, attendu l'étape 5 à pass", printed)
	}
	if printed.Remedy != "" || printed.Doc != "" {
		t.Errorf("AnswerStep(true, \"\") = %+v : pass ne porte ni remède ni renvoi", printed)
	}

	notPrinted := AnswerStep(false, "le voyant 1 clignote")
	if notPrinted.State != StepFail {
		t.Errorf("AnswerStep(false, …) = %q, attendu fail", notPrinted.State)
	}
	if !strings.Contains(notPrinted.Observed, "le voyant 1 clignote") {
		t.Errorf("Observed = %q : doit porter ce que le bénévole a vu", notPrinted.Observed)
	}
	if got := docAnchor(notPrinted.Doc); got != "etape-5" {
		t.Errorf("Doc = %q, attendu une ancre etape-5", notPrinted.Doc)
	}
}

func TestGroupThousands(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{214, "214"},
		{16214, "16 214"},
		{1000000, "1 000 000"},
	} {
		if got := groupThousands(c.n); got != c.want {
			t.Errorf("groupThousands(%d) = %q, attendu %q", c.n, got, c.want)
		}
	}
}
