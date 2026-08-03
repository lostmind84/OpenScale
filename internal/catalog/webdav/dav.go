package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is WebDAV on the wire: the three verbs of §10.1 — PROPFIND for the size
// and the date, GET to read, DELETE as the acknowledgement — the one request helper
// that carries the credentials and refuses a redirection off the declared host, and
// the XML a listing comes back as.

// propfind asks for the size and the date of the watched file.
//
// Depth 1 on the FOLDER rather than Depth 0 on the file, because that is the request
// a WebDAV server always answers the same way, and because a 404 on a folder listing
// tells an operator something a 404 on a file does not: the path is wrong.
func (s *Source) propfind(ctx context.Context) (catalog.Stamp, bool, error) {
	const body = `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:propfind xmlns:D="DAV:"><D:prop>` +
		`<D:getcontentlength/><D:getlastmodified/>` +
		`</D:prop></D:propfind>`

	response, err := s.do(ctx, "PROPFIND", s.folder, strings.NewReader(body), func(r *http.Request) {
		r.Header.Set("Depth", "1")
		r.Header.Set("Content-Type", "application/xml; charset=utf-8")
	})
	if err != nil {
		return catalog.Stamp{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMultiStatus && response.StatusCode != http.StatusOK {
		return catalog.Stamp{}, false, fmt.Errorf("PROPFIND %s : %s", s.folder, response.Status)
	}

	var listing multistatus
	if err := xml.NewDecoder(io.LimitReader(response.Body, maxListingBytes)).Decode(&listing); err != nil {
		return catalog.Stamp{}, false, fmt.Errorf("réponse PROPFIND illisible : %w", err)
	}
	return listing.find(s.fileName)
}

// get downloads the file and parses it as it arrives.
func (s *Source) get(ctx context.Context) (*ports.Batch, error) {
	// A copy still in flight means the previous batch was never acknowledged — a file
	// the share would not let us DELETE, downloaded again five seconds later. It is
	// thrown away rather than left behind: keeping it would hold an open handle per
	// download, and half a file in the archive directory is worse than no file at all.
	s.take().Discard()

	response, err := s.do(ctx, http.MethodGet, s.file, nil, func(r *http.Request) {
		// identity: a compressed body would make the byte count of the import record
		// and the ceiling of §10.1 measure two different things.
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		s.unreachable(err)
		return nil, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		s.unreachable(fmt.Errorf("GET %s : %s", s.file, response.Status))
		return nil, nil
	}

	pending, err := s.archive.Begin(s.fileName)
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue impossible.", err.Error())
	}
	options := s.parse
	options.Now = s.clock.Now()
	batch, err := csvodoo.Parse(io.TeeReader(response.Body, pending), options)
	if err != nil {
		s.refuse(ctx, pending, err)
		return nil, err
	}
	s.keep(pending)
	return batch, nil
}

// delete removes the file from the share. A file already gone is a success: the
// acknowledgement has taken place, whoever performed it.
func (s *Source) delete(ctx context.Context) error {
	response, err := s.do(ctx, http.MethodDelete, s.file, nil, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxListingBytes))
	switch response.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("DELETE %s : %s", s.file, response.Status)
}

// do issues one request, bounded by a budget measured on the INJECTED clock.
//
// That is what makes a test of a hanging share instantaneous instead of two minutes:
// http.Client.Timeout would read the wall clock (§16.4).
func (s *Source) do(ctx context.Context, method string, target *url.URL, body io.Reader,
	decorate func(*http.Request)) (*http.Response, error) {
	ctx, cancel := ports.WithBudget(ctx, s.clock, bodyBudget)
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		cancel()
		return nil, err
	}
	if s.username != "" {
		request.SetBasicAuth(s.username, s.password)
	}
	if decorate != nil {
		decorate(request)
	}
	response, err := s.client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	// The budget covers the BODY as well, so it is released when the body is closed
	// and not when the headers arrive.
	response.Body = &closingBody{ReadCloser: response.Body, release: cancel}
	return response, nil
}

// closingBody releases the budget of a request when its body is closed.
type closingBody struct {
	io.ReadCloser
	release context.CancelFunc
}

// Close closes the body and releases the budget, once.
func (b *closingBody) Close() error {
	err := b.ReadCloser.Close()
	if b.release != nil {
		b.release()
		b.release = nil
	}
	return err
}

// maxListingBytes bounds a PROPFIND answer. A directory listing that does not fit in
// a megabyte is not a directory listing.
const maxListingBytes = 1 << 20

// multistatus is the answer of a PROPFIND, reduced to the two properties asked for.
type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

// davResponse is one entry of the listing.
type davResponse struct {
	Href     string      `xml:"DAV: href"`
	Propstat []davStatus `xml:"DAV: propstat"`
}

// davStatus is one property block of one entry.
type davStatus struct {
	Status        string `xml:"DAV: status"`
	ContentLength string `xml:"DAV: prop>getcontentlength"`
	LastModified  string `xml:"DAV: prop>getlastmodified"`
}

// find reports the size and the date of one file of the listing.
//
// The comparison is on the LAST SEGMENT of the href and never on the whole path: a
// server is free to answer with an absolute path, a relative one or an escaped one,
// and the file name is the only part all three agree on.
func (m multistatus) find(fileName string) (catalog.Stamp, bool, error) {
	for _, entry := range m.Responses {
		href := entry.Href
		if unescaped, err := url.PathUnescape(href); err == nil {
			href = unescaped
		}
		if path.Base(strings.TrimSuffix(href, "/")) != fileName {
			continue
		}
		for _, property := range entry.Propstat {
			if property.ContentLength == "" {
				continue
			}
			size, err := strconv.ParseInt(strings.TrimSpace(property.ContentLength), 10, 64)
			if err != nil {
				return catalog.Stamp{}, false, fmt.Errorf(
					"taille annoncée %q pour %s", property.ContentLength, fileName)
			}
			modified, err := http.ParseTime(strings.TrimSpace(property.LastModified))
			if err != nil {
				// A share that does not date its files is not a reason to refuse the
				// catalog: the size alone still makes the stability rule work, it
				// just makes it slightly weaker.
				modified = time.Time{}
			}
			return catalog.Stamp{Size: size, Modified: modified}, true, nil
		}
	}
	return catalog.Stamp{}, false, nil
}
