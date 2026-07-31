package platform

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// RawPrintPort is the port a label printer with an Ethernet card listens on: 9100, the
// raw socket every one of them implements (§8.4, transport `tcp`).
//
// It is not configurable HERE on purpose: what a station prints on travels in
// printer.options.address, host and port, and this scan only proposes candidates.
const RawPrintPort = 9100

// DiscoverBudget is how long the scan of §14.4 gets: two seconds.
//
// It is short deliberately. A volunteer pressed « Rechercher l'imprimante » and is
// looking at the screen; a scan that took thirty seconds would be pressed again, twice,
// and then abandoned. What it costs is the far end of the range on a busy network, and
// the remedy for that is the one the same screen already offers: type the address.
const DiscoverBudget = 2 * time.Second

// discoverConcurrency is how many addresses are probed at once.
//
// Sixty-four, so that 254 addresses fit in four rounds inside the budget. Higher would
// exhaust the ephemeral ports of a Windows kiosk PC, which fails as « network unreachable
// » on the connections a customer's weighing needs — the scan must never be able to cost
// a label.
const discoverConcurrency = 64

// DiscoverOptions is what the scan needs and what a test replaces.
//
// The two seams exist for the same reason serial.Options.Open does: the production path
// opens 254 sockets on somebody's shop network, and a test must exercise the scan itself
// — the budget, the concurrency, the ordering of the answers — without touching a
// network at all.
type DiscoverOptions struct {
	// Clock measures the budget. Required: a scan bounded by the real clock would make
	// its own test wait two seconds (§5.3).
	Clock ports.Clock
	// Budget bounds the whole scan. Zero means DiscoverBudget.
	Budget time.Duration
	// Dial opens one connection. nil means the real dialer.
	Dial func(ctx context.Context, address string) (net.Conn, error)
	// Subnets lists the /24 networks to walk. nil means « the ones this machine is on ».
	Subnets func() ([]net.IP, error)
}

// DiscoverPrinters scans the local /24 for something listening on the raw print port.
//
// # What it proves, and what it does not
//
// A host that accepts a connection on 9100 is a candidate and NOT a printer: a proxy, a
// switch's management port or another station's forwarded socket would answer the same
// way. That is why the answer goes into a list an operator picks from, and why the
// wording says « répond » rather than « imprimante trouvée ». Sending a status query to
// find out would mean writing bytes to a device nobody identified yet, on a network the
// shop shares.
//
// # Why it is a POST in the route table
//
// It takes seconds and it touches every address of the subnet. §14.5 declares it
// POST /admin/api/printers/discover for that reason, next to the GET that merely lists
// what is already installed.
func DiscoverPrinters(ctx context.Context, o DiscoverOptions) ([]PrintQueue, error) {
	if o.Clock == nil {
		return nil, fmt.Errorf("platform : le balayage réseau reçoit une horloge, jamais time.Now")
	}
	budget := o.Budget
	if budget <= 0 {
		budget = DiscoverBudget
	}
	dial := o.Dial
	if dial == nil {
		dial = dialRaw
	}
	subnets := o.Subnets
	if subnets == nil {
		subnets = localSubnets
	}

	bases, err := subnets()
	if err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("ce poste n'a aucune adresse IPv4 locale : il n'y a pas de réseau " +
			"à balayer. Une imprimante réseau se déclare par son adresse dans printer.options.address")
	}

	ctx, cancel := ports.WithBudget(ctx, o.Clock, budget)
	defer cancel()

	found := probeAll(ctx, dial, addressesOf(bases))
	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found, nil
}

// addressesOf turns each /24 base into the 254 host addresses of that network.
//
// The network address and the broadcast address are left out: neither is a host, and a
// broadcast probe on a shop network is the kind of thing that makes a switch complain.
func addressesOf(bases []net.IP) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(bases)*254)
	for _, base := range bases {
		four := base.To4()
		if four == nil {
			continue
		}
		for host := 1; host <= 254; host++ {
			address := net.JoinHostPort(
				net.IPv4(four[0], four[1], four[2], byte(host)).String(),
				strconv.Itoa(RawPrintPort))
			if seen[address] {
				continue
			}
			seen[address] = true
			out = append(out, address)
		}
	}
	return out
}

// probeAll dials every address with a bounded pool and reports what answered.
//
// Every goroutine it starts is TRANSIENT and ends with the context: the budget cancels
// them, and the wait group is what makes « the scan returned » mean « nothing of it is
// still running » (§13.1).
func probeAll(ctx context.Context, dial func(context.Context, string) (net.Conn, error),
	addresses []string) []PrintQueue {
	queue := make(chan string)
	var (
		mu    sync.Mutex
		found []PrintQueue
		wait  sync.WaitGroup
	)
	for worker := 0; worker < discoverConcurrency; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for address := range queue {
				conn, err := dial(ctx, address)
				if err != nil {
					continue
				}
				_ = conn.Close()
				mu.Lock()
				found = append(found, PrintQueue{
					Name: address,
					Key:  domain.DeviceKeyAddress,
					Detail: fmt.Sprintf("répond sur le port %d : candidat pour "+
						"printer.options.transport = tcp", RawPrintPort),
				})
				mu.Unlock()
			}
		}()
	}

	for _, address := range addresses {
		select {
		case queue <- address:
		case <-ctx.Done():
			// The budget ran out. What was already found is what the operator gets, and
			// the screen offers to type an address — never an empty list presented as a
			// finished search.
			close(queue)
			wait.Wait()
			return found
		}
	}
	close(queue)
	wait.Wait()
	return found
}

// dialRaw opens one TCP connection and closes it at once.
//
// The per-address timeout is a NETWORK deadline in the kernel's TCP stack, of the same
// nature as the ReadHeaderTimeout of internal/web and the single-instance probe of
// cmd/openscale: no business decision rests on it, and no test can be made to wait on it
// because the scan itself is bounded by the injected clock. What it buys is that one
// address that black-holes packets does not hold a worker for the whole budget.
func dialRaw(ctx context.Context, address string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: perAddressTimeout}
	return dialer.DialContext(ctx, "tcp", address)
}

// perAddressTimeout is how long one address gets before the worker moves on. It is
// deliberately shorter than the whole budget, so that the pool keeps turning.
const perAddressTimeout = 500 * time.Millisecond

// localSubnets reports the /24 networks this machine has an address on.
//
// The loopback is excluded — there is no printer at 127.0.0.x — and so is anything that
// is not IPv4: the raw print port is an IPv4 practice, and a /64 of IPv6 is not something
// anybody scans.
func localSubnets() ([]net.IP, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("les adresses de ce poste ne peuvent pas être lues : %w", err)
	}
	var out []net.IP
	seen := make(map[string]bool)
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if !ok {
			continue
		}
		four := network.IP.To4()
		if four == nil || network.IP.IsLoopback() {
			continue
		}
		key := fmt.Sprintf("%d.%d.%d", four[0], four[1], four[2])
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, four)
	}
	return out, nil
}
