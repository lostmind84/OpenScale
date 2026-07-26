package catalog

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"openscale/internal/domain"
)

// extensions is the name a format is written under, and the mapping only ever goes
// this way.
//
// The legacy application wrote <id>_image.jpg whatever it had decoded, so 10 of the
// 181 photos of the reference file are PNGs called .jpg. Here the header bytes decide
// the format, the format decides the extension, and there is no path by which the two
// can diverge (§10.7a).
var extensions = map[string]string{
	domain.ImageJPEG: ".jpg",
	domain.ImagePNG:  ".png",
	domain.ImageGIF:  ".gif",
	domain.ImageBMP:  ".bmp",
}

// fanOut is how many characters of the sha name the sub-directory.
//
// Two, as §10.7 writes it: 256 directories for a parc measured at 165 distinct photos
// leaves a handful of files each, and a directory listing stays something a volunteer
// can read over somebody's shoulder.
const fanOut = 2

// digestLength is the hexadecimal form of a sha256, and it is checked rather than
// assumed: the address of a photo is its whole digest, and a truncated one would name
// a file that two different images could claim.
const digestLength = sha256.Size * 2

// ImageStore keeps the photos an import decoded, addressed BY THEIR CONTENT.
//
// Addressing by sha is what makes an import idempotent: re-importing the same catalog
// recomputes 165 digests and writes no file at all (§10.7). It is also what removes
// the seven-day garbage collector — an image is reachable as long as a product carries
// its sha, and a product is no longer destroyed at every import (§10.9).
type ImageStore struct {
	root string
}

// ImageDirectory reports where a station keeps its photos.
//
// One declaration of the fact, because two spellings of the same path is the failure
// the legacy application died of: the HTTP route that serves an image and the import
// that writes one must not be able to disagree.
func ImageDirectory(dataDir string) string { return filepath.Join(dataDir, "images") }

// NewImageStore prepares the directory of <data>/images.
//
// It creates it, like the local drop creates the directory it watches: a station that
// has never imported anything must still show an existing, named path.
func NewImageStore(dataDir string) (*ImageStore, error) {
	root := ImageDirectory(dataDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("répertoire des images %s : %w", root, err)
	}
	return &ImageStore{root: root}, nil
}

// Directory reports the root the photos are kept under.
func (s *ImageStore) Directory() string { return s.root }

// Path reports where the photo of a sha in a given format is kept.
//
// An unknown format has no path: the four accepted ones are the four the parser
// recognises from their header bytes, and anything else was refused before it ever got
// here (§10.7a).
func (s *ImageStore) Path(sha, format string) (string, bool) {
	extension, known := extensions[format]
	if !known || len(sha) != digestLength {
		return "", false
	}
	return filepath.Join(s.root, sha[:fanOut], sha+extension), true
}

// Put writes one photo, and writing the same content twice is a NO-OP.
//
// The write is a temporary file, an fsync and a rename, in that order and inside the
// same directory so that the rename never crosses a device. That is what corrects the
// corrupted JPEG of the legacy application, which reopened the target `For Binary`
// without truncating it and left the tail of the previous image behind whenever the
// new one was shorter (§10.7).
func (s *ImageStore) Put(sha, format string, content []byte) error {
	path, ok := s.Path(sha, format)
	if !ok {
		return fmt.Errorf("image %s : format %q inattendu", sha, format)
	}
	if _, err := os.Stat(path); err == nil {
		// The name IS the content: a file that is already there is already right, and
		// this is the line that makes a re-import write nothing.
		return nil
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("répertoire d'images %s : %w", directory, err)
	}

	temporary, err := os.CreateTemp(directory, sha+".*.part")
	if err != nil {
		return fmt.Errorf("écriture de l'image %s : %w", sha, err)
	}
	name := temporary.Name()
	if err := write(temporary, content); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("écriture de l'image %s : %w", sha, err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("nommage de l'image %s : %w", sha, err)
	}
	return nil
}

// write fills a file and makes sure the bytes really are on the disk.
//
// The fsync is not belt and braces here: the source file is removed as soon as the
// batch is acknowledged, so a photo that only exists in the page cache is a photo a
// power cut loses for good, with nothing left to re-read it from.
func write(file *os.File, content []byte) error {
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Compile-time proof that the disk store is the sink the parser writes to.
var _ ImageSink = (*ImageStore)(nil)
