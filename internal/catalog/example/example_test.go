package example

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// epoch is the instant every test starts at. A fixed one, so that a photo stamped by the
// injected clock reads the same from one run to the next.
var epoch = time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)

// The reference vector of §18: reference 0493021000003 is AIL VIOLET SAF, sold by weight,
// and it is the product every test of this repository weighs.
const (
	garlicID      = "4412"
	garlicBarcode = "0493021000003"
)

// --- The producer this package pretends to read ---------------------------------

// producer is a fake ERP: a list of pages, and a record of what was asked of it.
type producer struct {
	// pages are the raw bodies, one per page number starting at 1. Raw, and not a
	// structure to marshal, because two tests are ABOUT the exact bytes: key order and
	// whitespace must not change the identity of a catalog.
	pages []string
	// status, when non-zero, is answered instead of a page.
	status int
	// asked records every page number the station requested, in order.
	asked []int
	// token records the credential presented on the last request.
	token string
}

// serve starts the fake ERP and stops it when the test ends.
func (p *producer) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		number, _ := strconv.Atoi(r.URL.Query().Get("page"))
		p.asked = append(p.asked, number)
		if p.status != 0 {
			w.WriteHeader(p.status)
			w.Write([]byte("<html>erreur interne</html>"))
			return
		}
		if number < 1 || number > len(p.pages) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(p.pages[number-1]))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/api/products"
}

// onePage is the ordinary answer: every product, and no page after it.
func onePage(products ...string) []string {
	return []string{fmt.Sprintf(`{"products":[%s],"next_page":0}`, strings.Join(products, ","))}
}

// product spells one record the way the fake ERP publishes it.
func product(id, name, barcode, price, category, unit string) string {
	return fmt.Sprintf(
		`{"id":%q,"name":%q,"barcode":%q,"price":%q,"category":%q,"unit":%q,"photo":""}`,
		id, name, barcode, price, category, unit)
}

// garlic is the reference product, weighable and complete.
func garlic() string {
	return product(garlicID, "AIL VIOLET SAF", garlicBarcode, "12.90", "L", "kg")
}

// --- The station side -----------------------------------------------------------

// newSource builds the source against a fake ERP, with the shipped options unless a test
// says otherwise.
func newSource(t *testing.T, address string, options ...string) *Source {
	t.Helper()
	declared := append([]string{fmt.Sprintf("%q:%q", URLOption, address)}, options...)
	source, err := New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          driverOptions(t, "{"+strings.Join(declared, ",")+"}"),
			FallbackCategory: "other",
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
		},
		StationNumber: 2,
		Clock:         fake.NewClock(epoch),
	})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source
}

// driverOptions reads a block of catalog.options the way a configuration file carries it.
func driverOptions(t *testing.T, raw string) domain.DriverOptions {
	t.Helper()
	var options domain.DriverOptions
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatalf("options %s : %v", raw, err)
	}
	return options
}

// readOnce polls once and demands a batch.
func readOnce(t *testing.T, source *Source) *ports.Batch {
	t.Helper()
	batch, err := source.poll(context.Background())
	if err != nil {
		t.Fatalf("poll : %v", err)
	}
	if batch == nil {
		t.Fatal("aucun lot n'est remonté alors que l'ERP a publié un catalogue")
	}
	return batch
}

// --- The seam ------------------------------------------------------------------

