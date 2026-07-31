package transport_test

// What the shared conformance suite does not cover: the refusals a configuration meets,
// the two answers a one-way transport gives, the status probe and its budget, and the
// round trip of the file transport read back off the real disk.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing/transport"
	"openscale/internal/station/ports"
)

// t0 is where the injected clock starts. It is the instant §8.4 shows in the name of a
// job file, so the names this package produces can be read against the document.
var t0 = time.Date(2026, 7, 24, 14, 32, 5, 0, time.UTC)

// frame stands in for an encapsulated label: binary, with the ESC bytes of §8.3 in it.
var frame = []byte("\x1bA\x1bA1020300320\x1bGH040203\x00\xffABCDEF\r\n\x1bZ")

// TestDescriptorsAreTheFourOfTheDocument keeps the registry and the configuration in
// step: control 8 of Config.Validate checks printer.options.transport against these
// names, and control 42 lists the same four when it refuses a serial link.
func TestDescriptorsAreTheFourOfTheDocument(t *testing.T) {
	want := []string{
		domain.TransportWinspool, domain.TransportDevfile,
		domain.TransportTCP, domain.TransportFile,
	}
	var got []string
	for _, d := range transport.Descriptors() {
		if d.Label == "" {
			t.Errorf("le transport %q n'a pas de libellé ; c'est ce qu'un bénévole lit dans la liste", d.ID)
		}
		got = append(got, d.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Descriptors() = %v, attendu %v", got, want)
	}
}

// TestEachTransportNamesTheDeviceKeyItReallyReads is what the administration screen rests
// on: it draws ONE device field and takes its key from the descriptor of the transport
// chosen, instead of showing the same `queue` box whatever the transport is.
//
// A descriptor naming a key its transport does not read is not a cosmetic mismatch. That
// is precisely how an IP address ended up in printer.options.queue on a station set to
// `tcp`: the file validated — `queue` is a key of the driver, and no control ties one to a
// transport — and the failure only surfaced when the socket was opened, on the morning of
// a sale.
//
// The assertion is made from the OTHER end: built with every device key empty, each
// transport must complain about the one key its own descriptor names, and about no other.
func TestEachTransportNamesTheDeviceKeyItReallyReads(t *testing.T) {
	for _, descriptor := range transport.Descriptors() {
		t.Run(descriptor.ID, func(t *testing.T) {
			if descriptor.DeviceKey == "" {
				t.Fatalf("le transport %q ne nomme aucune clé d'appareil ; l'écran ne saurait "+
					"pas où écrire ce qu'un bénévole y tape", descriptor.ID)
			}
			built, err := transport.New(descriptor.ID, transport.Config{Clock: fake.NewClock(t0)})
			if err == nil {
				built.Close()
				t.Fatalf("le transport %q s'est construit sans appareil désigné", descriptor.ID)
			}
			want := "printer.options." + descriptor.DeviceKey
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("le refus ne nomme pas %q : %v", want, err)
			}
		})
	}
}

// TestEveryTransportAnswersToItsConfiguredName is the other half of the same contract:
// a transport whose Name did not match its descriptor would be selectable and
// unreachable.
func TestEveryTransportAnswersToItsConfiguredName(t *testing.T) {
	for _, tc := range []struct {
		want string
		open func(ports.Clock) (ports.Transport, error)
	}{
		{domain.TransportWinspool, func(ports.Clock) (ports.Transport, error) {
			return transport.NewWinspool(transport.WinspoolOptions{Queue: testQueue})
		}},
		{domain.TransportDevfile, func(clk ports.Clock) (ports.Transport, error) {
			return transport.NewDevfile(transport.DevfileOptions{Path: testNode, Clock: clk})
		}},
		{domain.TransportTCP, func(clk ports.Clock) (ports.Transport, error) {
			return transport.NewTCP(transport.TCPOptions{Address: testAddress, Clock: clk})
		}},
		{domain.TransportFile, func(clk ports.Clock) (ports.Transport, error) {
			return transport.NewFile(transport.FileOptions{Dir: t.TempDir(), Clock: clk})
		}},
	} {
		t.Run(tc.want, func(t *testing.T) {
			tr, err := tc.open(fake.NewClock(t0))
			if err != nil {
				t.Fatalf("construction : %v", err)
			}
			defer tr.Close()
			if got := tr.Name(); got != tc.want {
				t.Fatalf("Name() = %q, attendu %q", got, tc.want)
			}
		})
	}
}

