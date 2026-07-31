package catalog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// What Assemble owns, and what it owes every reader that will ever exist.
//
// These tests go through catalog.RowReader and never through a format: that is the
// point of the seam ADR-052 draws. Before it, every one of the rules below lived in
// csvodoo and was proved by CSV fixtures — so a second format would have had to prove
// them again, or, far more likely, would have re-implemented them differently.

// scripted is a RowReader a test writes out step by step.
type scripted struct {
	steps  []step
	at     int
	closed int
}

// step is one answer of Next: a row, what the reader had to say, and how it ended.
type step struct {
	row      catalog.Row
	findings []domain.Finding
	err      error
}

// Next hands over the next scripted answer, then io.EOF for ever.
func (s *scripted) Next() (catalog.Row, []domain.Finding, error) {
	if s.at >= len(s.steps) {
		return catalog.Row{}, nil, io.EOF
	}
	answer := s.steps[s.at]
	s.at++
	return answer.row, answer.findings, answer.err
}

// Close counts, because Assemble promises to call it on EVERY exit.
func (s *scripted) Close() error {
	s.closed++
	return nil
}

// reads turns a list of rows into a reader that yields them and ends.
func reads(rows ...catalog.Row) *scripted {
	steps := make([]step, 0, len(rows))
	for _, r := range rows {
		steps = append(steps, step{row: r})
	}
	return &scripted{steps: steps}
}

// goodRow is a complete, weighable row: the reference plan of §6.2, a reserved zone at
// zero, a readable price and a unit that agrees with the prefix.
func goodRow(t *testing.T, rank int) catalog.Row {
	t.Helper()
	reference, err := domain.Compose(fmt.Sprintf("0493%03d00000", 100+rank))
	if err != nil {
		t.Fatalf("composition du code-barres de rang %d : %v", rank, err)
	}
	return catalog.Row{
		Line: rank + 1, ID: fmt.Sprintf("44%02d", rank), Name: fmt.Sprintf("PRODUIT %d", rank),
		Barcode: string(reference), Price: "5.32",
		CategoryCode: "vegetables", Magnitude: catalog.Continuous, PriceSuffix: " €/kg",
	}
}

// manyGoodRows is a page whose sound lines are the majority the absolute guard is
// written for: under 90 % readable, a batch is refused WHOLE, and a test about
// something else would then be testing the guard.
func manyGoodRows(t *testing.T, count int) []catalog.Row {
	t.Helper()
	rows := make([]catalog.Row, 0, count)
	for rank := 0; rank < count; rank++ {
		rows = append(rows, goodRow(t, rank))
	}
	return rows
}

// collecting is an ImageSink that remembers what it was handed.
type collecting struct {
	put    map[string]int
	refuse bool
}

func newCollecting() *collecting { return &collecting{put: map[string]int{}} }

// Put records one photo, or refuses when the test asked it to.
func (c *collecting) Put(sha, _ string, _ []byte) error {
	if c.refuse {
		return errors.New("disque plein")
	}
	c.put[sha]++
	return nil
}

// TestAssembleTurnsRowsIntoAWholeBatch: the nominal path, and the counts that go with
// it.
func TestAssembleTurnsRowsIntoAWholeBatch(t *testing.T) {
	reader := reads(manyGoodRows(t, 3)...)
	batch, err := catalog.Assemble(reader, catalog.AssembleOptions{
		Source: "essai", Designation: "flv_2.csv"})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Products) != 3 || batch.RowsRead != 3 || batch.UnreadableRows != 0 {
		t.Fatalf("%d produits, %d lignes lues, %d illisibles ; attendu 3/3/0",
			len(batch.Products), batch.RowsRead, batch.UnreadableRows)
	}
	if batch.Source != "essai" || batch.FileName != "flv_2.csv" {
		t.Errorf("la provenance n'est pas reportée : %q / %q", batch.Source, batch.FileName)
	}
	if batch.Products[0].Qualification != domain.Weighable ||
		batch.Products[0].Mode != domain.ByWeight {
		t.Errorf("le produit de référence n'est pas pesable au poids : %+v", batch.Products[0])
	}
	if reader.closed != 1 {
		t.Errorf("le lecteur a été fermé %d fois, attendu 1", reader.closed)
	}
}

