package web

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"openscale/internal/domain"
)

// pngBytes is the smallest thing that starts with the PNG signature. The route serves
// bytes and checks a name; it decodes nothing, so nothing more is needed.
var pngBytes = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("corps")...)

// TestAPhotoIsServedUnderTheExtensionOfItsDetectedFormat.
//
// The extension of the source file is worth nothing: the legacy application wrote
// <id>_image.jpg whatever it had decoded, and ten of the 181 real images are PNGs
// behind a .jpg name. Here the name DERIVES from the detected format, so a header byte
// and a file name cannot diverge.
func TestAPhotoIsServedUnderTheExtensionOfItsDetectedFormat(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	b := imageBench(t, sha, domain.ImagePNG)

	response := b.get("/images/" + sha + ".png")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /images/%s.png = %d", sha, response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, attendu image/png", got)
	}
	if got := response.Header.Get("Cache-Control"); got != imageCacheControl {
		t.Fatalf("Cache-Control = %q, attendu %q", got, imageCacheControl)
	}
	if got := response.Header.Get("ETag"); got != `"`+sha+`"` {
		t.Fatalf("ETag = %q, attendu l'empreinte du contenu", got)
	}
}

// TestAPhotoRequestedUnderTheWrongExtensionIsNotFound is the rule of §10.7 stated as a
// refusal: serving the bytes anyway is exactly what put PNGs behind a .jpg name.
func TestAPhotoRequestedUnderTheWrongExtensionIsNotFound(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	b := imageBench(t, sha, domain.ImagePNG)
	// The same bytes are ALSO on disk under the three other names, so that the only
	// thing left that can refuse is the comparison against the stored format. Without
	// this, the file simply not being there would answer 404 and the test would pass
	// even with the comparison deleted — which is exactly what a mutation showed.
	for _, extension := range []string{"jpg", "gif", "bmp"} {
		b.putImageFile(sha, extension)
	}

	for _, name := range []string{sha + ".jpg", sha + ".gif", sha + ".bmp"} {
		response := b.get("/images/" + name)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET /images/%s = %d, attendu 404 (le format stocké est png)",
				name, response.StatusCode)
		}
	}
}

// TestAnUnservableImageNameIsRefusedBeforeAnythingIsOpened.
//
// The sha is checked to be 64 hexadecimal characters BEFORE it becomes a path: that,
// and not a string replacement, is what makes a traversal impossible.
func TestAnUnservableImageNameIsRefusedBeforeAnythingIsOpened(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	b := imageBench(t, sha, domain.ImagePNG)

	for _, name := range []string{
		"sans-extension",
		strings.Repeat("zz", 32) + ".png",
		strings.Repeat("ab", 31) + ".png",
		sha + ".exe",
		strings.Repeat("cd", 32) + ".png",
	} {
		response := b.get("/images/" + name)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET /images/%s = %d, attendu 404", name, response.StatusCode)
		}
	}
}

// TestNoNameThatIsNotASHAEverBecomesAPath checks the guard at its own level.
//
// It is not tested over HTTP because the client, the mux and path.Clean would each
// have rewritten « ../ » long before the handler saw it — so a green request would
// prove that somebody else defended us, and this function is what has to.
func TestNoNameThatIsNotASHAEverBecomesAPath(t *testing.T) {
	for _, name := range []string{
		"../../secret.png", "..%2f..%2fsecret.png", "/etc/passwd.png",
		strings.Repeat("AB", 32) + ".png", // uppercase is not the spelling we store
		strings.Repeat("ab", 32) + ".PNG",
		"", ".png", strings.Repeat("ab", 32),
	} {
		if _, _, ok := splitImageName(name); ok {
			t.Errorf("splitImageName(%q) a accepté un nom qui n'est pas {sha}.{ext}", name)
		}
	}
	sha := strings.Repeat("ab", 32)
	got, extension, ok := splitImageName(sha + ".jpg")
	if !ok || got != sha || extension != "jpg" {
		t.Fatalf("splitImageName(%q) = %q, %q, %v", sha+".jpg", got, extension, ok)
	}
	if want := sha[:2] + "/" + sha + ".jpg"; imagePath(sha, "jpg") != want {
		t.Fatalf("imagePath = %q, attendu %q", imagePath(sha, "jpg"), want)
	}
}