// TestAConfigurationThatCannotWorkIsRefusedAtConstruction covers control by control what
// §11.3 would otherwise only discover at print time.
//
// Every message is FRENCH and names the configuration key at fault, because that is what
// reaches the administration screen. The assertion is on the key: a message that does not
// say WHICH field is wrong sends a volunteer through the whole form.
func TestAConfigurationThatCannotWorkIsRefusedAtConstruction(t *testing.T) {
	clk := fake.NewClock(t0)
	for _, tc := range []struct {
		name    string
		build   func() (ports.Transport, error)
		mustSay string
	}{
		{"une file d'impression sans nom", func() (ports.Transport, error) {
			return transport.NewWinspool(transport.WinspoolOptions{Queue: "   "})
		}, "printer.options.queue"},
		{"un nœud d'impression sans chemin", func() (ports.Transport, error) {
			return transport.NewDevfile(transport.DevfileOptions{Clock: clk})
		}, "printer.options.path"},
		{"un nœud d'impression sans horloge", func() (ports.Transport, error) {
			return transport.NewDevfile(transport.DevfileOptions{Path: testNode})
		}, "horloge"},
		{"une imprimante réseau sans adresse", func() (ports.Transport, error) {
			return transport.NewTCP(transport.TCPOptions{Clock: clk})
		}, "printer.options.address"},
		{"une adresse qui n'en est pas une", func() (ports.Transport, error) {
			return transport.NewTCP(transport.TCPOptions{Address: "192.168.1.50:9100:9100", Clock: clk})
		}, "printer.options.address"},
		{"une imprimante réseau sans horloge", func() (ports.Transport, error) {
			return transport.NewTCP(transport.TCPOptions{Address: testAddress})
		}, "horloge"},
		{"un transport fichier sans répertoire", func() (ports.Transport, error) {
			return transport.NewFile(transport.FileOptions{Clock: clk})
		}, "printer.options.path"},
		{"un transport fichier sans horloge", func() (ports.Transport, error) {
			return transport.NewFile(transport.FileOptions{Dir: t.TempDir()})
		}, "horloge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := tc.build()
			if err == nil {
				tr.Close()
				t.Fatalf("la construction a été acceptée")
			}
			if !strings.Contains(err.Error(), tc.mustSay) {
				t.Fatalf("le message ne dit pas %q : %v", tc.mustSay, err)
			}
		})
	}
}

// TestAnAddressWithoutAPortGetsTheRawPrintingOne is the one completion this package
// makes on what an operator typed, and §8.4 names the figure: 9100.
func TestAnAddressWithoutAPortGetsTheRawPrintingOne(t *testing.T) {
	socket, err := transport.NewTCP(transport.TCPOptions{Address: " 192.168.1.50 ", Clock: fake.NewClock(t0)})
	if err != nil {
		t.Fatalf("NewTCP : %v", err)
	}
	defer socket.Close()
	if got, want := socket.Describe(), "imprimante réseau 192.168.1.50:9100"; got != want {
		t.Fatalf("Describe() = %q, attendu %q", got, want)
	}
}

