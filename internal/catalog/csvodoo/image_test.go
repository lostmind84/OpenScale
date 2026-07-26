package csvodoo_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
)

// collector is an ImageSink that keeps what it was handed, so a test can check that
// what the sink received is what the header bytes announced.
type collector struct {
	formats map[string]string
	sizes   map[string]int
	refuse  error
}

// newCollector returns an empty sink.
func newCollector() *collector {
	return &collector{formats: map[string]string{}, sizes: map[string]int{}}
}

// Put records one image, or refuses every one of them when refuse is set.
func (c *collector) Put(sha, format string, content []byte) error {
	if c.refuse != nil {
		return c.refuse
	}
	c.formats[sha] = format
	c.sizes[sha] = len(content)
	return nil
}

// encodedPNG returns the base64 of a real PNG of the given size.
func encodedPNG(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 200, G: 30, B: 30, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encodage PNG : %v", err)
	}
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

// encodedJPEG returns the base64 of a real JPEG.
func encodedJPEG(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatalf("encodage JPEG : %v", err)
	}
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

// oversizedPNGHeader builds the signature and the IHDR of a PNG that DECLARES huge
// dimensions and carries no pixel at all.
//
// It is the decompression bomb of §10.7b in its cheapest form: a few dozen bytes on
// the wire, gigabytes once decoded. The guard reads the declaration and stops there,
// which is the whole point of DecodeConfig.
func oversizedPNGHeader(width, height int) string {
	body := []byte("IHDR")
	body = binary.BigEndian.AppendUint32(body, uint32(width))
	body = binary.BigEndian.AppendUint32(body, uint32(height))
	body = append(body, 8, 6, 0, 0, 0) // depth 8, truecolour with alpha

	out := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)-4))
	out = append(out, body...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
	return base64.StdEncoding.EncodeToString(out)
}

// TestAPhotoIsAddressedByItsContentAndTypedByItsHeader (§10.7a).
func TestAPhotoIsAddressedByItsContentAndTypedByItsHeader(t *testing.T) {
	sink := newCollector()
	batch := parse(t, buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", encodedPNG(t, 4, 4)},
		row{"21", "AMANDES", "0493117000009", "16.05", "V", "kg", encodedJPEG(t, 8, 8)},
	), func(o *csvodoo.Options) { o.Images = sink })

	if len(batch.Images) != 2 {
		t.Fatalf("%d image(s) dans le lot, attendu 2", len(batch.Images))
	}
	formats := map[string]string{}
	for _, img := range batch.Images {
		formats[img.SHA256] = img.Format
		if sink.formats[img.SHA256] != img.Format {
			t.Errorf("le puits a reçu %q pour %s, le lot annonce %q",
				sink.formats[img.SHA256], img.SHA256, img.Format)
		}
		if sink.sizes[img.SHA256] != img.ByteCount {
			t.Errorf("%d octets remis, %d annoncés", sink.sizes[img.SHA256], img.ByteCount)
		}
	}
	if formats[batch.Products[0].ImageSHA] != domain.ImagePNG {
		t.Errorf("le premier produit porte un %q, attendu png", formats[batch.Products[0].ImageSHA])
	}
	if formats[batch.Products[1].ImageSHA] != domain.ImageJPEG {
		t.Errorf("le second produit porte un %q, attendu jpeg", formats[batch.Products[1].ImageSHA])
	}
	if batch.Images[0].Width != 4 || batch.Images[0].Height != 4 {
		t.Errorf("dimensions %d × %d, attendu 4 × 4", batch.Images[0].Width, batch.Images[0].Height)
	}
}

// TestOnePhotoOnTwoProductsIsOneFile: the sha IS the address, which is what makes a
// re-import write nothing at all (§10.7).
func TestOnePhotoOnTwoProductsIsOneFile(t *testing.T) {
	shared := encodedPNG(t, 4, 4)
	batch := parse(t, buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", shared},
		row{"21", "AMANDES", "0493117000009", "16.05", "V", "kg", shared},
	))
	if len(batch.Images) != 1 {
		t.Fatalf("%d image(s), attendu 1 pour deux produits qui partagent la photo", len(batch.Images))
	}
	if batch.Products[0].ImageSHA != batch.Products[1].ImageSHA || batch.Products[0].ImageSHA == "" {
		t.Errorf("les deux produits portent %q et %q",
			batch.Products[0].ImageSHA, batch.Products[1].ImageSHA)
	}
}

