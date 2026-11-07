package catalog_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"openscale/internal/catalog"
)

var watermarkStamp = catalog.Stamp{Size: 4096, Modified: time.Date(2026, 11, 7, 9, 30, 0, 0, time.UTC)}

// openWatermark opens the watermark of a location, failing the test on error.
func openWatermark(t *testing.T, dataDir, location string) *catalog.Watermark {
	t.Helper()
	watermark, err := catalog.OpenWatermark(dataDir, location)
	if err != nil {
		t.Fatalf("ouverture du repère : %v", err)
	}
	return watermark
}

func TestAWatermarkRemembersAcrossReopening(t *testing.T) {
	dataDir := t.TempDir()
	first := openWatermark(t, dataDir, "https://a/flv.csv")
	if first.Seen(watermarkStamp) {
		t.Fatal("un repère neuf ne doit rien avoir vu")
	}
	if err := first.Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}

	reopened := openWatermark(t, dataDir, "https://a/flv.csv")
	if !reopened.Seen(watermarkStamp) {
		t.Error("le repère retenu doit survivre à une réouverture")
	}
}

func TestAWatermarkOfAnotherLocationIsEmpty(t *testing.T) {
	dataDir := t.TempDir()
	if err := openWatermark(t, dataDir, "https://a/flv.csv").Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}

	other := openWatermark(t, dataDir, "https://a/flv_1.csv")
	if other.Seen(watermarkStamp) {
		t.Error("un repère écrit pour un autre emplacement ne doit pas être repris")
	}
}

func TestAStampWithoutDateIsNeverSeen(t *testing.T) {
	watermark := openWatermark(t, t.TempDir(), "https://a/flv.csv")
	if err := watermark.Remember(catalog.Stamp{Size: 10}); err != nil {
		t.Fatalf("Remember : %v", err)
	}
	if watermark.Seen(catalog.Stamp{Size: 10}) {
		t.Error("un tampon sans date ne prouve rien : il ne doit jamais être vu")
	}
}

func TestAChangedSizeOrDateIsNotSeen(t *testing.T) {
	watermark := openWatermark(t, t.TempDir(), "https://a/flv.csv")
	if err := watermark.Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}
	bigger := catalog.Stamp{Size: watermarkStamp.Size + 1, Modified: watermarkStamp.Modified}
	later := catalog.Stamp{Size: watermarkStamp.Size, Modified: watermarkStamp.Modified.Add(time.Second)}
	if watermark.Seen(bigger) {
		t.Error("une taille différente doit se relire")
	}
	if watermark.Seen(later) {
		t.Error("une date différente doit se relire")
	}
	if !watermark.Seen(watermarkStamp) {
		t.Error("le tampon retenu doit être vu")
	}
}

func TestAnUnreadableWatermarkIsEmpty(t *testing.T) {
	dataDir := t.TempDir()
	directory := filepath.Join(dataDir, "catalog")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "watermark.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	watermark, err := catalog.OpenWatermark(dataDir, "https://a/flv.csv")
	if err != nil {
		t.Fatalf("un repère illisible doit donner un repère vide, pas une erreur : %v", err)
	}
	if watermark.Seen(watermarkStamp) {
		t.Error("un repère illisible ne doit rien avoir vu")
	}
}

func TestAWatermarkThatCannotBeWrittenStillRemembersInMemory(t *testing.T) {
	dataDir := t.TempDir()
	// A regular file where the "catalog" directory should be makes the write fail
	// whoever runs the test; directory permissions would not stop root.
	if err := os.WriteFile(filepath.Join(dataDir, "catalog"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	watermark := openWatermark(t, dataDir, "https://a/flv.csv")

	if err := watermark.Remember(watermarkStamp); err == nil {
		t.Fatal("Remember doit signaler l'échec de l'écriture")
	}
	if !watermark.Seen(watermarkStamp) {
		t.Error("après un échec d'écriture, le tampon doit rester retenu en mémoire pour ne pas relire le fichier à chaque scrutation")
	}
}

func TestForgettingAWatermarkEmptiesItInMemoryAndOnDisk(t *testing.T) {
	dataDir := t.TempDir()
	watermark := openWatermark(t, dataDir, "https://a/flv.csv")
	if err := watermark.Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}

	if err := watermark.Forget(); err != nil {
		t.Fatalf("Forget : %v", err)
	}
	if watermark.Seen(watermarkStamp) {
		t.Error("le repère oublié voit encore le tampon en mémoire")
	}
	if openWatermark(t, dataDir, "https://a/flv.csv").Seen(watermarkStamp) {
		t.Error("le repère oublié est revenu depuis le disque")
	}
}

func TestForgettingAnAbsentWatermarkIsNotAnError(t *testing.T) {
	if err := openWatermark(t, t.TempDir(), "https://a/flv.csv").Forget(); err != nil {
		t.Errorf("Forget sans enregistrement : %v", err)
	}
}

// TestForgettingErasesTheRecordWhateverItsLocation: there is one catalog source per
// station, so keep(A) then delete(B) then keep(A) must not bring A's stamp back while
// B's catalog was in service.
func TestForgettingErasesTheRecordWhateverItsLocation(t *testing.T) {
	dataDir := t.TempDir()
	if err := openWatermark(t, dataDir, "https://a/flv.csv").Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}

	if err := openWatermark(t, dataDir, "https://a/flv_1.csv").Forget(); err != nil {
		t.Fatalf("Forget : %v", err)
	}
	if openWatermark(t, dataDir, "https://a/flv.csv").Seen(watermarkStamp) {
		t.Error("le repère de l'ancien emplacement est revenu après un passage par un autre")
	}
}