// TestAOneWayTransportDeclaresItCannotBeInterrogated is important-7 at the byte layer:
// nothing comes back up a print queue or out of a file, and saying so is what lets the
// printer driver answer PrinterUnknown instead of inventing a verdict.
func TestAOneWayTransportDeclaresItCannotBeInterrogated(t *testing.T) {
	clk := fake.NewClock(t0)
	queue, err := transport.NewWinspool(transport.WinspoolOptions{Queue: testQueue})
	if err != nil {
		t.Fatalf("NewWinspool : %v", err)
	}
	defer queue.Close()
	spool, err := transport.NewFile(transport.FileOptions{Dir: t.TempDir(), Clock: clk})
	if err != nil {
		t.Fatalf("NewFile : %v", err)
	}
	defer spool.Close()

	for _, tr := range []ports.Transport{queue, spool} {
		t.Run(tr.Name(), func(t *testing.T) {
			raw, err := tr.Query(context.Background(), []byte{0x05}, 500*time.Millisecond)
			if !errors.Is(err, ports.ErrUnsupported) {
				t.Fatalf("Query = (%x, %v), attendu une erreur enveloppant ports.ErrUnsupported", raw, err)
			}
			if !strings.Contains(err.Error(), tr.Name()) {
				t.Fatalf("le message ne nomme pas le transport : %v", err)
			}
		})
	}
}

// TestTheProbeReportsWhatCameBack is level N3 of §8.5: « toute réponse non vide =
// imprimante vivante », and the transport hands the bytes up without reading them.
func TestTheProbeReportsWhatCameBack(t *testing.T) {
	answer := []byte{0x30, 0x41, 0x0d}
	d := newDevice()
	d.reply = answer
	node := nodeOn(t, d, fake.NewClock(t0))
	defer node.Close()

	raw, err := node.Query(context.Background(), []byte{0x05}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Query : %v", err)
	}
	if string(raw) != string(answer) {
		t.Fatalf("Query = %x, attendu %x", raw, answer)
	}
	if sent := d.delivered(); len(sent) != 1 || sent[0] != 0x05 {
		t.Fatalf("la requête envoyée est %x, attendu 05 (ENQ)", sent)
	}
}

// TestSilenceWithinTheBudgetIsNotAFailure is the decision of §8.5 spelled out: the
// contrapositive of « toute réponse non vide = vivante » is « on ne sait pas », never
// « morte ».
//
// The budget is spent on the INJECTED clock, which is why this test costs microseconds
// instead of half a second.
func TestSilenceWithinTheBudgetIsNotAFailure(t *testing.T) {
	clk := fake.NewClock(t0)
	node := nodeOn(t, newDevice(), clk)
	defer node.Close()

	answered := make(chan struct{})
	var raw []byte
	var err error
	go func() {
		defer close(answered)
		raw, err = node.Query(context.Background(), []byte{0x05}, 500*time.Millisecond)
	}()
	waitForWaiter(t, clk)
	clk.Advance(500 * time.Millisecond)
	<-answered

	if err != nil {
		t.Fatalf("un silence a été rendu comme une panne : %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("Query = %x, attendu aucune réponse", raw)
	}
}

// TestABrokenLinkIsAFailureAndSilenceIsNot draws the line the previous test leaves: a
// read that fails is a link that broke, and that one IS an error.
func TestABrokenLinkIsAFailureAndSilenceIsNot(t *testing.T) {
	d := newDevice()
	d.readErr = errors.New("le périphérique a disparu")
	node := nodeOn(t, d, fake.NewClock(t0))
	defer node.Close()

	if _, err := node.Query(context.Background(), []byte{0x05}, 500*time.Millisecond); err == nil {
		t.Fatalf("une lecture en échec a été rendue comme un silence")
	}

	// And the end of the channel is silence, not a breakage: a device that closed without
	// saying anything told us nothing, which is a legitimate outcome of a probe whose
	// decoding §8.5 still files under « à qualifier ».
	quiet := newDevice()
	quiet.readErr = io.EOF
	closed := nodeOn(t, quiet, fake.NewClock(t0))
	defer closed.Close()
	if raw, err := closed.Query(context.Background(), []byte{0x05}, 500*time.Millisecond); err != nil || raw != nil {
		t.Fatalf("Query = (%x, %v), attendu un silence sans erreur", raw, err)
	}
}

// TestTheProbeRefusesWhatItCannotDo covers the two ways a caller can ask for something
// that is not a status probe.
func TestTheProbeRefusesWhatItCannotDo(t *testing.T) {
	node := nodeOn(t, newDevice(), fake.NewClock(t0))
	defer node.Close()

	if _, err := node.Query(context.Background(), nil, 500*time.Millisecond); err == nil {
		t.Fatalf("une interrogation sans requête a été acceptée")
	}
	if _, err := node.Query(context.Background(), []byte{0x05}, 0); err == nil {
		t.Fatalf("une interrogation sans délai a été acceptée")
	}
}

// TestAClosedTransportRefusesEverything is what a configuration reload leaves behind: the
// old transport must not reopen the device the new one has just taken (§11.4).
func TestAClosedTransportRefusesEverything(t *testing.T) {
	node := nodeOn(t, newDevice(), fake.NewClock(t0))
	if err := node.Close(); err != nil {
		t.Fatalf("Close : %v", err)
	}

	if _, err := node.Write(context.Background(), frame); !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("Write après Close = %v, attendu transport.ErrClosed", err)
	}
	if _, err := node.Query(context.Background(), []byte{0x05}, time.Second); !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("Query après Close = %v, attendu transport.ErrClosed", err)
	}
}

