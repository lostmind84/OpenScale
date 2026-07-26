package catalog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// fixture is where the authentic exchange files live.
//
// ONE copy in the repository, the one CLAUDE.md names. Copying 527 kB a second time
// under this package would create a second truth, and the day the two diverge the
// tests would keep passing against the wrong one.
const fixture = "../../testdata/catalog/"

// TestAPhotoIsAddressedByItsContentAndItsRealFormat is §10.7 in one assertion.
//
// The path is <2 first characters of the sha>/<sha>.<extension of the DETECTED
// format>, and the extension can never contradict the bytes: the legacy application
// wrote <id>_image.jpg whatever it had decoded, and 10 of the 181 photos of the
// reference file are PNGs called .jpg.
func TestAPhotoIsAddressedByItsContentAndItsRealFormat(t *testing.T) {
	store, err := catalog.NewImageStore(t.TempDir())
	if err != nil {
		t.Fatalf("puits d'images : %v", err)
	}
	const sha = "0badc0de0badc0de0badc0de0badc0de0badc0de0badc0de0badc0de0badc0de"
	for _, c := range []struct{ format, extension string }{
		{domain.ImageJPEG, ".jpg"},
		{domain.ImagePNG, ".png"},
		{domain.ImageGIF, ".gif"},
		{domain.ImageBMP, ".bmp"},
	} {
		path, ok := store.Path(sha, c.format)
		if !ok {
			t.Fatalf("le format %s n'a pas de chemin", c.format)
		}
		want := filepath.Join(store.Directory(), "0b", sha+c.extension)
		if path != want {
			t.Errorf("chemin de %s = %q, attendu %q", c.format, path, want)
		}
	}
	if _, ok := store.Path(sha, "webp"); ok {
		t.Error("un format que le parseur ne reconnaît pas a reçu un chemin")
	}
	if _, ok := store.Path(sha[:32], domain.ImageJPEG); ok {
		t.Error("un digest tronqué a reçu un chemin : deux images pourraient le revendiquer")
	}
}

// TestWritingTheSamePhotoTwiceWritesNothing is what makes an import idempotent.
//
// Re-importing the same catalog recomputes 165 digests and writes no file at all
// (§10.7). The proof is the modification time: a second Put that rewrote the file
// would touch it.
func TestWritingTheSamePhotoTwiceWritesNothing(t *testing.T) {
	store, err := catalog.NewImageStore(t.TempDir())
	if err != nil {
		t.Fatalf("puits d'images : %v", err)
	}
	content := []byte{0xFF, 0xD8, 0xFF, 'p', 'h', 'o', 't', 'o'}
	const sha = "1122334455667788112233445566778811223344556677881122334455667788"
	if err := store.Put(sha, domain.ImageJPEG, content); err != nil {
		t.Fatalf("première écriture : %v", err)
	}
	path, _ := store.Path(sha, domain.ImageJPEG)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("la photo n'a pas été écrite : %v", err)
	}

	if err := store.Put(sha, domain.ImageJPEG, []byte("des octets differents")); err != nil {
		t.Fatalf("seconde écriture : %v", err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat : %v", err)
	}
	if !second.ModTime().Equal(first.ModTime()) || second.Size() != first.Size() {
		t.Error("la seconde écriture a touché le fichier : le sha EST le contenu, " +
			"réimporter le même catalogue ne doit écrire aucun fichier")
	}
	kept, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(kept, content) {
		t.Errorf("le contenu conservé n'est pas celui de la première écriture : %v", err)
	}
	if left := parts(t, filepath.Dir(path)); left != 0 {
		t.Errorf("%d fichier(s) temporaire(s) laissés derrière", left)
	}
}

// TestAFormatTheParserNeverProducesIsRefused: the four accepted formats are the four
// recognised from the header bytes, so anything else has already been refused upstream
// — and a sink that wrote it anyway would serve a Content-Type nobody decided.
func TestAFormatTheParserNeverProducesIsRefused(t *testing.T) {
	store, err := catalog.NewImageStore(t.TempDir())
	if err != nil {
		t.Fatalf("puits d'images : %v", err)
	}
	err = store.Put("aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
		"webp", []byte("RIFF"))
	if err == nil {
		t.Fatal("un format inattendu a été écrit sur le disque")
	}
	if !strings.Contains(err.Error(), "webp") {
		t.Errorf("le message ne nomme pas le format fautif : %v", err)
	}
}