const refusedDigest = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func TestAWatermarkRemembersARefusedContentAcrossReopening(t *testing.T) {
	dataDir := t.TempDir()
	first := openWatermark(t, dataDir, "https://a/flv.csv")
	if first.HoldsContent(refusedDigest) {
		t.Fatal("un repère neuf ne retient aucun contenu")
	}
	if err := first.RememberContent(refusedDigest); err != nil {
		t.Fatalf("RememberContent : %v", err)
	}
	if !first.HoldsContent(refusedDigest) {
		t.Error("le contenu retenu n'est pas retenu en mémoire")
	}
	if first.HoldsContent("autre") {
		t.Error("un autre contenu ne doit pas être retenu")
	}

	reopened := openWatermark(t, dataDir, "https://a/flv.csv")
	if !reopened.HoldsContent(refusedDigest) {
		t.Error("le contenu refusé doit survivre à une réouverture")
	}
	if openWatermark(t, dataDir, "https://a/flv_1.csv").HoldsContent(refusedDigest) {
		t.Error("le contenu retenu pour un autre emplacement ne doit pas être repris")
	}
}

func TestAnEmptyDigestIsNeverHeld(t *testing.T) {
	watermark := openWatermark(t, t.TempDir(), "https://a/flv.csv")
	if watermark.HoldsContent("") {
		t.Error("un repère neuf retient le contenu vide")
	}
	if err := watermark.Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}
	if watermark.HoldsContent("") {
		t.Error("un repère sans contenu retenu retient le contenu vide")
	}
}

func TestRememberingAStampReleasesTheHeldContent(t *testing.T) {
	dataDir := t.TempDir()
	watermark := openWatermark(t, dataDir, "https://a/flv.csv")
	if err := watermark.RememberContent(refusedDigest); err != nil {
		t.Fatalf("RememberContent : %v", err)
	}
	if err := watermark.Remember(watermarkStamp); err != nil {
		t.Fatalf("Remember : %v", err)
	}
	if watermark.HoldsContent(refusedDigest) {
		t.Error("un catalogue accepté ensuite doit libérer le contenu refusé")
	}
	if openWatermark(t, dataDir, "https://a/flv.csv").HoldsContent(refusedDigest) {
		t.Error("le contenu libéré est revenu depuis le disque")
	}
}

func TestAContentThatCannotBeWrittenStillIsHeldInMemory(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "catalog"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	watermark := openWatermark(t, dataDir, "https://a/flv.csv")

	if err := watermark.RememberContent(refusedDigest); err == nil {
		t.Fatal("RememberContent doit signaler l'échec de l'écriture")
	}
	if !watermark.HoldsContent(refusedDigest) {
		t.Error("après un échec d'écriture, le contenu doit rester retenu en mémoire")
	}
}

func TestForgettingAlsoForgetsTheHeldContent(t *testing.T) {
	dataDir := t.TempDir()
	watermark := openWatermark(t, dataDir, "https://a/flv.csv")
	if err := watermark.RememberContent(refusedDigest); err != nil {
		t.Fatalf("RememberContent : %v", err)
	}
	if err := watermark.Forget(); err != nil {
		t.Fatalf("Forget : %v", err)
	}
	if watermark.HoldsContent(refusedDigest) {
		t.Error("le contenu oublié est encore retenu en mémoire")
	}
	if openWatermark(t, dataDir, "https://a/flv.csv").HoldsContent(refusedDigest) {
		t.Error("le contenu oublié est revenu depuis le disque")
	}
}
