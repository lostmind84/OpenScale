package platform

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.bug.st/serial/enumerator"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// What these tests can and cannot assert is worth saying once. Enumerating the serial
// ports and the print queues of the machine the test runs on answers a question nobody can
// predict — a development laptop has an unknown number of both — so what is asserted is
// the CONTRACT: it does not fail, it invents nothing, and the order is stable. The network
// scan, on the other hand, is fully driven: its two seams replace the dialer and the
// subnets, so the budget, the concurrency and the wording are exercised without touching
// anybody's network.

// TestEnumeratingThePortsOfThisMachineNeverFails is the property the « Détecter
// automatiquement » button rests on.
//
// A machine with no serial port at all is the ORDINARY development case, and it must
// produce an empty list rather than an error: a screen that showed « impossible
// d'énumérer les ports » on a station whose scale is simply unplugged would send a
// volunteer looking for a software failure.
func TestEnumeratingThePortsOfThisMachineNeverFails(t *testing.T) {
	found, err := SerialPorts(context.Background())
	if err != nil {
		t.Fatalf("énumération des ports : %v", err)
	}
	for _, port := range found {
		if strings.TrimSpace(port.Name) == "" {
			t.Fatalf("un port sans nom a été rendu : %+v", port)
		}
	}
	for i := 1; i < len(found); i++ {
		if found[i-1].Name > found[i].Name {
			t.Fatalf("les ports ne sont pas ordonnés : %s avant %s, "+
				"une liste déroulante dont les entrées bougent fait choisir la mauvaise",
				found[i-1].Name, found[i].Name)
		}
	}
}

// TestACancelledEnumerationIsNotStarted is the honest half of the context: the system call
// cannot be interrupted half way, so what the context buys is refusing to start one nobody
// is waiting for.
func TestACancelledEnumerationIsNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SerialPorts(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("erreur %v, attendu context.Canceled", err)
	}
	if _, err := PrintQueues(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("erreur %v, attendu context.Canceled", err)
	}
}

// TestThePortDescriptionSaysWhatTheSystemKnowsAndNothingMore is §14.4: « COM8 » names
// nothing, « COM8 — USB Serial Port » names a cable somebody can see.
//
// The last case is the one that matters: a port with no USB identity at all gets an EMPTY
// description, not the word « port série ». A label that repeats the name teaches an
// operator to stop reading labels.
func TestThePortDescriptionSaysWhatTheSystemKnowsAndNothingMore(t *testing.T) {
	for _, c := range []struct {
		what string
		port enumerator.PortDetails
		want string
	}{
		{"nom et fabricant", enumerator.PortDetails{
			Product: "USB Serial Port", Manufacturer: "FTDI"}, "USB Serial Port, FTDI"},
		{"le fabricant déjà contenu dans le nom", enumerator.PortDetails{
			Product: "FTDI FT232R USB UART", Manufacturer: "FTDI"}, "FTDI FT232R USB UART"},
		{"aucun nom, mais une identité USB", enumerator.PortDetails{
			IsUSB: true, VID: "0403", PID: "6001"}, "adaptateur USB 0403:6001"},
		{"un port série de carte mère", enumerator.PortDetails{}, ""},
	} {
		t.Run(c.what, func(t *testing.T) {
			if got := describePort(&c.port); got != c.want {
				t.Fatalf("description = %q, attendu %q", got, c.want)
			}
		})
	}
}