// TestAPhotoIsRevalidatedByItsContent: the URL carries the hash, so the bytes behind it
// can never change and a revalidation is always a 304.
func TestAPhotoIsRevalidatedByItsContent(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	b := imageBench(t, sha, domain.ImagePNG)

	response := b.do(http.MethodGet, "/images/"+sha+".png", "",
		http.Header{"If-None-Match": {`"` + sha + `"`}})
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation = %d, attendu 304", response.StatusCode)
	}
}

// TestAStationWithoutPhotosSaysSoRatherThanPretending.
func TestAStationWithoutPhotosSaysSoRatherThanPretending(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.noStore = true
		o.images = nil
	})
	response := b.get("/images/" + strings.Repeat("ab", 32) + ".png")
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotImplemented {
		t.Fatalf("statut = %d, attendu 501", response.StatusCode)
	}
}

// TestTheClientScreenAnswersEvenWithoutABundle: a station whose front end was not built
// still has to say something to the person standing in front of it.
func TestTheClientScreenAnswersEvenWithoutABundle(t *testing.T) {
	b := newBench(t)
	for _, path := range []string{"/", "/admin", "/admin/"} {
		response := b.get(path)
		text := body(t, response)
		if response.StatusCode != http.StatusOK || !strings.Contains(text, "Le service répond") {
			t.Errorf("GET %s = %d : %q", path, response.StatusCode, text)
		}
	}
}

// TestTheBuiltBundleIsServedWhenItIsThere.
func TestTheBuiltBundleIsServedWhenItIsThere(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.assets = fstest.MapFS{
			"index.html":     {Data: []byte("<!doctype html><title>client</title>")},
			"admin.html":     {Data: []byte("<!doctype html><title>admin</title>")},
			"assets/app.js":  {Data: []byte("export const version = 1")},
			"assets/app.css": {Data: []byte(":root{}")},
		}
	})
	if got := body(t, b.get("/")); !strings.Contains(got, "client") {
		t.Fatalf("GET / = %q", got)
	}
	if got := body(t, b.get("/admin")); !strings.Contains(got, "admin") {
		t.Fatalf("GET /admin = %q", got)
	}
	if got := body(t, b.get("/assets/app.js")); !strings.Contains(got, "version") {
		t.Fatalf("GET /assets/app.js = %q", got)
	}
	missing := b.get("/assets/absent.js")
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /assets/absent.js = %d, attendu 404", missing.StatusCode)
	}
}

// TestAnUnknownAPIPathAnswersInJSON: an API that answers a route with an HTML page
// teaches a front end to parse HTML.
func TestAnUnknownAPIPathAnswersInJSON(t *testing.T) {
	b := newBench(t)
	for _, path := range []string{"/api/v1/inconnu", "/admin/api/inconnu"} {
		response := b.get(path)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, attendu 404", path, response.StatusCode)
		}
		if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("GET %s : Content-Type = %q", path, got)
		}
		response.Body.Close()
	}
}

// imageBench is a station with exactly one photo on disk and in the store.
func imageBench(t *testing.T, sha, format string) *bench {
	t.Helper()
	files := fstest.MapFS{}
	b := newBench(t, func(o *benchOptions) { o.images = files })
	b.imageFiles = files
	b.putImageFile(sha, extensionOf[format])
	b.store.images[sha] = domain.Image{
		SHA256: sha, Format: format, ByteCount: len(pngBytes), Width: 64, Height: 64,
	}
	return b
}

// putImageFile writes the same bytes under one more name, which is what lets a test
// isolate the NAME check from the mere absence of a file.
func (b *bench) putImageFile(sha, extension string) {
	b.imageFiles[imagePath(sha, extension)] = &fstest.MapFile{Data: pngBytes}
}

// TestTheEmbeddedBundleCarriesBothEntryPoints (§14.1).
//
// Two separate entry points, and the separation is about WEIGHT and LOADING, not about
// execution: on a station where nobody ever opens the administration screen — the
// nominal case, all day long — not one byte of it is downloaded, parsed or run.
func TestTheEmbeddedBundleCarriesBothEntryPoints(t *testing.T) {
	assets := Assets()
	for _, name := range []string{"index.html", "admin.html"} {
		raw, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatalf("le binaire ne porte pas %s : %v", name, err)
		}
		if !strings.Contains(string(raw), "<!doctype html>") {
			t.Errorf("%s ne ressemble pas à un document HTML", name)
		}
	}
}
