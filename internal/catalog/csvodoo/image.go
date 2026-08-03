package csvodoo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// unwrap turns one base64 field into the bytes of a photo, and it is ALL this adapter
// knows about images.
//
// Everything else §10.7 decides — the four accepted headers, the ceiling on the decoded
// size, the bound on the dimensions, the sha that addresses the file and the two findings
// that name a refusal — belongs to catalog, because none of it is a fact about the
// exchange format. A photo that arrived in a base64 column and one an ERP handed over as
// bytes are judged by the same rules, once.
//
// The bytes go through a LIMITED reader rather than through a full decode followed by a
// length test: a field claiming three megabytes is refused after 256 kB have been read,
// not after three megabytes have been allocated. It deliberately stops at max+1 rather
// than at max — that extra byte is what lets the assembler tell « exactly at the
// ceiling » from « past it » and name the ceiling in the finding.
func unwrap(encoded string, ceiling int) ([]byte, error) {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	var decoded bytes.Buffer
	decoded.Grow(min(len(encoded)*3/4+3, ceiling+1))
	if _, err := io.Copy(&decoded, io.LimitReader(decoder, int64(ceiling)+1)); err != nil {
		return nil, fmt.Errorf("le champ image n'est pas du base64 lisible (%v)", err)
	}
	return decoded.Bytes(), nil
}
