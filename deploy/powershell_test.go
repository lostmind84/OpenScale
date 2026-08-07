package deploy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// PowerShell taken as a LANGUAGE and not as prose: a dot-sourced constant that lands on a
// parameter of its caller, a constant silently reassigned, the byte-order mark PowerShell
// 5.1 needs in order to read an accent, and the fact that every script parses. These are
// the faults that only show at run time, on the station, on a Saturday morning.

// TestNoDotSourcedConstantLandsOnAParameterOfItsCaller is the second half of the same trap,
// and the one no single file shows.
//
// A dot-source runs the sourced file IN THE CALLER'S SCOPE, and the parameters of a script
// live in that script's scope. common.ps1 sets `$script:InstallDir` and `$script:DataRoot`;
// bootstrap.ps1 declares `-InstallDir` and `-DataRoot`. Loading the first therefore
// REPLACED what the operator had asked with the factory locations — measured on a bench:
// `-InstallDir D:\OpenScale` comes back out as `C:\Program Files\OpenScale`, and the three
// branches that choose the paths always take the first. Nothing warned, and the station was
// installed somewhere else than where it had been asked to go.
//
// Renaming a parameter is not an option: -InstallDir and -DataRoot are the public names of
// two options, and TestTheInstallerDeclaresEveryParameterTheBootstrapPasses holds bootstrap
// and installer in step. What is asked is therefore put out of reach BEFORE the dot-source,
// under a name common.ps1 does not know — and what this test checks is exactly that: past
// the dot-source, the parameter is EMPTIED, so reading it is the defect. A rule about where
// the value is read survives a rename; one about how it is saved would not.
func TestNoDotSourcedConstantLandsOnAParameterOfItsCaller(t *testing.T) {
	shared := map[string]bool{}
	constant := regexp.MustCompile(`^\$script:(\w+)\s*=[^=]`)
	for _, line := range strings.Split(codeOnly(readFile(t, filepath.Join("windows", "common.ps1"))), "\n") {
		if match := constant.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			shared[strings.ToLower(match[1])] = true
		}
	}
	if len(shared) == 0 {
		t.Fatal("common.ps1 ne pose plus aucune variable de script : ce test ne prouve plus rien")
	}

	// `\$nom` cannot match inside `$requestedNom` nor behind the `-Nom` of a call: the
	// dollar sign has to touch the name.
	readers := map[string]*regexp.Regexp{}
	for name := range shared {
		readers[name] = regexp.MustCompile(`(?i)\$` + regexp.QuoteMeta(name) + `\b`)
	}

	for _, script := range []string{"bootstrap.ps1", "install.ps1", "update.ps1", "uninstall.ps1", "harden.ps1"} {
		lines := strings.Split(codeOnly(readFile(t, filepath.Join("windows", script))), "\n")
		source := -1
		for number, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), ". (") && strings.Contains(line, "common.ps1") {
				source = number
			}
		}
		if source < 0 {
			t.Errorf("%s ne charge plus common.ps1 : ce test ne prouve plus rien pour lui", script)
			continue
		}

		for number, line := range lines[source+1:] {
			for name, reader := range readers {
				if reader.MatchString(line) && !strings.Contains(strings.ToLower(line), "$script:"+name) {
					t.Errorf("%s, ligne %d : $%s est lu APRÈS le point-source de common.ps1, "+
						"qui vient de l'écraser avec la valeur d'usine — ce que l'opérateur a "+
						"demandé se met à l'abri avant.\n      %s",
						script, source+number+2, name, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestNoScriptConstantIsSilentlyReassigned is the regression of v1.1, and it is a trap
// PowerShell lays rather than a typo somebody made.
//
// Variable names are case-INSENSITIVE, and an unqualified assignment written at the top
// level of a script writes into the SCRIPT scope. `$checksumAsset = $release.assets | …`
// was therefore not a new variable at all: it overwrote `$script:ChecksumAsset`, the
// constant holding the NAME of that asset. Three lines later, `Join-Path $workspace
// $script:ChecksumAsset` built a path out of a stringified object —
// « …\Temp\openscale-v1.1\@{url=https:\\api.github.com\…; id=497915905; …} » — and
// Invoke-WebRequest answered « Le format du chemin d'accès donné n'est pas pris en charge »,
// naming neither the variable nor the line that emptied it. No station could get past the
// fingerprint check, and nothing in deploy/ saw it: these tests read the scripts, they do
// not run them against an API.
//
// The rule is one a reader can hold in their head: a constant of the header is written
// ONCE. No script here has any reason to reassign one, so a second assignment — whatever
// its case, whatever its scope prefix — is the defect and not a style.
func TestNoScriptConstantIsSilentlyReassigned(t *testing.T) {
	// Both patterns are anchored on the START of the statement, and that is what separates
	// an assignment from a parameter default: `param([string]$DataRoot = $script:DataRoot)`
	// declares a LOCAL and shadows nothing, whereas the defect is a line that opens on the
	// variable it is about to empty.
	declaration := regexp.MustCompile(`^\$script:(\w+)\s*=[^=]`)

	for _, script := range powerShellScripts(t) {
		lines := strings.Split(codeOnly(readFile(t, script)), "\n")

		// `\$nom` cannot match inside `$script:nom`: what follows the dollar sign there is
		// « script: ». Case-insensitive, because PowerShell is — that is the whole trap.
		clobbers := map[string]*regexp.Regexp{}
		declared := map[string]int{}
		for number, line := range lines {
			for _, match := range declaration.FindAllStringSubmatch(strings.TrimSpace(line), -1) {
				name := strings.ToLower(match[1])
				declared[name] = number + 1
				clobbers[name] = regexp.MustCompile(`(?i)^\$` + regexp.QuoteMeta(name) + `\s*=[^=]`)
			}
		}

		for number, line := range lines {
			for name, clobber := range clobbers {
				if clobber.MatchString(strings.TrimSpace(line)) {
					t.Errorf("%s, ligne %d : cette affectation écrase $script:%s, la constante "+
						"déclarée ligne %d — les noms de variables PowerShell sont insensibles "+
						"à la casse, et à la racine d'un script une affectation non qualifiée "+
						"écrit dans la portée du script.\n      %s",
						script, number+1, name, declared[name], strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestNoLocalVariableCollidesWithAParameterByCaseAlone is the third fault of the same
// family, and the one that reached a station.
//
// bootstrap.ps1 declared `[switch]$Relaunched` and, forty lines into the elevation branch,
// wrote `$relaunched = Join-Path $env:TEMP 'openscale-bootstrap.ps1'`. Those are the SAME
// variable — PowerShell compares names without case — and a parameter variable is TYPED, so
// the path went into a switch and the assignment threw: « Impossible de convertir la valeur
// "System.String" en type "System.Management.Automation.SwitchParameter" ». With
// $ErrorActionPreference = 'Stop' the message came back out attributed to `iex`, at
// character 96 of the one-liner, naming neither the file nor the line — the installation
// looked broken from the first command.
//
// It only fired on a NON-elevated console, which is the branch that copies the script into
// %TEMP% to relaunch it. Anyone testing from an administrator window skipped it entirely,
// which is how it shipped.
//
// The rule is about CASE and not about assigning to a parameter, because assigning to one is
// legitimate and this repository does it on purpose: bootstrap.ps1 fills `$AccountPassword`
// with what was typed, and turns `$Pilot` on from an answer. Those write the name they
// declared. A name that differs only in case is somebody believing they opened a new local
// variable — the intent is visible in the spelling, and that is what is forbidden.
//
// SCOPE is what makes this test mean something rather than merely fire. common.ps1 has a
// `-Directory` in one function and a `$directory` in three others; those are four separate
// scopes and not one collision — an assignment inside a function opens a LOCAL, which
// shadows the parameter of a sibling without ever touching it. The rule therefore compares
// an assignment against the parameters of the scope that governs it, and against no other.
// Measured before it did: fifteen findings, fourteen of them in another function.
func TestNoLocalVariableCollidesWithAParameterByCaseAlone(t *testing.T) {
	assignment := regexp.MustCompile(`^\$(\w+)\s*=[^=]`)

	checked := 0
	for _, script := range powerShellScripts(t) {
		lines := strings.Split(codeOnly(readFile(t, script)), "\n")
		owner := owningScopes(lines)
		declared, where := parametersByScope(lines, owner)
		if len(declared) == 0 {
			continue
		}
		checked++

		for number, line := range lines {
			match := assignment.FindStringSubmatch(strings.TrimSpace(line))
			if match == nil {
				continue
			}
			name := match[1]
			parameter, isParameter := declared[owner[number]][strings.ToLower(name)]
			if !isParameter || parameter == name {
				continue
			}
			t.Errorf("%s, ligne %d : $%s et le paramètre -%s déclaré ligne %d sont la MÊME "+
				"variable — PowerShell compare les noms sans la casse — et celle d'un "+
				"paramètre est typée : cette affectation lèvera « Impossible de convertir la "+
				"valeur … » à l'exécution.\n      %s",
				script, number+1, name, parameter,
				where[owner[number]][strings.ToLower(name)], strings.TrimSpace(line))
		}
	}

	if checked == 0 {
		t.Fatal("aucun script PowerShell ne déclare de paramètre : ce test ne prouve plus rien")
	}
}

// owningScopes names, for each line, the function whose body holds it — empty for the body
// of the script itself.
//
// Only `function` opens a scope. `if`, `foreach` and `try` also open braces and DO NOT: a
// variable assigned inside them belongs to the enclosing scope, which is precisely how
// bootstrap.ps1 wrote a path into a switch from inside its elevation branch.
func owningScopes(lines []string) []string {
	declaration := regexp.MustCompile(`(?i)^function\s+([\w-]+)`)
	type frame struct {
		name  string
		below int
	}
	var stack []frame
	current := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1].name
	}

	owner := make([]string, len(lines))
	depth := 0
	for number, line := range lines {
		code := withoutStringLiterals(line)
		owner[number] = current()
		opening := declaration.FindStringSubmatch(strings.TrimSpace(code))
		before := depth
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if opening != nil {
			// The declaration line itself belongs to the enclosing scope, and the brace may
			// sit on it or on the next one — the frame closes when the depth comes back
			// under what it was before the function was named, either way.
			stack = append(stack, frame{name: opening[1], below: before})
			continue
		}
		for len(stack) > 0 && depth <= stack[len(stack)-1].below {
			stack = stack[:len(stack)-1]
		}
	}
	return owner
}

// parametersByScope collects the parameters each scope declares, keyed by lowercase name so
// a lookup answers the question PowerShell itself asks, and holding the spelling so the
// failure can show both.
func parametersByScope(lines []string, owner []string) (map[string]map[string]string, map[string]map[string]int) {
	// A parameter name is followed by a comma, the closing parenthesis, or its default
	// value. `$script:DataRoot` in a default is not one: what follows its name is a colon.
	name := regexp.MustCompile(`\$(\w+)\s*(?:[,)=]|$)`)
	opener := regexp.MustCompile(`(?i)^param\s*\(`)

	declared := map[string]map[string]string{}
	where := map[string]map[string]int{}
	for number, line := range lines {
		code := withoutStringLiterals(line)
		if !opener.MatchString(strings.TrimSpace(code)) {
			continue
		}
		scope := owner[number]
		if declared[scope] == nil {
			declared[scope] = map[string]string{}
			where[scope] = map[string]int{}
		}
		depth := 0
		for cursor := number; cursor < len(lines); cursor++ {
			inside := withoutStringLiterals(lines[cursor])
			for _, match := range name.FindAllStringSubmatch(inside, -1) {
				declared[scope][strings.ToLower(match[1])] = match[1]
				where[scope][strings.ToLower(match[1])] = cursor + 1
			}
			depth += strings.Count(inside, "(") - strings.Count(inside, ")")
			if depth <= 0 {
				break
			}
		}
	}
	return declared, where
}

// withoutStringLiterals blanks out what is inside quotes, so a brace or a parenthesis in a
// message never moves the depth counters above.
//
// « '{0}-{1}{2}' » is a real format string of common.ps1, and « ) » closes a sentence in
// half the messages of these scripts.
func withoutStringLiterals(line string) string {
	var out strings.Builder
	var quote rune
	for _, letter := range line {
		switch {
		case quote == 0 && (letter == '\'' || letter == '"'):
			quote = letter
			out.WriteRune(letter)
		case quote != 0 && letter == quote:
			quote = 0
			out.WriteRune(letter)
		case quote != 0:
			out.WriteRune(' ')
		default:
			out.WriteRune(letter)
		}
	}
	return out.String()
}

// TestEveryPowerShellScriptCarriesTheMarkWindowsPowerShellNeeds is the encoding contract,
// and it exists because v0.1 shipped without it.
//
// Windows PowerShell 5.1 — the ONLY PowerShell on a station, and the one a right-click
// « Exécuter avec PowerShell » starts — decodes a .ps1 with no mark as ANSI. PowerShell 7
// assumes UTF-8. The two therefore read every accent in these scripts differently, and one
// sequence is fatal rather than merely ugly: « — » is E2 80 94 in UTF-8, which CP1252 reads
// as « â€” » whose last character is U+201D, a closing DOUBLE QUOTE for the PowerShell
// parser. The string literal ends on the dash, the rest of the line becomes code, and the
// installer stops parsing. That is what v0.1 did on the machine it was written for: five
// scripts, thirteen parse errors, and not one line executed.
//
// The rule is « all of them » and not « those with an accent », because a script that is
// ASCII today gets its first French message tomorrow, and whoever writes that message will
// not be thinking about byte order marks.
//
// The whole repository is walked rather than deploy/windows: make.ps1 lives at the root and
// carries the same trap.
func TestEveryPowerShellScriptCarriesTheMarkWindowsPowerShellNeeds(t *testing.T) {
	for _, script := range powerShellScripts(t) {
		// bootstrap.ps1 est la seule exception, et c'est la règle INVERSE plutôt qu'une
		// absence de règle : c'est le seul .ps1 que personne ne lit sur un disque — « irm …
		// | iex » le donne au parseur comme un FLUX, où la marque se colle au « <# » de son
		// en-tête et fait lire tout le fichier comme du code. Il ne porte donc pas la
		// marque, et pas d'accent dans son code non plus, ce qui est exactement ce qui rend
		// une relecture en CP1252 sans effet. Les deux moitiés sont tenues par
		// TestTheBootstrapIsReadAsAStreamSoItCarriesNeitherMarkNorAccent.
		if filepath.Base(script) == bootstrapPath {
			continue
		}
		raw, err := os.ReadFile(script)
		if err != nil {
			t.Errorf("lecture de %s : %v", script, err)
			continue
		}
		if bytes.HasPrefix(raw, utf8Mark) {
			continue
		}
		t.Errorf("%s n'a pas de marque d'ordre des octets (EF BB BF) : Windows PowerShell 5.1 "+
			"le lira en ANSI.\n%s", script, whatFiveOneWillRead(raw))
	}
}

// whatFiveOneWillRead shows the first line a mark-less file would lose, as CP1252 reads it.
//
// The failure message names the damage instead of citing three magic bytes: whoever adds a
// script sees the line that will break and why, not a constant to silence.
func whatFiveOneWillRead(raw []byte) string {
	for number, line := range bytes.Split(raw, []byte("\n")) {
		decoded, differs := decodeCP1252(line)
		if !differs {
			continue
		}
		return fmt.Sprintf("      ligne %d, écrite en UTF-8      : %s\n"+
			"      ligne %d, telle que 5.1 la lira : %s",
			number+1, strings.TrimRight(string(line), "\r"),
			number+1, strings.TrimRight(decoded, "\r"))
	}
	return "      (ce fichier est en ASCII pur, donc il survivrait aujourd'hui — la règle vaut " +
		"pour tous, parce qu'il ne le restera pas)"
}

// cp1252Extras is the 0x80–0x9F range of CP1252, the only place it differs from Latin-1.
//
// COPIED from the Windows code page table, never derived. Five positions are unassigned
// (0x81, 0x8D, 0x8F, 0x90, 0x9D) and Windows maps them to the control character of the same
// value; they are written out here so the table has thirty-two entries and no hole to
// misread. 0x94 is U+201D, the one that ends a string literal, and 0x97 is the em dash it
// comes from.
var cp1252Extras = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// decodeCP1252 reads bytes the way Windows PowerShell 5.1 reads a script with no mark, and
// says whether that reading differs from the UTF-8 one.
func decodeCP1252(raw []byte) (string, bool) {
	var text strings.Builder
	differs := false
	for _, b := range raw {
		switch {
		case b < 0x80:
			text.WriteByte(b)
		case b < 0xA0:
			text.WriteRune(cp1252Extras[b-0x80])
			differs = true
		default:
			text.WriteRune(rune(b))
			differs = true
		}
	}
	return text.String(), differs
}

// TestEveryPowerShellScriptParses is the syntactic check: a script with a typo in it is a
// station half-installed, and the typo is found by whoever runs it as administrator on a
// Saturday morning.
//
// It uses the PowerShell parser itself rather than a heuristic, and it checks the four
// scripts plus the shared file — under EVERY PowerShell installed, because the encoding
// defect above is invisible to PowerShell 7 and fatal to 5.1.
func TestEveryPowerShellScriptParses(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("windows", "*.ps1"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("aucun script PowerShell trouvé : %v", err)
	}

	body := `$ErrorActionPreference = 'Stop'
$failed = 0
foreach ($path in $args) { }
$scripts = @(` + quoteForPowerShell(scripts) + `)
foreach ($script in $scripts) {
  $tokens = $null
  $errors = $null
  [void][System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path $script), [ref]$tokens, [ref]$errors)
  if ($errors.Count -gt 0) {
    $failed = 1
    Write-Output "FAUTE $script"
    foreach ($e in $errors) { Write-Output ("  ligne {0} : {1}" -f $e.Extent.StartLineNumber, $e.Message) }
  } else {
    Write-Output "OK $script"
  }
}
exit $failed
`
	for _, shell := range powershellPaths(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			harness := filepath.Join(t.TempDir(), "parse.ps1")
			writeScript(t, harness, body)
			output, err := runPowerShell(t, shell, harness)
			if err != nil {
				t.Fatalf("un script PowerShell ne s'analyse pas sous %s :\n%s", shell, output)
			}
			for _, script := range scripts {
				if !strings.Contains(output, "OK "+script) && !strings.Contains(output, filepath.Base(script)) {
					t.Errorf("%s n'a pas été analysé :\n%s", script, output)
				}
			}
			t.Logf("%s", strings.TrimSpace(output))
		})
	}
}
