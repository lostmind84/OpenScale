package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

// This file says where, under the data directory of §11.1, the service keeps what it
// produces: the photos of the catalog, the raw frames a transport drops and the files
// a driver renders.

// imagesRoot is the photo directory of §11.1, laid out as
// <2 first characters of the sha>/<sha>.<detected extension> (§10.7).
func imagesRoot(dataDir string) string { return filepath.Join(dataDir, "images") }

// imagesDir is that directory as the HTTP layer reads it.
func imagesDir(dataDir string) fs.FS { return os.DirFS(imagesRoot(dataDir)) }

// labelsDir is where the `file` transport drops one copy per label (§11.1).
func labelsDir(dataDir string) string { return filepath.Join(dataDir, "labels") }

// previewsDir is where a driver that PRODUCES FILES writes them — today, the `preview`
// driver's PNG and PDF of each label.
//
// A directory OF ITS OWN, and not the one above. Both answer « envoyez-moi le fichier de la
// dernière étiquette », and that sentence is how support works: mixing the raw frames of a
// transport with the images of an aperçu would make it a question with two answers.
//
// KNOWN DRIFT, and it is worth stating rather than discovering: this directory is handed to
// EVERY driver, so a second file-producing driver would share it silently and re-open the
// ambiguity this split closed. There is only one such driver today. Adding a second means
// giving each its own sub-directory — the argument above is about ONE answer per question,
// not about the `preview` driver.
func previewsDir(dataDir string) string { return filepath.Join(dataDir, "previews") }