// TestAPaginatedCatalogArrivesWhole.
//
// The point is not that paging works, it is WHERE it works: the pages are stitched
// together inside catalog.RowReader, so nothing downstream — not Assemble, not Qualify,
// not the importer — knows this catalog arrived in three pieces. A source that assembled
// its own batch would have had to answer §10.3 a third time.
func TestAPaginatedCatalogArrivesWhole(t *testing.T) {
	erp := &producer{pages: []string{
		fmt.Sprintf(`{"products":[%s],"next_page":2}`, garlic()),
		fmt.Sprintf(`{"products":[%s],"next_page":3}`,
			product("4413", "CAROTTE BOTTE", "0493022000000", "2.30", "L", "kg")),
		fmt.Sprintf(`{"products":[%s],"next_page":0}`,
			product("4414", "POMME GALA", "0493023000007", "3.15", "F", "kg")),
	}}
	batch := readOnce(t, newSource(t, erp.serve(t)))

	if len(batch.Products) != 3 {
		t.Fatalf("%d produits, attendu 3 sur trois pages", len(batch.Products))
	}
	if got := len(erp.asked); got != 3 {
		t.Fatalf("%d pages demandées, attendu 3 : %v", got, erp.asked)
	}
	if batch.RowsRead != 3 {
		t.Errorf("RowsRead = %d, attendu 3 : le compte traverse les pages", batch.RowsRead)
	}
	if batch.Products[0].Qualification != domain.Weighable {
		t.Errorf("le produit de référence n'est pas pesable : %q", batch.Products[0].Reason)
	}
	if batch.Products[0].Mode != domain.ByWeight {
		t.Error("le mode de vente ne vient pas du préfixe du code-barres")
	}
}

// TestTheIdentityDoesNotMoveWhenTheSerialisationDoes.
//
// This is the test the whole design of catalog.Fingerprint exists for. The same catalog,
// published twice with different key order, different whitespace and one extra field this
// station does not read, MUST arrive with the same identity — otherwise « le même
// catalogue deux fois » stops being the nominal case of §10.5: every poll would rewrite
// the grid under a customer's finger, and the quarantine would never count one content
// refused three times.
//
// A digest of the BYTES would fail this test, and that is exactly why the bytes are only
// used to name a REFUSAL.
func TestTheIdentityDoesNotMoveWhenTheSerialisationDoes(t *testing.T) {
	tidy := &producer{pages: onePage(garlic())}
	untidy := &producer{pages: []string{fmt.Sprintf(
		"{\n  \"next_page\" : 0,\n  \"products\" : [ {\n"+
			"    \"barcode\": %q,\n    \"unit\": \"kg\",\n    \"name\": %q,\n"+
			"    \"price\": %q,\n    \"id\": %q,\n    \"category\": \"L\",\n"+
			"    \"photo\": \"\",\n    \"updated_by\": \"jean\"\n  } ]\n}",
		garlicBarcode, "AIL VIOLET SAF", "12.90", garlicID)}}

	first := readOnce(t, newSource(t, tidy.serve(t)))
	second := readOnce(t, newSource(t, untidy.serve(t)))

	if first.ID == "" {
		t.Fatal("le lot n'a pas d'identité : la quarantaine de §10.5 compte par contenu")
	}
	if first.ID != second.ID {
		t.Fatalf("deux sérialisations du même catalogue ont deux identités :\n%s\n%s",
			first.ID, second.ID)
	}
	if first.Bytes == second.Bytes {
		t.Error("les deux réponses pèsent le même nombre d'octets : le cas n'est pas exercé")
	}
}

// TestAChangedPriceChangesTheIdentity is the other half: an identity that never moves is
// an identity that hides an update.
func TestAChangedPriceChangesTheIdentity(t *testing.T) {
	before := &producer{pages: onePage(garlic())}
	after := &producer{pages: onePage(
		product(garlicID, "AIL VIOLET SAF", garlicBarcode, "13.50", "L", "kg"))}

	if readOnce(t, newSource(t, before.serve(t))).ID ==
		readOnce(t, newSource(t, after.serve(t))).ID {
		t.Fatal("un prix qui change ne change pas l'identité du catalogue")
	}
}

// --- Acknowledgement, which is what a source with no file has to invent ----------

// TestAnAppliedCatalogIsNotDownloadedAgain.
//
// A file source acquits by DELETING and the next poll finds nothing. This one has nothing
// to delete, so the acknowledgement is a watermark — and without it a station would fetch
// a producer's whole catalog every five minutes to conclude it already had it.
func TestAnAppliedCatalogIsNotDownloadedAgain(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t))

	batch := readOnce(t, source)
	if err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("Acknowledge : %v", err)
	}

	again, err := source.poll(context.Background())
	if err != nil {
		t.Fatalf("poll : %v", err)
	}
	if again != nil {
		t.Fatal("le catalogue déjà appliqué est remonté une seconde fois")
	}
}