// stubbornWrite is how long the test below waits for a cancelled Write NOT to come back.
//
// A negative assertion about concurrency needs a window, and this is the only one in the
// package. It is not a business delay in disguise — nothing here is timing a printer —
// and the alternative it rules out is a handful of instructions, so a tenth of a second
// is orders of magnitude more than enough while staying well inside the ten seconds §16.4
// budgets for the whole race-enabled suite.
const stubbornWrite = 100 * time.Millisecond

// TestACancelledWriteWaitsForItsOwnWriteToLeaveTheDevice is the half of failure test 6
// that giving the caller the floor back does NOT cover.
//
// Closing the handle is what returns a parked write — that much a leak count catches. But
// closing a handle does not always END the write that was already inside it: CloseHandle
// comes back before the I/O it interrupted does, and WritePrinter has no documented
// behaviour while a document is being ended underneath it. So the cancellation path WAITS
// for its own goroutine, and this is where that wait is verified: the destination here
// stays inside its write after the close, and Write may not come back before it leaves.
//
// Without the wait, the print service — which serializes on one mutex, §8.2 — would start
// the next label while a goroutine of the previous one is still in the device.
func TestACancelledWriteWaitsForItsOwnWriteToLeaveTheDevice(t *testing.T) {
	d := newDevice()
	d.parks = true
	d.lingers = true
	node := nodeOn(t, d, fake.NewClock(t0))
	defer node.Close()

	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	go func() {
		_, err := node.Write(ctx, frame)
		returned <- err
	}()

	<-d.writeStarted()
	cancel()
	<-d.entered // the handle is closed and the write is still inside the destination

	select {
	case err := <-returned:
		t.Fatalf("Write a rendu la main (%v) alors que sa propre écriture était encore "+
			"dans le périphérique : fermer la poignée ne suffit pas, il faut attendre la goroutine", err)
	case <-time.After(stubbornWrite):
	}

	close(d.letGo)
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatalf("Write = %v, attendu une erreur de contexte", err)
	}
}

// TestAProbeOnADeadContextWritesNothing is the same clause as the one the suite checks on
// Write, applied to the probe: the troubleshooting screen that asked for a status has
// already gone away.
func TestAProbeOnADeadContextWritesNothing(t *testing.T) {
	d := newDevice()
	node := nodeOn(t, d, fake.NewClock(t0))
	defer node.Close()

	dead, cancel := context.WithCancel(context.Background())
	cancel()

	if raw, err := node.Query(dead, []byte{0x05}, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("Query = (%x, %v), attendu une erreur de contexte", raw, err)
	}
	if sent := d.delivered(); len(sent) != 0 {
		t.Fatalf("%x ont été envoyés sur un contexte déjà annulé", sent)
	}
}