// TestTheRealPhotosLandOnDiskUnderTheirOwnDigest reads the authentic file and writes
// what it carries.
//
// The two counts differ and the gap is the point: 181 rows of flv.csv carry a photo,
// but only 165 of them are distinct — 16 products share a picture with another —, and
// addressing by sha is what turns that into 165 files instead of 181 (§10.7).
func TestTheRealPhotosLandOnDiskUnderTheirOwnDigest(t *testing.T) {
	directory := t.TempDir()
	store, err := catalog.NewImageStore(directory)
	if err != nil {
		t.Fatalf("puits d'images : %v", err)
	}
	batch := parseFixture(t, "flv.csv", store)
	report := catalog.Summarize(batch)

	if report.ImagesDecoded != 181 || report.ImagesStored != 165 {
		t.Fatalf("%d produits illustrés pour %d fichiers, attendu 181 et 165",
			report.ImagesDecoded, report.ImagesStored)
	}
	written := map[string]int{}
	for _, image := range batch.Images {
		path, ok := store.Path(image.SHA256, image.Format)
		if !ok {
			t.Fatalf("l'image %s n'a pas de chemin", image.SHA256)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("photo %s absente du disque : %v", image.SHA256, err)
		}
		if len(content) != image.ByteCount {
			t.Fatalf("photo %s : %d octets sur le disque, %d annoncés",
				image.SHA256, len(content), image.ByteCount)
		}
		// The extension follows the bytes and never the other way round.
		if got := headerFormat(content); got != image.Format {
			t.Fatalf("photo %s écrite en .%s alors que ses octets disent %s",
				image.SHA256, filepath.Ext(path), got)
		}
		written[filepath.Ext(path)]++
	}
	if total := written[".jpg"] + written[".png"]; total != 165 {
		t.Errorf("%d fichiers écrits, attendu 165 : %v", total, written)
	}
	// Measured, not assumed: 171 of the 181 FIELDS are JPEG and 10 are PNG (§10.7); the
	// 16 duplicates are 14 JPEG and 2 PNG, so 157 and 8 distinct files remain. The
	// document counts fields; the disk counts contents, and the two are not the same
	// number.
	if written[".jpg"] != 157 || written[".png"] != 8 {
		t.Errorf("répartition sur le disque %v, attendu 157 .jpg et 8 .png", written)
	}
	// Every product that carries a sha finds its file, which is the only definition of
	// « no hole in the grid » that the disk can answer for.
	for _, p := range batch.Products {
		if p.ImageSHA == "" {
			continue
		}
		if _, err := os.Stat(mustPath(t, store, p.ImageSHA, batch)); err != nil {
			t.Fatalf("le produit %s porte un sha sans fichier : %v", p.ID, err)
		}
	}
}

// parseFixture reads one of the two authentic files into the sink given.
func parseFixture(t *testing.T, name string, sink catalog.ImageSink) *ports.Batch {
	t.Helper()
	file, err := os.Open(fixture + name)
	if err != nil {
		t.Fatalf("ouverture de %s : %v", name, err)
	}
	defer file.Close()
	batch, err := csvodoo.Parse(file, csvodoo.Options{
		Source: domain.CatalogSourceLocalDrop, FileName: name,
		FallbackCategory: "other", ImageSource: domain.ImageSourceCSV, Images: sink,
	})
	if err != nil {
		t.Fatalf("lecture de %s : %v", name, err)
	}
	return batch
}

// mustPath resolves the file of a sha through the format the batch recorded for it.
func mustPath(t *testing.T, store *catalog.ImageStore, sha string, batch *ports.Batch) string {
	t.Helper()
	for _, image := range batch.Images {
		if image.SHA256 != sha {
			continue
		}
		path, ok := store.Path(sha, image.Format)
		if !ok {
			t.Fatalf("le sha %s n'a pas de chemin", sha)
		}
		return path
	}
	t.Fatalf("le sha %s n'est dans aucune ligne d'image du lot", sha)
	return ""
}

// headerFormat reports the format the first bytes of a file declare.
func headerFormat(content []byte) string {
	switch {
	case bytes.HasPrefix(content, []byte{0xFF, 0xD8, 0xFF}):
		return domain.ImageJPEG
	case bytes.HasPrefix(content, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return domain.ImagePNG
	case bytes.HasPrefix(content, []byte("GIF8")):
		return domain.ImageGIF
	case bytes.HasPrefix(content, []byte("BM")):
		return domain.ImageBMP
	}
	return "inconnu"
}

// parts counts the half-written files left in a directory.
func parts(t *testing.T, directory string) int {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("lecture de %s : %v", directory, err)
	}
	n := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			n++
		}
	}
	return n
}