// TestAssembleLeavesTheIdentityToWhoeverAcquiredTheBatch.
//
// ID and Bytes stay at zero on purpose: how a catalog is fingerprinted is the
// acquisition's business — the digest of the bytes for a file, catalog.Fingerprint for
// a producer that has none. An assembler that invented one would give the same identity
// to two batches read from two different places.
func TestAssembleLeavesTheIdentityToWhoeverAcquiredTheBatch(t *testing.T) {
	batch, err := catalog.Assemble(reads(manyGoodRows(t, 2)...), catalog.AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if batch.ID != "" || batch.Bytes != 0 {
		t.Fatalf("l'assemblage a inventé une identité : ID=%q Bytes=%d", batch.ID, batch.Bytes)
	}
}

// TestAnUnreadableRowIsCountedAndTheReadingCarriesOn: one mangled line is not a reason
// to lose the sound ones (§10.4a).
func TestAnUnreadableRowIsCountedAndTheReadingCarriesOn(t *testing.T) {
	rows := manyGoodRows(t, 10)
	reader := &scripted{steps: []step{{
		findings: []domain.Finding{{Code: domain.FindingUnreadableRow, CSVLine: 2}},
		err:      catalog.ErrRowUnreadable,
	}}}
	for _, r := range rows {
		reader.steps = append(reader.steps, step{row: r})
	}

	batch, err := catalog.Assemble(reader, catalog.AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Products) != 10 {
		t.Fatalf("%d produits, attendu 10 : la ligne cassée a emporté les autres",
			len(batch.Products))
	}
	if batch.RowsRead != 11 || batch.UnreadableRows != 1 {
		t.Errorf("%d lignes lues dont %d illisibles ; attendu 11 et 1",
			batch.RowsRead, batch.UnreadableRows)
	}
	if len(batch.Findings) != 1 {
		t.Errorf("%d signalement(s), attendu 1 : le motif du lecteur est perdu", len(batch.Findings))
	}
}

// TestAFailedReadRefusesTheWholeBatch: half a catalog never replaces a whole one
// (§10.4), so an error that is neither io.EOF nor ErrRowUnreadable stops everything.
func TestAFailedReadRefusesTheWholeBatch(t *testing.T) {
	broken := errors.New("la connexion est tombée")
	reader := &scripted{steps: []step{
		{row: goodRow(t, 0)},
		{err: broken},
	}}

	batch, err := catalog.Assemble(reader, catalog.AssembleOptions{})
	if batch != nil {
		t.Fatalf("un lot est remonté malgré une lecture en échec : %d produits", len(batch.Products))
	}
	if !errors.Is(err, broken) {
		t.Fatalf("l'échec du lecteur n'est pas remonté tel quel : %v", err)
	}
	if reader.closed != 1 {
		t.Errorf("le lecteur n'est pas fermé sur le chemin du refus (%d fermetures)", reader.closed)
	}
}

// TestATwiceUsedIdentifierIsSetAsideAndNamed.
//
// The id is the PRODUCER's key and an import is an upsert on it (§10.9): two rows
// sharing one would make the second overwrite the first in silence. The finding has to
// name the line that used it first, or nobody can go and fix it.
func TestATwiceUsedIdentifierIsSetAsideAndNamed(t *testing.T) {
	rows := manyGoodRows(t, 10)
	twin := goodRow(t, 42)
	twin.ID = rows[0].ID
	twin.Line = 99

	batch, err := catalog.Assemble(reads(append(rows, twin)...), catalog.AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Products) != 10 {
		t.Fatalf("%d produits, attendu 10 : le doublon est entré", len(batch.Products))
	}
	if batch.UnreadableRows != 1 {
		t.Errorf("UnreadableRows = %d, attendu 1", batch.UnreadableRows)
	}
	found := false
	for _, f := range batch.Findings {
		if f.CSVLine == 99 && strings.Contains(f.Message, fmt.Sprint(rows[0].Line)) {
			found = true
		}
	}
	if !found {
		t.Errorf("le signalement ne nomme pas la ligne %d qui portait déjà l'identifiant : %+v",
			rows[0].Line, batch.Findings)
	}
}

// TestTheAbsoluteGuardRefusesACatalogMostlyUnreadable: a file cut off in mid-write does
// not replace a healthy catalog (§10.4a).
func TestTheAbsoluteGuardRefusesACatalogMostlyUnreadable(t *testing.T) {
	reader := &scripted{steps: []step{
		{row: goodRow(t, 0)},
		{err: catalog.ErrRowUnreadable},
		{err: catalog.ErrRowUnreadable},
	}}

	batch, err := catalog.Assemble(reader, catalog.AssembleOptions{})
	if batch != nil {
		t.Fatal("un catalogue majoritairement illisible est entré en service")
	}
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("le refus ne porte pas ErrContent : %v", err)
	}
	if !strings.Contains(err.Error(), "illisible") {
		t.Errorf("le motif ne dit pas ce qui cloche : %v", err)
	}
}