// TestAProbeCancelledMidReadLetsGoOfTheLink is failure test 6 on the status path: the
// device answered nothing and the caller went away, and neither the goroutine nor the
// handle may outlive the call.
func TestAProbeCancelledMidReadLetsGoOfTheLink(t *testing.T) {
	clk := fake.NewClock(t0)
	d := newDevice()
	node := nodeOn(t, d, clk)
	defer node.Close()

	ctx, cancel := context.WithCancel(context.Background())
	answered := make(chan error, 1)
	go func() {
		_, err := node.Query(ctx, []byte{0x05}, time.Second)
		answered <- err
	}()

	waitForWaiter(t, clk)
	cancel()

	if err := <-answered; !errors.Is(err, context.Canceled) {
		t.Fatalf("Query = %v, attendu une erreur de contexte", err)
	}
	if !d.isClosed() {
		t.Fatalf("la liaison est restée ouverte après l'annulation ; c'est elle qui débloque la lecture")
	}
}

// TestAClosedNetworkTransportRefusesEverything covers the second bidirectional transport
// on the same clause: a reload replaces it, and the one that was replaced must not dial.
func TestAClosedNetworkTransportRefusesEverything(t *testing.T) {
	socket, err := transport.NewTCP(transport.TCPOptions{
		Address: testAddress,
		Clock:   fake.NewClock(t0),
		Dial:    func(context.Context, string) (transport.Duplex, error) { return newDevice(), nil },
	})
	if err != nil {
		t.Fatalf("NewTCP : %v", err)
	}
	if err := socket.Close(); err != nil {
		t.Fatalf("Close : %v", err)
	}

	if _, err := socket.Write(context.Background(), frame); !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("Write après Close = %v, attendu transport.ErrClosed", err)
	}
	if _, err := socket.Query(context.Background(), []byte{0x05}, time.Second); !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("Query après Close = %v, attendu transport.ErrClosed", err)
	}
}

// TestACloseThatFailsIsNotASuccessfulPrint is the second half of important-7 at this
// layer: on a spooler it is EndDocPrinter that RELEASES the job, so a write that
// succeeded and a close that failed is a label nobody printed.
func TestACloseThatFailsIsNotASuccessfulPrint(t *testing.T) {
	d := newDevice()
	d.closeErr = errors.New("le travail n'a pas pu être remis")
	node := nodeOn(t, d, fake.NewClock(t0))
	defer node.Close()

	n, err := node.Write(context.Background(), frame)
	if err == nil {
		t.Fatalf("Write a rendu %d octets et aucune erreur alors que la fermeture a échoué", n)
	}
}

// TestAWriteThatFailsMidwayReportsWhatWentThrough keeps the count honest on the path
// where the device gives up in the middle.
func TestAWriteThatFailsMidwayReportsWhatWentThrough(t *testing.T) {
	d := newDevice()
	d.writeErr = errors.New("l'imprimante a coupé la liaison")
	node := nodeOn(t, d, fake.NewClock(t0))
	defer node.Close()

	n, err := node.Write(context.Background(), frame)
	if err == nil {
		t.Fatalf("une écriture en échec a été rendue comme un succès")
	}
	if n != 0 {
		t.Fatalf("Write = %d octets, attendu 0", n)
	}
	if !strings.Contains(err.Error(), testNode) {
		t.Fatalf("le message ne nomme pas la destination : %v", err)
	}
}

// --- the file transport, read back off the disk ----------------------------