// TestARefusedCatalogIsOfferedAgain.
//
// The asymmetry of Acknowledge, and it is load bearing. Remembering a REFUSED content
// would make the station stop asking about a catalog it never put in service: the
// quarantine of §10.5 would never see it three times, the red light would never come on,
// and the producer would fix nothing because nobody would have told them.
func TestARefusedCatalogIsOfferedAgain(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t))

	batch := readOnce(t, source)
	if err := source.Acknowledge(context.Background(), batch, ports.BatchResult{
		Result: domain.ImportRejected, Code: "ERR-CAT-03", Reason: "essai"}); err != nil {
		t.Fatalf("Acknowledge : %v", err)
	}

	again, err := source.poll(context.Background())
	if err != nil {
		t.Fatalf("poll : %v", err)
	}
	if again == nil {
		t.Fatal("un catalogue REFUSÉ n'est plus proposé : il ne sera jamais compté trois fois")
	}
}

// --- What must not lose a catalog -----------------------------------------------

// TestOneBrokenRecordDoesNotLoseThePage: a single mangled object is one row set aside and
// named, not 354 products lost (§10.4a).
//
// Ten sound records around the broken one, and the number is not padding: the absolute
// guard refuses a catalog whose readable share falls under 90 %, so a page of three
// products with one broken record is REFUSED WHOLE — correctly. Proving that one bad
// record survives its neighbours takes a page where the neighbours are the majority the
// guard is written for.
func TestOneBrokenRecordDoesNotLoseThePage(t *testing.T) {
	records := []string{`{"id":42,"name":"IDENTIFIANT NUMÉRIQUE"}`}
	for rank := 0; rank < 10; rank++ {
		records = append(records, product(
			strconv.Itoa(5000+rank), fmt.Sprintf("PRODUIT %d", rank),
			soundBarcode(t, rank), "3.15", "F", "kg"))
	}
	erp := &producer{pages: onePage(records...)}
	batch := readOnce(t, newSource(t, erp.serve(t)))

	if len(batch.Products) != 10 {
		t.Fatalf("%d produits, attendu 10 : le record cassé emporte la page", len(batch.Products))
	}
	if batch.UnreadableRows != 1 {
		t.Errorf("UnreadableRows = %d, attendu 1 : la garde absolue compte cette ligne",
			batch.UnreadableRows)
	}
	if len(batch.Findings) == 0 {
		t.Error("le record cassé ne produit aucun signalement : personne ne peut le corriger")
	}
}

// TestAPhotoTooBigLosesItsPhotoAndNotItsProduct.
//
// The ceiling belongs to catalog.Assemble and this package only stops unwrapping one byte
// past it. What the test proves is that the two halves meet: the product keeps its tile
// and loses its photo, which is what §10.7 asks for and what a customer sees.
func TestAPhotoTooBigLosesItsPhotoAndNotItsProduct(t *testing.T) {
	erp := &producer{pages: []string{fmt.Sprintf(
		`{"products":[{"id":%q,"name":"AIL VIOLET SAF","barcode":%q,"price":"12.90",`+
			`"category":"L","unit":"kg","photo":%q}],"next_page":0}`,
		garlicID, garlicBarcode, base64.StdEncoding.EncodeToString(pngOf(t, 64, 64)))}}

	// Sixteen kilobytes is the floor the schema allows; the photo is well under it.
	batch := readOnce(t, newSource(t, erp.serve(t), `"max_image_size_kb":16`))
	if len(batch.Images) != 1 {
		t.Fatalf("%d image(s) retenue(s), attendu 1", len(batch.Images))
	}
	if batch.Products[0].ImageSHA == "" {
		t.Fatal("le produit ne porte pas l'adresse de sa photo")
	}

	// The same photo against a ceiling it cannot fit under: this station accepts no image
	// below 16 kB, so the guard is exercised with a bigger picture instead.
	big := &producer{pages: []string{fmt.Sprintf(
		`{"products":[{"id":%q,"name":"AIL VIOLET SAF","barcode":%q,"price":"12.90",`+
			`"category":"L","unit":"kg","photo":%q}],"next_page":0}`,
		garlicID, garlicBarcode, base64.StdEncoding.EncodeToString(pngOf(t, 1024, 1024)))}}

	batch = readOnce(t, newSource(t, big.serve(t), `"max_image_size_kb":16`))
	if len(batch.Products) != 1 || batch.Products[0].Qualification != domain.Weighable {
		t.Fatal("le produit a perdu sa tuile en même temps que sa photo")
	}
	if batch.Products[0].ImageSHA != "" {
		t.Error("une photo au-delà du plafond a été adressée quand même")
	}
	if len(batch.Images) != 0 {
		t.Errorf("%d image(s) retenue(s), attendu 0", len(batch.Images))
	}
}