// TestACatalogWithNoRowAtAllIsRefused: « la grille est vide » ne doit jamais être
// quelque chose qu'un import peut causer.
func TestACatalogWithNoRowAtAllIsRefused(t *testing.T) {
	if _, err := catalog.Assemble(reads(), catalog.AssembleOptions{}); !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("un catalogue sans une ligne est accepté : %v", err)
	}
}

// TestAFindingAboutTheWholeStreamTravelsWithTheFirstRow.
//
// A remark about the FILE — a header nobody expected — has no row of its own. The
// contract lets a reader hand it over with the first row it yields, and Assemble must
// keep it rather than assume every finding describes the row it arrived with.
func TestAFindingAboutTheWholeStreamTravelsWithTheFirstRow(t *testing.T) {
	preamble := domain.Finding{Code: domain.FindingUnexpectedHeader, CSVLine: 1}
	reader := &scripted{steps: []step{
		{row: goodRow(t, 0), findings: []domain.Finding{preamble}},
		{row: goodRow(t, 1)},
	}}

	batch, err := catalog.Assemble(reader, catalog.AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	for _, f := range batch.Findings {
		if f.Code == domain.FindingUnexpectedHeader {
			return
		}
	}
	t.Fatalf("la remarque sur l'en-tête est perdue : %+v", batch.Findings)
}

// --- Les photos, qui sont les règles de §10.7 et non celles d'un format ----------

// TestTheSamePhotoOnTwoProductsIsOneFile.
//
// The sha IS the address, which is what turns 181 rows carrying a photo into 165 files
// written — and what makes a re-import write nothing at all (§10.7).
func TestTheSamePhotoOnTwoProductsIsOneFile(t *testing.T) {
	shared := pngOf(t, 8, 8)
	rows := manyGoodRows(t, 10)
	rows[0].Image, rows[1].Image = shared, shared
	sink := newCollecting()

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
		KeepPhotos: true, Images: sink})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Images) != 1 {
		t.Fatalf("%d image(s) dans le lot, attendu 1", len(batch.Images))
	}
	if len(sink.put) != 1 {
		t.Fatalf("%d empreinte(s) écrite(s), attendu 1", len(sink.put))
	}
	for sha, times := range sink.put {
		if times != 1 {
			t.Errorf("la photo %s a été écrite %d fois", sha[:8], times)
		}
	}
	if batch.Products[0].ImageSHA == "" || batch.Products[0].ImageSHA != batch.Products[1].ImageSHA {
		t.Errorf("les deux produits ne partagent pas l'adresse : %q et %q",
			batch.Products[0].ImageSHA, batch.Products[1].ImageSHA)
	}
}

// TestAPhotoRefusedLosesItsPhotoAndNeverItsProduct: les deux refus de §10.7 sont NON
// BLOQUANTS — le produit garde sa tuile dans les deux cas.
func TestAPhotoRefusedLosesItsPhotoAndNeverItsProduct(t *testing.T) {
	for _, c := range []struct {
		name    string
		image   []byte
		ceiling int
		want    string
	}{
		{"au-delà du plafond", pngOf(t, 64, 64), 64, domain.FindingImageTooLarge},
		{"en-tête d'aucun format accepté", []byte("ceci n'est pas une image"), 0,
			domain.FindingImageInvalid},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := manyGoodRows(t, 10)
			rows[0].Image = c.image
			batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
				KeepPhotos: true, MaxImageSize: c.ceiling})
			if err != nil {
				t.Fatalf("Assemble : %v", err)
			}
			if len(batch.Products) != 10 ||
				batch.Products[0].Qualification != domain.Weighable {
				t.Fatal("le produit a perdu sa tuile en même temps que sa photo")
			}
			if batch.Products[0].ImageSHA != "" || len(batch.Images) != 0 {
				t.Fatal("une photo refusée a été adressée quand même")
			}
			if !hasCode(batch.Findings, c.want) {
				t.Errorf("le refus n'est pas classé %s : %v", c.want, codesOf(batch.Findings))
			}
		})
	}
}

// TestAStationThatKeepsNoPhotoOpensNone: `catalog.images.source` à autre chose que les
// photos de la source, et rien n'est décodé ni écrit.
func TestAStationThatKeepsNoPhotoOpensNone(t *testing.T) {
	rows := manyGoodRows(t, 10)
	rows[0].Image = pngOf(t, 8, 8)
	sink := newCollecting()

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
		KeepPhotos: false, Images: sink})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Images) != 0 || len(sink.put) != 0 || batch.Products[0].ImageSHA != "" {
		t.Fatal("une photo a été retenue sur un poste qui n'en garde aucune")
	}
}

