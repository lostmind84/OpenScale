package csvodoo

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"strings"

	// The four formats §10.7 accepts, registered so that image.DecodeConfig can read
	// the header of each. Nothing else is registered, so nothing else can slip in
	// through a format this application never decided to serve.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"

	"openscale/internal/catalog"
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

// imageFault is why a photo was refused, and which of the two findings says so.
//
// Both are NON-BLOCKING and they differ only in the sentence a volunteer reads: the
// product keeps its tile in either case, and loses its photo (§10.7).
type imageFault struct {
	// tooLarge separates "this is not an image" from "this image is too big", which
	// call for different actions in Odoo.
	tooLarge bool
	why      string
}

// Error reports the French reason, which is also what goes into the finding.
func (f imageFault) Error() string { return f.why }

// decode turns one base64 field into an addressable photo.
//
// The bytes go through a LIMITED reader rather than through a full decode followed by
// a length test: a field claiming three megabytes is refused after 256 kB have been
// read, not after three megabytes have been allocated.
func (o Options) decode(encoded string) (domain.Image, []byte, error) {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	var decoded bytes.Buffer
	decoded.Grow(min(len(encoded)*3/4+3, o.MaxImageSize+1))
	size, err := io.Copy(&decoded, io.LimitReader(decoder, int64(o.MaxImageSize)+1))
	if err != nil {
		return domain.Image{}, nil, imageFault{
			why: fmt.Sprintf("le champ image n'est pas du base64 lisible (%v)", err)}
	}
	if size > int64(o.MaxImageSize) {
		return domain.Image{}, nil, imageFault{tooLarge: true, why: fmt.Sprintf(
			"elle dépasse le plafond de %d ko une fois décodée, quand la plus grosse du "+
				"catalogue de référence en pèse 12", o.MaxImageSize>>10)}
	}

	content := decoded.Bytes()
	format, recognised := sniff(content)
	if !recognised {
		return domain.Image{}, nil, imageFault{why: "son en-tête n'est celui d'aucun des " +
			"quatre formats acceptés"}
	}

	// The dimensions are read from the CONFIG, before a single pixel is decoded: an
	// image that declares 40 000 pixels a side costs nothing to declare and gigabytes
	// to decode (§10.7b).
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return domain.Image{}, nil, imageFault{why: fmt.Sprintf(
			"son en-tête annonce un %s que le décodeur refuse (%v)", format, err)}
	}
	if config.Width > maxImageEdge || config.Height > maxImageEdge {
		return domain.Image{}, nil, imageFault{tooLarge: true, why: fmt.Sprintf(
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
		SeenAt:    o.Now,
	}, content, nil
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

// imageRefused turns a refusal into the finding that names it.
func imageRefused(row catalog.Row, err error) domain.Finding {
	var fault imageFault
	if errors.As(err, &fault) && fault.tooLarge {
		return catalog.ImageTooLarge(row, fault.why)
	}
	return catalog.ImageInvalid(row, err.Error())
}