// TestEnumeratingThePrintQueuesOfThisMachineNeverFails is the same contract for the
// button « Lister les files ».
//
// On Windows it really does talk to the spooler; on Linux it globs the print nodes. Neither
// is allowed to fail on a machine that has none.
func TestEnumeratingThePrintQueuesOfThisMachineNeverFails(t *testing.T) {
	queues, err := PrintQueues(context.Background())
	if err != nil {
		t.Fatalf("énumération des files : %v", err)
	}
	defaults := 0
	for _, queue := range queues {
		if strings.TrimSpace(queue.Name) == "" {
			t.Fatalf("une file sans nom a été rendue : %+v", queue)
		}
		// The destination says which printer.options key it goes INTO, and the two twins of
		// §5.1 do not answer the same one: a Windows queue goes into `queue`, a print node
		// into `path`. The screen has no way of telling them apart by looking at the name,
		// and it wrote every one of them into `queue`.
		if queue.Key != domain.DeviceKeyQueue && queue.Key != domain.DeviceKeyPath {
			t.Fatalf("la destination %+v ne dit pas dans quelle clé elle va", queue)
		}
		if queue.Default {
			defaults++
		}
	}
	if defaults > 1 {
		t.Fatalf("%d files se déclarent « par défaut » : le système n'en a qu'une", defaults)
	}
}

// TestTheScanReportsWhatAnsweredAndNothingElse is the whole of the discovery of §14.4.
//
// Two hosts answer out of 254, and the scan reports those two, in order, with the wording
// that says « candidat » rather than « imprimante trouvée » — a host that accepts a
// connection on 9100 may be a proxy or a switch, and claiming otherwise would have an
// operator print a customer's label into a network appliance.
func TestTheScanReportsWhatAnsweredAndNothingElse(t *testing.T) {
	answering := map[string]bool{
		"192.168.1.7:" + strconv.Itoa(RawPrintPort):  true,
		"192.168.1.42:" + strconv.Itoa(RawPrintPort): true,
	}
	// An atomic and not an int: sixty-four workers probe at once, and a plain counter
	// would lose increments — which is the defect this test found in its own first
	// version.
	var attempts atomic.Int64
	found, err := DiscoverPrinters(context.Background(), DiscoverOptions{
		Clock:   fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)),
		Subnets: func() ([]net.IP, error) { return []net.IP{net.IPv4(192, 168, 1, 0)}, nil },
		Dial: func(_ context.Context, address string) (net.Conn, error) {
			attempts.Add(1)
			if answering[address] {
				return &closedConn{}, nil
			}
			return nil, errors.New("connexion refusée")
		},
	})
	if err != nil {
		t.Fatalf("balayage : %v", err)
	}
	if got := attempts.Load(); got != 254 {
		t.Fatalf("%d adresses essayées, attendu 254 : ni l'adresse de réseau ni la diffusion "+
			"ne sont des hôtes", got)
	}
	if len(found) != 2 {
		t.Fatalf("%d candidat(s), attendu 2 : %+v", len(found), found)
	}
	if found[0].Name != "192.168.1.42:9100" || found[1].Name != "192.168.1.7:9100" {
		t.Fatalf("les candidats ne sont pas ordonnés : %+v", found)
	}
	if !strings.Contains(found[0].Detail, "candidat") {
		t.Fatalf("le libellé promet plus qu'une connexion acceptée : %q", found[0].Detail)
	}
	// What the scan reports is an ADDRESS, and it says so. Clicking one on the Matériel
	// screen used to write 192.168.1.42:9100 into printer.options.queue, which validates,
	// which no transport reads, and which only fails when the socket is opened.
	if found[0].Key != domain.DeviceKeyAddress {
		t.Fatalf("le candidat %+v ne se déclare pas comme une adresse", found[0])
	}
}