// TestARefusedPhotoLeavesTheProductInTheGrid, whatever the reason: a tile without a
// picture is 49 % of the real catalog, never a degraded case (§10.7).
func TestARefusedPhotoLeavesTheProductInTheGrid(t *testing.T) {
	for _, c := range []struct {
		what    string
		content string
		code    string
		tune    func(*csvodoo.Options)
	}{{
		what:    "un contenu qui n'est aucun des quatre formats",
		content: base64.StdEncoding.EncodeToString([]byte("ceci n'est pas une image")),
		code:    domain.FindingImageInvalid,
	}, {
		what:    "un champ qui n'est pas du base64",
		content: "ceci n'est pas du base64 !!!",
		code:    domain.FindingImageInvalid,
	}, {
		what:    "une image plus grosse que max_image_size_kb",
		content: encodedPNG(t, 64, 64),
		code:    domain.FindingImageTooLarge,
		tune:    func(o *csvodoo.Options) { o.MaxImageSize = 64 },
	}, {
		what:    "une image qui déclare 5 000 pixels de côté",
		content: oversizedPNGHeader(5000, 5000),
		code:    domain.FindingImageTooLarge,
	}} {
		t.Run(c.what, func(t *testing.T) {
			tune := []func(*csvodoo.Options){}
			if c.tune != nil {
				tune = append(tune, c.tune)
			}
			batch := parse(t, buildCSV(
				row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", c.content}), tune...)

			if len(batch.Products) != 1 || batch.Products[0].Qualification != domain.Weighable {
				t.Fatalf("le produit a perdu sa tuile en perdant sa photo")
			}
			if batch.Products[0].ImageSHA != "" {
				t.Errorf("le produit porte encore une photo : %q", batch.Products[0].ImageSHA)
			}
			if len(batch.Images) != 0 {
				t.Errorf("%d image(s) dans le lot, attendu aucune", len(batch.Images))
			}
			finding := findingWithCode(t, batch, c.code)
			if finding.Code == "" {
				t.Fatalf("aucun %s ; signalements %v", c.code, findingCodes(batch))
			}
			if finding.ProductID != "20" || finding.CSVLine != 2 {
				t.Errorf("signalement %+v : il doit nommer le produit et sa ligne", finding)
			}
		})
	}
}

// TestASinkThatRefusesCostsThePhotoAndNotTheCatalog.
func TestASinkThatRefusesCostsThePhotoAndNotTheCatalog(t *testing.T) {
	sink := newCollector()
	sink.refuse = errors.New("disque plein")
	batch := parse(t, buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", encodedPNG(t, 4, 4)}),
		func(o *csvodoo.Options) { o.Images = sink })

	if len(batch.Products) != 1 || batch.Products[0].Qualification != domain.Weighable {
		t.Fatal("un disque plein a coûté un produit")
	}
	if f := findingWithCode(t, batch, domain.FindingImageInvalid); !strings.Contains(f.Message, "disque plein") {
		t.Errorf("le signalement ne dit pas ce qui a échoué : %q", f.Message)
	}
}

// TestTheSeventhColumnIsNotEvenLookedAtWhenImagesAreOff: `none` and
// `image_directory` leave the column alone, and NEITHER raises a finding — a station
// configured without photos is not a station with 355 missing ones (§10.7).
func TestTheSeventhColumnIsNotEvenLookedAtWhenImagesAreOff(t *testing.T) {
	for _, source := range []string{domain.ImageSourceNone, domain.ImageSourceDirectory} {
		batch := parse(t, buildCSV(
			row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", "n'importe quoi"}),
			func(o *csvodoo.Options) { o.ImageSource = source })

		if len(batch.Images) != 0 || batch.Products[0].ImageSHA != "" {
			t.Errorf("%s : la colonne image a été lue", source)
		}
		if len(batch.Findings) != 0 {
			t.Errorf("%s : signalements %v, attendu aucun", source, findingCodes(batch))
		}
	}
}