// TestAPaginationThatLoopsIsRefused: an ERP answering `next_page: 1` on page 1 would
// otherwise be polled for ever, and « le poste ne répond plus » is a symptom nobody can
// act on.
func TestAPaginationThatLoopsIsRefused(t *testing.T) {
	erp := &producer{pages: []string{
		fmt.Sprintf(`{"products":[%s],"next_page":1}`, garlic()),
	}}
	source := newSource(t, erp.serve(t))

	if batch, err := source.poll(context.Background()); batch != nil || err != nil {
		t.Fatalf("une pagination qui boucle doit être écartée sans lot : lot=%v err=%v", batch, err)
	}
	if len(erp.asked) != 1 {
		t.Errorf("%d pages demandées, attendu 1 : la boucle a été suivie", len(erp.asked))
	}
}

// TestAProducerThatIsDownDoesNotStopTheWatch: a feed that fails is a feed that will be
// asked again, and the catalog N−1 stays in service (§10.4).
func TestAProducerThatIsDownDoesNotStopTheWatch(t *testing.T) {
	erp := &producer{pages: onePage(garlic()), status: http.StatusInternalServerError}
	source := newSource(t, erp.serve(t))

	if batch, err := source.poll(context.Background()); batch != nil || err != nil {
		t.Fatalf("un ERP en panne ne remonte ni lot ni erreur fatale : lot=%v err=%v", batch, err)
	}
	erp.status = 0
	if readOnce(t, source).ID == "" {
		t.Fatal("la source n'a pas repris après la panne de l'ERP")
	}
}

// --- What a screen and a registry see -------------------------------------------

// TestTheTokenIsPresentedAndNeverDisplayed.
//
// Two facts in one test because they are two halves of the same decision: the credential
// goes on the wire, and it never goes on a screen a volunteer photographs to ask for help.
func TestTheTokenIsPresentedAndNeverDisplayed(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t), `"token":"s3cr3t-de-la-coop"`)
	readOnce(t, source)

	if erp.token != "s3cr3t-de-la-coop" {
		t.Errorf("le jeton n'a pas été présenté : %q", erp.token)
	}
	if strings.Contains(source.Describe(), "s3cr3t") {
		t.Errorf("le jeton apparaît dans ce que l'écran affiche : %s", source.Describe())
	}
}

// TestTheDescriptorDeclaresWhatTheControlsRead.
//
// The two declarations ADR-052 turned into behaviour, checked on the one source that
// exists to be copied: `url` is an OptionURL, which is what puts this source in the list
// control 39 offers, and NO key claims a drop directory, so the probe of control 46 never
// runs against it. Neither fact is written in internal/domain.
func TestTheDescriptorDeclaresWhatTheControlsRead(t *testing.T) {
	var url, drop bool
	for _, option := range Descriptor().Options {
		if option.Key == URLOption && option.Kind == domain.OptionURL {
			url = true
		}
		if option.Use == domain.UseDropDirectory {
			drop = true
		}
	}
	if !url {
		t.Errorf("%q n'est pas déclarée comme URL : le contrôle 39 ne saura pas la proposer",
			URLOption)
	}
	if drop {
		t.Error("une clé est déclarée répertoire de dépôt : le contrôle 46 sonderait un " +
			"répertoire que cette source ne surveille pas")
	}
}

// --- Le contrat que le Hub appelle vraiment ---------------------------------------

// TestNameAndDescribeSayWhatThisSourceIs.
//
// `Name` est la clé de registre qui atterrit dans `imports.source`, et `Describe` la
// phrase que l'écran d'administration affiche en permanence (§10.1).
func TestNameAndDescribeSayWhatThisSourceIs(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t))

	if source.Name() != ID {
		t.Errorf("Name() = %q, attendu %q", source.Name(), ID)
	}
	if !strings.Contains(source.Describe(), "/api/products") {
		t.Errorf("l'écran n'apprend pas ce qui est interrogé : %s", source.Describe())
	}
}

