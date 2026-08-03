package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"time"

	// The four formats §10.7 accepts, registered so that image.DecodeConfig can read
	// the header of each. Nothing else is registered, so nothing else can slip in
	// through a format this application never decided to serve.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	// Le quatrième format, qui ne vient pas de la bibliothèque standard : les exports de
	// l'ERP en portent, et sans cet enregistrement leur en-tête ne serait lu par personne.
	_ "golang.org/x/image/bmp"

	"openscale/internal/domain"
)

// signatures are the header bytes each accepted format begins with.
//
// The format is recognised HERE and never from an extension, because the extension
// lies: the legacy application wrote <id>_image.jpg whatever it had decoded, and 10
// of the 181 photos of the reference file are PNGs named .jpg. That is without
// consequence in Access, which never looks at the name; it is a real one for a
// browser and for anything that derives a type from a path (§10.7a).
var signatures = []struct {
	magic  []byte
	format string
}{
	{[]byte{0xFF, 0xD8, 0xFF}, domain.ImageJPEG},
	{[]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, domain.ImagePNG},
	{[]byte("GIF8"), domain.ImageGIF},
	{[]byte("BM"), domain.ImageBMP},
}

// maxImageEdge closes the decompression bomb: an image declaring 40 000 pixels a side
// costs nothing to declare and gigabytes to decode. It is checked on the CONFIG,
// before the pixels are ever read (§10.7b).
const maxImageEdge = 4096

// photoFault is why a photo was refused, and which of the two findings says so.
//
// Both are NON-BLOCKING and they differ only in the sentence a volunteer reads: the
// product keeps its tile in either case, and loses its photo (§10.7).
type photoFault struct {
	// tooLarge separates "this is not an image" from "this image is too big", which
	// call for different actions in Odoo.
	tooLarge bool
	why      string
}

// Error reports the French reason, which is also what goes into the finding.
func (f photoFault) Error() string { return f.why }

// describePhoto turns the bytes of one photo into the address a product carries.
//
// It lives in this package and not in an adapter because none of these rules is a fact
// about a FORMAT: the four accepted headers, the ceiling on the decoded size, the bound
// on the dimensions and the sha that addresses the file are the rules of §10.7, and they
// hold for a photo that arrived in a base64 column exactly as for one an ERP handed over
// as bytes. An adapter that owned them would own them once per adapter.
//
// The caller is expected to have stopped reading at max+1 bytes rather than to hand over
// everything it found: that is what refuses a field claiming three megabytes after 256 kB
// have been read instead of after three megabytes have been allocated. The check here is
// the second half of that guard, and it is the one that names the ceiling.
func describePhoto(content []byte, ceiling int, seenAt time.Time) (domain.Image, error) {
	if len(content) > ceiling {
		return domain.Image{}, photoFault{tooLarge: true, why: fmt.Sprintf(
			"elle dépasse le plafond de %d ko une fois décodée, quand la plus grosse du "+
				"catalogue de référence en pèse 12", ceiling>>10)}
	}

	format, recognised := sniff(content)
	if !recognised {
		return domain.Image{}, photoFault{why: "son en-tête n'est celui d'aucun des " +
			"quatre formats acceptés"}
	}

	// The dimensions are read from the CONFIG, before a single pixel is decoded: an
	// image that declares 40 000 pixels a side costs nothing to declare and gigabytes
	// to decode (§10.7b).
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return domain.Image{}, photoFault{why: fmt.Sprintf(
			"son en-tête annonce un %s que le décodeur refuse (%v)", format, err)}
	}
	if config.Width > maxImageEdge || config.Height > maxImageEdge {
		return domain.Image{}, photoFault{tooLarge: true, why: fmt.Sprintf(
			"elle mesure %d × %d pixels, au-delà de la limite de %d × %d",
			config.Width, config.Height, maxImageEdge, maxImageEdge)}
	}

	sum := sha256.Sum256(content)
	return domain.Image{
		SHA256:    hex.EncodeToString(sum[:]),
		Format:    format,
		ByteCount: len(content),
		Width:     config.Width,
		Height:    config.Height,
		SeenAt:    seenAt,
	}, nil
}

// sniff reports the format the first bytes declare.
func sniff(content []byte) (string, bool) {
	for _, s := range signatures {
		if bytes.HasPrefix(content, s.magic) {
			return s.format, true
		}
	}
	return "", false
}

// photoRefused turns a refusal into the finding that names it.
//
// Only a refusal THIS package raised can be too large, and that is why the test is on
// the type rather than on the wording: an adapter is entitled to refuse a photo for a
// reason of its own — a base64 field that is not base64 at all — and such a refusal
// lands on ImageInvalid without pretending to name a ceiling it never measured.
func photoRefused(row Row, err error) domain.Finding {
	var fault photoFault
	if errors.As(err, &fault) && fault.tooLarge {
		return ImageTooLarge(row, fault.why)
	}
	return ImageInvalid(row, err.Error())
}
