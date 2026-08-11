package deploy

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTheJSONCReaderLeavesTheInsideOfStringsAlone is the whole difficulty of reading a
// devcontainer.json, and the reason this reader is not three lines of strings.ReplaceAll.
//
// A devcontainer.json is JSONC: containers.dev allows comments, and this repository
// comments its configuration files at length. encoding/json refuses a comment, so they
// have to go — but a reader that hunted for "//" without knowing where the strings are
// would cut "https://containers.dev" in half and hand json.Unmarshal an unterminated
// string. The error it reports then names a line number and nothing else, and the search
// starts in the wrong file.
func TestTheJSONCReaderLeavesTheInsideOfStringsAlone(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "un commentaire de ligne disparaît, le retour à la ligne reste",
			source: "{\n  // la version vient de go.mod\n  \"a\": 1\n}\n",
			want:   "{\n  \n  \"a\": 1\n}\n",
		},
		{
			name:   "un commentaire de bloc disparaît, sur plusieurs lignes",
			source: "{/* deux\nlignes */\"a\": 1}",
			want:   `{"a": 1}`,
		},
		{
			name:   "les deux barres d'une URL ne sont pas un commentaire",
			source: `{"doc": "https://containers.dev"}`,
			want:   `{"doc": "https://containers.dev"}`,
		},
		{
			name:   "un guillemet échappé ne termine pas la chaîne",
			source: `{"a": "un guillemet \" puis // rien du tout"}`,
			want:   `{"a": "un guillemet \" puis // rien du tout"}`,
		},
		{
			name:   "l'identifiant d'un feature traverse sans une égratignure",
			source: `{"ghcr.io/devcontainers/features/go:1": {"version": "1.26.5"}}`,
			want:   `{"ghcr.io/devcontainers/features/go:1": {"version": "1.26.5"}}`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := withoutJSONComments(testCase.source); got != testCase.want {
				t.Errorf("withoutJSONComments :\n  reçu   %q\n  attendu %q", got, testCase.want)
			}
		})
	}
}

// TestTheJSONCReaderProducesSomethingEncodingJSONAccepts closes the loop: stripping the
// comments is only useful if what comes out decodes.
func TestTheJSONCReaderProducesSomethingEncodingJSONAccepts(t *testing.T) {
	source := "{\n  // le commentaire de tête\n  \"name\": \"OpenScale\", /* et un bloc */\n  \"remoteUser\": \"vscode\"\n}\n"
	var decoded struct {
		Name       string `json:"name"`
		RemoteUser string `json:"remoteUser"`
	}
	if err := jsonDecode(withoutJSONComments(source), &decoded); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if decoded.Name != "OpenScale" || decoded.RemoteUser != "vscode" {
		t.Errorf("décodé : %+v", decoded)
	}
}

// jsonDecode is the one-line wrapper the tests of this file decode with.
func jsonDecode(source string, into any) error {
	return json.Unmarshal([]byte(source), into)
}

// withoutJSONComments removes the // and /* */ comments a JSONC file is allowed to carry.
//
// See TestTheJSONCReaderLeavesTheInsideOfStringsAlone for why it tracks strings rather
// than searching for two characters.
func withoutJSONComments(source string) string {
	var out strings.Builder
	inString, inLineComment, inBlockComment, escaped := false, false, false, false
	for index := 0; index < len(source); index++ {
		character := source[index]
		switch {
		case inLineComment:
			if character == '\n' {
				inLineComment = false
				out.WriteByte(character)
			}
		case inBlockComment:
			if character == '*' && index+1 < len(source) && source[index+1] == '/' {
				inBlockComment = false
				index++
			}
		case inString:
			out.WriteByte(character)
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				inString = false
			}
		case character == '"':
			inString = true
			out.WriteByte(character)
		case character == '/' && index+1 < len(source) && source[index+1] == '/':
			inLineComment = true
			index++
		case character == '/' && index+1 < len(source) && source[index+1] == '*':
			inBlockComment = true
			index++
		default:
			out.WriteByte(character)
		}
	}
	return out.String()
}
