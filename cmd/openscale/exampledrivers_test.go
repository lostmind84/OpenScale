package main

import (
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
	printerexample "openscale/internal/printing/example"
	"openscale/internal/scale"
	scaleexample "openscale/internal/scale/example"
)

// The two example drivers of docs/07: they are never registered in a shipped binary —
// a model nobody has must not appear in a volunteer's drop-down list — and they stay
// registrABLE, which is the whole promise the document makes to whoever copies them.

// --- Les deux drivers d'exemple ----------------------------------------------

// TestLesDriversDExempleNeSontJamaisEnregistres.
//
// internal/scale/example and internal/printing/example are COMPLETE drivers written to be
// copied (docs/07-ajouter-un-materiel.md). Registering either one would put in the drop-down
// list of a volunteer a value no station can honour — a toy protocol no scale of the parc
// speaks, a printer that writes into memory — and the fault would surface as a station that
// validates its configuration and then weighs nothing, or prints nothing.
//
// It is exactly the reasoning drivers.go already applies to `sbpl`, which §8.1 names and no
// station carries, and it is worth a test rather than a comment: the one-line registration
// of §5.2 is one line to add BY MISTAKE too, and the mistake reads like an improvement.
func TestLesDriversDExempleNeSontJamaisEnregistres(t *testing.T) {
	for _, descriptor := range scaleRegistry().Descriptors() {
		if descriptor.ID == scaleexample.ID {
			t.Errorf("le protocole d'exemple %q est enregistré : c'est un protocole JOUET, "+
				"qu'aucune balance ne parle. Un poste qui le choisit valide sa configuration "+
				"puis ne pèse rien", descriptor.ID)
		}
	}
	for _, descriptor := range printerRegistry().Descriptors() {
		if descriptor.ID == printerexample.ID {
			t.Errorf("le driver d'impression d'exemple %q est enregistré : il écrit en "+
				"mémoire et n'imprime rien. Un poste qui le choisit annonce « Étiquette "+
				"envoyée à l'imprimante » pendant que rien ne sort", descriptor.ID)
		}
	}
}

// TestLesDriversDExempleRestentEnregistrables is the other half, and without it the test
// above is satisfied by two packages that no longer compile as drivers.
//
// What is verified here is what NO conformance bench sees: the REGISTRY ENTRY, the value a
// driver package hands cmd/openscale and which the administration screen, `openscale doctor`
// and Config.Validate all read WITHOUT building anything. A driver can pass every bench and
// still register with an empty label, no option schema, or an endpoint that promises a
// detection nothing can perform — and an example that did any of those teaches it.
//
// The registries are THROWAWAY, built here and nowhere else: the examples are held to the
// completeness of a registered driver without ever becoming one.
func TestLesDriversDExempleRestentEnregistrables(t *testing.T) {
	scales := scale.NewRegistry()
	scales.Register(scaleexample.Driver())
	printers := printing.NewRegistry()
	printers.Register(printerexample.Driver())

	for _, descriptor := range scales.Descriptors() {
		t.Run("balance/"+descriptor.ID, func(t *testing.T) {
			checkIdentity(t, descriptor, "scale.type")
			checkOptionSchema(t, descriptor)
			checkExampleDecoder(t, scales, descriptor.ID)
			if descriptor.Endpoint != domain.EndpointSerialPort {
				t.Errorf("le protocole d'exemple déclare le point d'accès %q : l'exemple "+
					"montre la détection de §14.4, et un exemple qui ne la déclare pas ne "+
					"montre plus comment la déclarer", descriptor.Endpoint)
			}
			if len(scales.Candidates(scale.EndpointSerialPort)) != 1 {
				t.Error("le protocole d'exemple déclare le port série et la détection ne le " +
					"propose pas : la déclaration du descripteur et ce que le registre essaie " +
					"vraiment sont la même chose, ou la détection ment")
			}
		})
	}
	for _, descriptor := range printers.Descriptors() {
		t.Run("imprimante/"+descriptor.ID, func(t *testing.T) {
			checkIdentity(t, descriptor, "printer.type")
			checkOptionSchema(t, descriptor)
			checkSelfTests(t, descriptor)
			checkHeadGeometry(t, descriptor)
		})
	}
}

// checkExampleDecoder holds the example to the clause the registry itself cannot see: a
// decoder FACTORY that answers nil, or that hands the same accumulator to two callers.
//
// The second is the fabricated mass the whole grammar exists to refuse — half a frame read
// on one port completed by the bytes of another, on a label somebody sticks on a bag.
func checkExampleDecoder(t *testing.T, registry *scale.Registry, id string) {
	t.Helper()
	first, err := registry.NewDecoder(id)
	if err != nil {
		t.Fatalf("le protocole %q ne donne aucun décodeur : %v", id, err)
	}
	second, err := registry.NewDecoder(id)
	if err != nil {
		t.Fatalf("second décodeur de %q : %v", id, err)
	}
	if first == nil || second == nil {
		t.Fatalf("la fabrique de décodeurs de %q répond nil", id)
	}
	if address(first) == address(second) && address(first) != 0 {
		t.Errorf("les deux décodeurs de %q sont le même objet : deux ports qui partagent ce "+
			"tampon complètent la demi-trame de l'un avec les octets de l'autre", id)
	}
}