// TestTheScanStopsOnItsBudget is what keeps the button honest.
//
// The budget is spent on the INJECTED clock, so this test proves the bound without waiting
// two seconds — and it proves the other half too: what was found before the budget ran out
// is still reported, because a volunteer who has one candidate and a timeout is better off
// than one who has an empty list presented as a finished search.
func TestTheScanStopsOnItsBudget(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	first := make(chan struct{})
	type outcome struct {
		found []PrintQueue
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		found, err := DiscoverPrinters(context.Background(), DiscoverOptions{
			Clock:   clock,
			Budget:  DiscoverBudget,
			Subnets: func() ([]net.IP, error) { return []net.IP{net.IPv4(10, 0, 0, 0)}, nil },
			Dial: func(ctx context.Context, address string) (net.Conn, error) {
				if address == "10.0.0.1:9100" {
					close(first)
					return &closedConn{}, nil
				}
				// Every other address hangs until the budget cancels the scan, which is
				// what a black-holed address really does.
				<-ctx.Done()
				return nil, ctx.Err()
			},
		})
		done <- outcome{found, err}
	}()

	<-first
	// The budget elapses on the INJECTED clock: no test of this application waits two
	// real seconds for a bound it can move by hand (§16.4).
	awaitWaiter(t, clock)
	clock.Advance(DiscoverBudget)

	got := <-done
	if got.err != nil {
		t.Fatalf("balayage : %v", got.err)
	}
	if len(got.found) != 1 || got.found[0].Name != "10.0.0.1:9100" {
		t.Fatalf("le balayage interrompu doit rendre ce qu'il a trouvé : %+v", got.found)
	}
}

// awaitWaiter waits until the scan has registered its deadline on the fake clock.
//
// Without it the Advance could land BEFORE ports.WithBudget has asked for its deadline,
// and the scan would then wait for an instant that has already gone by — a flake that
// only shows up on a loaded machine.
func awaitWaiter(t *testing.T, clock *fake.Clock) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if waiters, _ := clock.Pending(); waiters > 0 {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("le balayage n'a jamais posé son échéance sur l'horloge injectée")
}

// TestTheScanRefusesWithoutAClockOrWithoutANetwork keeps both refusals in French and
// actionable: the first is a composition mistake, the second a station on no network at
// all — and the remedy there is to type the address, which the message says.
func TestTheScanRefusesWithoutAClockOrWithoutANetwork(t *testing.T) {
	if _, err := DiscoverPrinters(context.Background(), DiscoverOptions{}); err == nil {
		t.Fatal("un balayage sans horloge a été accepté")
	}
	_, err := DiscoverPrinters(context.Background(), DiscoverOptions{
		Clock:   fake.NewClock(time.Now()),
		Subnets: func() ([]net.IP, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("un balayage sans réseau s'est déclaré réussi")
	}
	if !strings.Contains(err.Error(), "printer.options.address") {
		t.Fatalf("le refus ne dit pas quoi faire : %v", err)
	}
}

// TestTheScanFindsTheNetworksOfThisMachineAndDialsForReal covers the two seams from the
// production side, which is the half a double cannot prove.
//
// localSubnets is asked what THIS machine is on — the loopback excluded, because there is no
// printer at 127.0.0.x — and dialRaw is pointed at a listener the test opens itself: a real
// TCP connection, instantly, with no shop network involved.
func TestTheScanFindsTheNetworksOfThisMachineAndDialsForReal(t *testing.T) {
	networks, err := localSubnets()
	if err != nil {
		t.Fatalf("adresses de ce poste : %v", err)
	}
	for _, base := range networks {
		if base.To4() == nil {
			t.Fatalf("un réseau non IPv4 est balayé : %s", base)
		}
		if base.IsLoopback() {
			t.Fatal("le loopback est balayé : il n'y a pas d'imprimante à 127.0.0.x")
		}
	}
	t.Logf("MESURE : %d réseau(x) /24 sur ce poste : %v", len(networks), networks)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("écoute de test : %v", err)
	}
	defer listener.Close()

	conn, err := dialRaw(context.Background(), listener.Addr().String())
	if err != nil {
		t.Fatalf("connexion à un port qui répond : %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	if _, err := dialRaw(context.Background(), "127.0.0.1:1"); err == nil {
		t.Fatal("un port que personne ne tient s'est déclaré ouvert")
	}
}

// closedConn is a connection the scan only ever closes.
//
// The scan never writes to a candidate: sending a status query would mean writing bytes to
// a device nobody has identified yet, on a network the shop shares.
type closedConn struct{ net.Conn }

// Close reports the release of a connection that never carried anything.
func (*closedConn) Close() error { return nil }
