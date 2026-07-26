package web

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"openscale/internal/domain"
)

// imageCacheControl is a year and immutable, which is exact here and nowhere else:
// the file is ADDRESSED BY ITS CONTENT, so the bytes behind one URL can never change.
const imageCacheControl = "public, max-age=31536000, immutable"

// extensionOf maps a DETECTED format to the one extension it may be served under.
//
// The extension of the source file is worth nothing: the legacy application wrote
// <id>_image.jpg whatever it had decoded, and on the real file TEN images out of 181
// are PNGs stored as .jpg. Access never looked at the name; a browser does, and so
// does everything that derives a type from a path (§10.7).
var extensionOf = map[string]string{
	domain.ImageJPEG: "jpg",
	domain.ImagePNG:  "png",
	domain.ImageGIF:  "gif",
	domain.ImageBMP:  "bmp",
}

// contentTypeOf maps a DETECTED format to the type served with it. The two maps are
// separate because they answer two questions, and one of them has four keys that are
// not extensions.
var contentTypeOf = map[string]string{
	domain.ImageJPEG: "image/jpeg",
	domain.ImagePNG:  "image/png",
	domain.ImageGIF:  "image/gif",
	domain.ImageBMP:  "image/bmp",
}

// image is GET /images/{sha}.{ext}.
//
// # The extension is a CLAIM, and it is checked
//
// The format is the one DETECTED at import and recorded next to the bytes. A request
// for .jpg on a row that says png is a 404, never a body with the wrong
// Content-Type: there is no path by which a header byte and a file name can diverge.
func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	sha, extension, ok := splitImageName(r.PathValue("name"))
	if !ok {
		writeProblem(w, http.StatusNotFound, "", "Cette image n'existe pas.")
		return
	}
	if s.store == nil || s.images == nil {
		unavailable(w, "les photos de produits ne sont pas servies")
		return
	}

	meta, err := s.store.Image(r.Context(), sha)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "", "Cette image n'existe pas.")
		return
	}
	if extensionOf[meta.Format] != extension {
		// The stored format and the requested extension disagree. Serving the bytes
		// anyway is what put PNGs behind a .jpg name in the first place.
		writeProblem(w, http.StatusNotFound, "", "Cette image n'existe pas.")
		return
	}

	file, err := s.images.Open(imagePath(sha, extension))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "", "Cette image n'existe pas.")
		return
	}
	defer file.Close()

	etag := `"` + sha + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", imageCacheControl)
	w.Header().Set("Content-Type", contentTypeOf[meta.Format])
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if seeker, isSeeker := file.(io.ReadSeeker); isSeeker {
		// ServeContent honours Range; the ZERO instant is what stops it from writing a
		// Last-Modified header, because the content is addressed by its hash and a date
		// would add a second, weaker validator to a strong one.
		http.ServeContent(w, r, "", time.Time{}, seeker)
		return
	}
	_, _ = io.Copy(w, file)
}

// splitImageName takes {sha}.{ext} apart and refuses everything that is not one.
//
// The sha is checked to be 64 lowercase hexadecimal characters BEFORE it is used to
// build a path: that, and not a string replacement, is what makes ../ impossible.
func splitImageName(name string) (sha, extension string, ok bool) {
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return "", "", false
	}
	sha, extension = name[:dot], name[dot+1:]
	if !isSHA256(sha) || !servedExtension(extension) {
		return "", "", false
	}
	return sha, extension, true
}

// servedExtension reports whether an extension is one of the four §10.7 accepts.
func servedExtension(extension string) bool {
	for _, allowed := range extensionOf {
		if allowed == extension {
			return true
		}
	}
	return false
}

// isSHA256 reports whether s is exactly 64 lowercase hexadecimal characters.
func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// imagePath is the layout of §10.7: the two first characters of the sha as a
// directory, so that no directory holds 355 entries.
func imagePath(sha, extension string) string {
	return path.Join(sha[:2], sha+"."+extension)
}

// imageURLFor builds the address of one photo, asking the store for its format.
//
// The extension comes from the FORMAT and never from a stored name: that is the
// single rule of §10.7, and applying it here is what makes the route above able to
// refuse a mismatch instead of having to tolerate one.
func (s *Server) imageURLFor(ctx context.Context, sha string) string {
	if sha == "" || s.store == nil {
		return ""
	}
	meta, err := s.store.Image(ctx, sha)
	if err != nil {
		return ""
	}
	extension, known := extensionOf[meta.Format]
	if !known {
		return ""
	}
	return "/images/" + sha + "." + extension
}

// staticAsset serves the built front end, which is embedded in the binary.
func (s *Server) staticAsset(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		writeProblem(w, http.StatusNotFound, "", "Cette adresse n'existe pas.")
		return
	}
	http.FileServerFS(s.assets).ServeHTTP(w, r)
}

// index is GET /: the client screen.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.serveDocument(w, r, "index.html")
}

// adminIndex is GET /admin: the administration screen, a SEPARATE bundle so that it
// weighs nothing on the client screen and is not even downloaded on a station where
// nobody opens it (§14.1).
func (s *Server) adminIndex(w http.ResponseWriter, r *http.Request) {
	s.serveDocument(w, r, "admin.html")
}

// serveDocument serves one entry point of the front end.
//
// When no bundle was built into this binary it answers 200 and a page that SAYS SO,
// in French. Not a 404: the person reading it is standing in front of the screen,
// and « cette adresse n'existe pas » would send them looking for a network problem
// they do not have.
func (s *Server) serveDocument(w http.ResponseWriter, r *http.Request, name string) {
	if s.assets == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, placeholderPage)
		return
	}
	raw, err := fs.ReadFile(s.assets, name)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "", "Cette adresse n'existe pas.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The document itself is never cached: it names the hashed bundles, and a stale
	// one would point at files a new binary no longer carries.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

// placeholderPage is what a binary built without the front end shows.
const placeholderPage = `<!doctype html>
<html lang="fr"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>OpenScale</title></head>
<body style="font-family:system-ui;margin:3rem;line-height:1.6">
<h1>Le service répond</h1>
<p>L'interface n'a pas été construite dans ce binaire.</p>
<p>L'état du poste reste lisible sur
<code>/api/v1/stream</code>, <code>/healthz</code> et <code>/readyz</code>.</p>
</body></html>
`