// TestASinkThatRefusesLeavesTheProductWithoutItsPhoto: un disque plein dégrade le
// confort, jamais le service (principe 6 de §4).
func TestASinkThatRefusesLeavesTheProductWithoutItsPhoto(t *testing.T) {
	rows := manyGoodRows(t, 10)
	rows[0].Image = pngOf(t, 8, 8)

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{
		KeepPhotos: true, Images: &collecting{put: map[string]int{}, refuse: true}})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Products) != 10 || batch.Products[0].ImageSHA != "" {
		t.Fatal("le produit a suivi le sort de sa photo")
	}
	if !hasCode(batch.Findings, domain.FindingImageInvalid) {
		t.Errorf("le refus du puits n'est pas signalé : %v", codesOf(batch.Findings))
	}
}

// TestANilSinkCountsThePhotosAndKeepsNone: c'est exactement ce que veut une lecture à
// blanc du rapport d'import (§10.3 bis).
func TestANilSinkCountsThePhotosAndKeepsNone(t *testing.T) {
	rows := manyGoodRows(t, 10)
	rows[0].Image = pngOf(t, 8, 8)

	batch, err := catalog.Assemble(reads(rows...), catalog.AssembleOptions{KeepPhotos: true})
	if err != nil {
		t.Fatalf("Assemble : %v", err)
	}
	if len(batch.Images) != 1 || batch.Products[0].ImageSHA == "" {
		t.Fatal("un puits nul doit compter la photo et l'adresser sans l'écrire")
	}
}

// --- Ce que les options par défaut valent ----------------------------------------

// TestABareOptionsIsAUsableAssembler: un appelant qui ne remplit rien reçoit les gardes
// que la spécification livre, jamais l'absence de garde.
func TestABareOptionsIsAUsableAssembler(t *testing.T) {
	reader := &scripted{steps: []step{
		{row: goodRow(t, 0)},
		{err: catalog.ErrRowUnreadable},
		{err: catalog.ErrRowUnreadable},
	}}
	if _, err := catalog.Assemble(reader, catalog.AssembleOptions{}); err == nil {
		t.Fatal("une AssembleOptions vide n'applique aucune garde absolue")
	}
}

// TestTheOptionsAreReadFromTheConfigurationOnce: les clés que l'assembleur applique
// sont lues là où elles sont appliquées (ADR-042).
func TestTheOptionsAreReadFromTheConfigurationOnce(t *testing.T) {
	options := catalog.AssembleOptionsFrom(domain.CatalogConfig{
		Options: mustOptions(t, `{"max_image_size_kb":32,"min_readable_ratio":0.5}`),
		Images:  domain.ImagesConfig{Source: domain.ImageSourceNone},
	})
	if options.MaxImageSize != 32<<10 {
		t.Errorf("MaxImageSize = %d, attendu %d", options.MaxImageSize, 32<<10)
	}
	if options.MinReadableRatio != 0.5 {
		t.Errorf("MinReadableRatio = %v, attendu 0,5", options.MinReadableRatio)
	}
	if options.KeepPhotos {
		t.Error("un poste réglé sur `none` garde quand même les photos")
	}

	// Et les valeurs livrées quand la configuration ne porte rien : une clé absente ne
	// veut jamais dire « aucune limite ».
	shipped := catalog.AssembleOptionsFrom(domain.CatalogConfig{})
	if shipped.MaxImageSize != catalog.DefaultMaxImageSizeKB<<10 ||
		shipped.MinReadableRatio != catalog.DefaultMinReadableRatio {
		t.Errorf("les valeurs livrées ne sont pas reprises : %+v", shipped)
	}
	if !shipped.KeepPhotos {
		t.Error("une configuration muette doit garder les photos, comme le fichier livré")
	}
}

// --- Outils de ces tests ---------------------------------------------------------

// mustOptions reads a block of catalog.options the way a configuration file carries it.
func mustOptions(t *testing.T, raw string) domain.DriverOptions {
	t.Helper()
	var options domain.DriverOptions
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatalf("options %s : %v", raw, err)
	}
	return options
}

// hasCode reports whether a motive was raised.
func hasCode(findings []domain.Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// codesOf lists the motives, for a failure message that names them all.
func codesOf(findings []domain.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Code)
	}
	return out
}

// pngOf builds a PNG of the requested size, so that a test about a CEILING carries a
// real image rather than bytes that would be refused for their header instead.
func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	// Pseudo-random noise, and both words matter. NOISE, because PNG squeezes a regular
	// picture down to almost nothing and a test about a size ceiling would never reach
	// it. PSEUDO-random, from a generator seeded here, because a test whose subject
	// varies from one run to the next is a test that fails on somebody else's machine.
	seed := uint32(1)
	next := func() uint8 {
		seed = seed*1664525 + 1013904223
		return uint8(seed >> 24)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: next(), G: next(), B: next(), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encodage du PNG d'essai : %v", err)
	}
	return encoded.Bytes()
}