// TestNextHandsOverTheFirstCatalogItFinds.
//
// `Next` sonde AVANT d'attendre : un poste qui démarre à côté d'un ERP qui a déjà un
// catalogue ne reste pas cinq minutes avec une grille vide.
func TestNextHandsOverTheFirstCatalogItFinds(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t))

	batch, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next : %v", err)
	}
	if batch == nil || len(batch.Products) != 1 {
		t.Fatalf("le premier catalogue n'est pas remonté : %v", batch)
	}
}

// TestNextGivesTheHandBackWhenTheStationStops.
//
// La veille du catalogue est une des goroutines de §13.1, et une goroutine qui
// n'entend pas l'annulation de son contexte est une qui empêche le poste de s'arrêter.
func TestNextGivesTheHandBackWhenTheStationStops(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t))

	// Un contexte déjà annulé : le sondage échoue — ce qui n'est pas une panne, la
	// source réessaiera — puis l'attente voit l'annulation et rend la main.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	batch, err := source.Next(ctx)
	if batch != nil {
		t.Fatalf("un lot est remonté d'un contexte annulé : %v", batch)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("l'annulation n'est pas rendue telle quelle : %v", err)
	}
}

// TestFivePressesAreOneExtraPollAndNotFive.
//
// « Recharger le catalogue » est un bouton qu'un bénévole presse plusieurs fois quand
// rien ne bouge à l'écran (§14.4). L'envoi est non bloquant et la capacité est de un :
// presser ne doit jamais attendre la fin d'une lecture de 355 lignes.
func TestFivePressesAreOneExtraPollAndNotFive(t *testing.T) {
	erp := &producer{pages: onePage(garlic())}
	source := newSource(t, erp.serve(t))

	for range 5 {
		source.Wake()
	}
	if pending := len(source.wake); pending != 1 {
		t.Fatalf("%d sondages en attente après cinq pressions, attendu 1", pending)
	}
}

// TestThisSourceIsRegisteredNowhere.
//
// The point of the package, held by a test rather than by a comment: an example that
// crept into the composition root would put, in a volunteer's drop-down list, a value no
// station can honour (ADR-050).
func TestThisSourceIsRegisteredNowhere(t *testing.T) {
	config := domain.Config{Catalog: domain.CatalogConfig{Type: ID}}
	registries := domain.Registries{CatalogSources: []domain.DriverDescriptor{
		{ID: domain.CatalogSourceLocalDrop, Label: "dépôt local"},
		{ID: domain.CatalogSourceWebDAV, Label: "partage WebDAV"},
	}}

	for _, fault := range config.Validate(registries) {
		if fault.Field == "catalog.type" {
			return
		}
	}
	t.Fatalf("%q est accepté comme catalog.type : ce paquet ne doit être enregistré nulle part", ID)
}

// soundBarcode builds a weighable EAN-13 of the plan of §6.2: the `0493` prefix, a
// product reference, a reserved zone AT ZERO, and the check digit that follows from them.
//
// Composed rather than written out, because a hand-typed check digit that happens to be
// wrong turns a test about one subject into a test about INVALID_BARCODE, silently.
func soundBarcode(t *testing.T, rank int) string {
	t.Helper()
	reference, err := domain.Compose(fmt.Sprintf("0493%03d00000", 100+rank))
	if err != nil {
		t.Fatalf("composition du code-barres de rang %d : %v", rank, err)
	}
	return string(reference)
}

// pngOf builds a PNG of the requested size, so that a test about a CEILING carries a real
// image rather than bytes that would be refused for their header instead.
func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	// Pseudo-random noise, and both words matter. NOISE, because PNG squeezes a flat or
	// regular picture down to almost nothing and a test about a size ceiling would then
	// never reach it — a 1024 × 1024 gradient came out under 16 kB. PSEUDO-random, from a
	// generator seeded here, because a test whose subject varies from one run to the next
	// is a test that fails on somebody else's machine.
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