// TestTheFileTransportWritesExactlyWhatItWasGiven is the round trip the whole diagnostic
// use rests on: an SBPL frame is binary, and a byte translated on the way is a frame the
// printer no longer understands.
func TestTheFileTransportWritesExactlyWhatItWasGiven(t *testing.T) {
	dir := t.TempDir()
	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()

	n, err := spool.Write(context.Background(), frame)
	if err != nil {
		t.Fatalf("Write : %v", err)
	}
	if n != len(frame) {
		t.Fatalf("Write = %d octets, attendu %d", n, len(frame))
	}

	written, err := os.ReadFile(spool.LastPath())
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(written) != string(frame) {
		t.Fatalf("le fichier porte %x, attendu %x", written, frame)
	}
	if got, want := filepath.Base(spool.LastPath()), "2026-07-24T14-32-05_001.sbpl"; got != want {
		t.Fatalf("le fichier s'appelle %q, attendu %q — c'est le nom de §8.4, les deux-points en moins", got, want)
	}
}

// TestTwoLabelsNeverShareAFile is why the creation is exclusive: a diagnostic file that
// replaced the one before it would lose the very frame somebody asked for.
func TestTwoLabelsNeverShareAFile(t *testing.T) {
	dir := t.TempDir()
	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()

	seen := make(map[string]bool)
	for range 3 {
		if _, err := spool.Write(context.Background(), frame); err != nil {
			t.Fatalf("Write : %v", err)
		}
		seen[spool.LastPath()] = true
	}
	if len(seen) != 3 {
		t.Fatalf("trois étiquettes ont produit %d fichiers : %v", len(seen), seen)
	}
}

// TestAFileLeftByAPreviousRunIsNeverOverwritten is the collision the sequence number
// alone cannot avoid: the counter restarts with the process, the clock does not.
func TestAFileLeftByAPreviousRunIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "2026-07-24T14-32-05_001.sbpl")
	if err := os.WriteFile(occupied, []byte("la trame d'hier"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}

	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()
	if _, err := spool.Write(context.Background(), frame); err != nil {
		t.Fatalf("Write : %v", err)
	}

	if spool.LastPath() == occupied {
		t.Fatalf("le transport a écrasé %s", occupied)
	}
	kept, err := os.ReadFile(occupied)
	if err != nil || string(kept) != "la trame d'hier" {
		t.Fatalf("le fichier précédent vaut %q (%v)", kept, err)
	}
}

// TestADirectoryThatDoesNotExistYetIsCreated keeps a support directory nobody created
// from costing a label.
func TestADirectoryThatDoesNotExistYetIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "etiquettes", "poste-2")
	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()

	if _, err := spool.Write(context.Background(), frame); err != nil {
		t.Fatalf("Write : %v", err)
	}
	if _, err := os.Stat(spool.LastPath()); err != nil {
		t.Fatalf("le fichier n'a pas été créé : %v", err)
	}
}

// TestADirectoryThatCannotBeCreatedIsAnError is failure test 11 in miniature: the path
// is a FILE, so no directory can go there, and the transport says so instead of losing
// the frame quietly.
func TestADirectoryThatCannotBeCreatedIsAnError(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "obstacle")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}

	spool := spoolIn(t, filepath.Join(blocked, "etiquettes"), fake.NewClock(t0))
	defer spool.Close()
	if _, err := spool.Write(context.Background(), frame); err == nil {
		t.Fatalf("un répertoire impossible a été accepté")
	}
}

// TestTheSearchForAFreeNameGivesUp bounds a loop that would otherwise be infinite the day
// something else is writing into the same directory.
func TestTheSearchForAFreeNameGivesUp(t *testing.T) {
	spool, err := transport.NewFile(transport.FileOptions{
		Dir:    t.TempDir(),
		Clock:  fake.NewClock(t0),
		Create: func(string) (transport.Sink, error) { return nil, os.ErrExist },
	})
	if err != nil {
		t.Fatalf("NewFile : %v", err)
	}
	defer spool.Close()

	if _, err := spool.Write(context.Background(), frame); err == nil {
		t.Fatalf("la recherche d'un nom libre n'a jamais rendu la main")
	} else if !strings.Contains(err.Error(), "aucun nom libre") {
		t.Fatalf("message inattendu : %v", err)
	}
}

