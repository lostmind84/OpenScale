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