// TestLastPathIsEmptyBeforeTheFirstLabel keeps the troubleshooting screen from offering a
// file that does not exist.
func TestLastPathIsEmptyBeforeTheFirstLabel(t *testing.T) {
	spool := spoolIn(t, t.TempDir(), fake.NewClock(t0))
	defer spool.Close()
	if path := spool.LastPath(); path != "" {
		t.Fatalf("LastPath() = %q avant toute impression", path)
	}
}

// --- the production seams, exercised without any printer -------------------

// TestOpenSystemNodeWritesToARealFile exercises the REAL opener of the Linux default,
// against an ordinary file rather than /dev/usb/lp0.
//
// What it proves is what can be proved without the device: the flags open, the handle
// writes, and the bytes land unchanged. What it cannot prove — that O_SYNC really pushes
// them past the kernel buffer, that the node accepts O_RDWR — needs the bench, and that
// is what the //go:build hardware tests are for.
func TestOpenSystemNodeWritesToARealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "faux-noeud")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	node, err := transport.NewDevfile(transport.DevfileOptions{Path: path, Clock: fake.NewClock(t0)})
	if err != nil {
		t.Fatalf("NewDevfile : %v", err)
	}
	defer node.Close()

	if _, err := node.Write(context.Background(), frame); err != nil {
		t.Fatalf("Write : %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(written) != string(frame) {
		t.Fatalf("le nœud porte %x, attendu %x", written, frame)
	}
}

// TestOpenSystemNodeReportsAMissingDevice is the failure a station really meets: the
// udev link is gone, or somebody unplugged the printer.
func TestOpenSystemNodeReportsAMissingDevice(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "aucun-peripherique")
	node, err := transport.NewDevfile(transport.DevfileOptions{Path: missing, Clock: fake.NewClock(t0)})
	if err != nil {
		t.Fatalf("NewDevfile : %v", err)
	}
	defer node.Close()

	if _, err := node.Write(context.Background(), frame); err == nil {
		t.Fatalf("un nœud absent a été accepté")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("le message ne nomme pas le nœud : %v", err)
	}
}

// TestDialSystemTCPReachesARealSocket exercises the REAL dialer against a listener on the
// loopback, which is the closest a test gets to a printer on the network without one.
func TestDialSystemTCPReachesARealSocket(t *testing.T) {
	address, received := listenAndCollect(t)
	socket, err := transport.NewTCP(transport.TCPOptions{Address: address, Clock: fake.NewClock(t0)})
	if err != nil {
		t.Fatalf("NewTCP : %v", err)
	}
	defer socket.Close()

	if _, err := socket.Write(context.Background(), frame); err != nil {
		t.Fatalf("Write : %v", err)
	}
	if got := <-received; string(got) != string(frame) {
		t.Fatalf("la socket a reçu %x, attendu %x", got, frame)
	}
}

// TestDialSystemTCPReportsAPrinterThatIsOff is the KindTransient of §8.5 on the network
// path: the address is right and nothing is listening.
func TestDialSystemTCPReportsAPrinterThatIsOff(t *testing.T) {
	address := closedPort(t)
	socket, err := transport.NewTCP(transport.TCPOptions{Address: address, Clock: fake.NewClock(t0)})
	if err != nil {
		t.Fatalf("NewTCP : %v", err)
	}
	defer socket.Close()

	if _, err := socket.Write(context.Background(), frame); err == nil {
		t.Fatalf("une imprimante éteinte a été acceptée")
	} else if !strings.Contains(err.Error(), address) {
		t.Fatalf("le message ne nomme pas l'imprimante : %v", err)
	}
}
