// Command mutation-report turns the gremlins results that mutation.sh writes
// into a Markdown pull request comment about the mutants no test catches.
//
// Usage, from the repository root:
//
//	go run .github/scripts/mutation-report.go -results DIR -out FILE [-sha SHA] [-run URL]
//
// GITHUB_SERVER_URL and GITHUB_REPOSITORY turn locations into links. When
// GITHUB_STEP_SUMMARY is set, the report is also appended to it. Nothing is
// written when no module had changes.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Marker starts every report; survivorsMarker is only present when mutants
// survive. mutation-comment.sh relies on both.
const (
	marker          = "<!-- mutation-report -->"
	survivorsMarker = "<!-- mutation-report:survivors -->"
)

type results struct {
	Files []struct {
		FileName  string `json:"file_name"`
		Mutations []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
		} `json:"mutations"`
	} `json:"files"`
}

type mutant struct {
	kind    string
	covered bool
	column  int
}

type location struct {
	path string
	line int
}

func main() {
	resultsDir := flag.String("results", "", "directory with <module>/gremlins.json results")
	out := flag.String("out", "", "file to write the report to")
	sha := flag.String("sha", "", "commit the line links point at")
	run := flag.String("run", "", "URL of the CI run")
	flag.Parse()
	if *resultsDir == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	survivors, caught, found, err := load(*resultsDir)
	if err != nil {
		log.Fatal(err)
	}
	if !found {
		return
	}
	report, err := render(survivors, caught, *sha, *run)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, []byte(report), 0o600); err != nil {
		log.Fatal(err)
	}
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(summary, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := f.WriteString(report); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
	}
}

// load reads every gremlins.json under dir. found is false when there are none.
func load(dir string) (survivors map[location][]mutant, caught int, found bool, err error) {
	var paths []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return filepath.SkipAll
		}
		if err == nil && !d.IsDir() && d.Name() == "gremlins.json" {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		return nil, 0, false, err
	}

	survivors = map[location][]mutant{}
	for _, path := range paths {
		module, err := filepath.Rel(dir, filepath.Dir(path))
		if err != nil {
			return nil, 0, false, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, false, err
		}
		var r results
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, 0, false, fmt.Errorf("%s: %w", path, err)
		}
		for _, f := range r.Files {
			file := filepath.ToSlash(filepath.Join(module, f.FileName))
			for _, m := range f.Mutations {
				switch m.Status {
				case "KILLED", "TIMED OUT":
					caught++
				case "LIVED", "NOT COVERED":
					loc := location{file, m.Line}
					survivors[loc] = append(survivors[loc], mutant{m.Type, m.Status == "LIVED", m.Column})
				}
			}
		}
	}
	return survivors, caught, len(paths) > 0, nil
}

// row is one surviving mutant in the table. Mutators that make the same edit,
// such as ARITHMETIC_BASE and INVERT_NEGATIVES on -n, share a row.
type row struct {
	loc      location
	kinds    []string
	replaced string
	covered  bool
}

func render(survivors map[location][]mutant, caught int, sha, run string) (string, error) {
	locs := make([]location, 0, len(survivors))
	for loc := range survivors {
		locs = append(locs, loc)
	}
	slices.SortFunc(locs, func(a, b location) int {
		if c := strings.Compare(a.path, b.path); c != 0 {
			return c
		}
		return a.line - b.line
	})

	sources := map[string][]byte{}
	var rows []row
	for _, loc := range locs {
		src, ok := sources[loc.path]
		if !ok {
			var err error
			if src, err = os.ReadFile(loc.path); err != nil {
				return "", err
			}
			sources[loc.path] = src
		}
		ms := survivors[loc]
		slices.SortFunc(ms, func(a, b mutant) int { return strings.Compare(a.kind, b.kind) })
		first := len(rows)
		for _, m := range ms {
			replaced := replace(m, src, loc.line)
			i := slices.IndexFunc(rows[first:], func(r row) bool { return r.replaced == replaced && r.covered == m.covered })
			if replaced != "" && i >= 0 {
				rows[first+i].kinds = append(rows[first+i].kinds, m.kind)
				continue
			}
			rows = append(rows, row{loc, []string{m.kind}, replaced, m.covered})
		}
	}

	var b strings.Builder
	b.WriteString(marker + "\n")
	if len(rows) > 0 {
		b.WriteString(survivorsMarker + "\n")
	}
	b.WriteString("Mutation testing breaks the lines a PR changes on purpose, one mutation at a time, and checks that a test fails each time. A mutant that survives the tests, or that no test runs, only warns.")
	if run != "" {
		fmt.Fprintf(&b, " [Full report](%s)", run)
	}
	b.WriteString("\n\n")

	switch total := caught + len(rows); {
	case total == 0:
		b.WriteString("### No mutants on the changed lines\n")
		return b.String(), nil
	case total == 1 && len(rows) == 0:
		b.WriteString("### Tests caught the 1 mutant on the changed lines\n")
		return b.String(), nil
	case len(rows) == 0:
		fmt.Fprintf(&b, "### Tests caught all %d mutants on the changed lines\n", total)
		return b.String(), nil
	default:
		fmt.Fprintf(&b, "### Tests caught %d of %d mutants on the changed lines\n\n", caught, total)
	}

	b.WriteString("| Location | Mutation | Replaced with | Status |\n|---|---|---|---|\n")
	for _, r := range rows {
		names := make([]string, len(r.kinds))
		for i, k := range r.kinds {
			names[i] = name(k)
		}
		replaced := "—"
		if r.replaced != "" {
			replaced = code(r.replaced)
		}
		status := "Not covered"
		if r.covered {
			status = "Survived"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", link(r.loc, sha), strings.Join(names, ", "), replaced, status)
	}
	return b.String(), nil
}

// link renders a location, linked to sha when the repository is known.
func link(loc location, sha string) string {
	text := code(fmt.Sprintf("%s:%d", loc.path, loc.line))
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" || sha == "" {
		return text
	}
	server := os.Getenv("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	return fmt.Sprintf("[%s](%s/%s/blob/%s/%s#L%d)", text, server, repo, sha, loc.path, loc.line)
}

// replace returns the source line with the mutant applied, or "" when the
// mutated token is unknown.
func replace(m mutant, src []byte, line int) string {
	lines := strings.Split(string(src), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	tok := tokenAt(src, line, m.column)
	to, ok := mutations[m.kind][tok]
	text, start := lines[line-1], m.column-1
	if !ok || start+len(tok.String()) > len(text) {
		return ""
	}
	return strings.TrimSpace(text[:start] + to.String() + text[start+len(tok.String()):])
}

// name turns a gremlins mutant type, such as CONDITIONALS_BOUNDARY, into
// "Conditionals boundary".
func name(kind string) string {
	s := strings.ToLower(strings.ReplaceAll(kind, "_", " "))
	return strings.ToUpper(s[:1]) + s[1:]
}

// code formats s as inline code in a Markdown table cell.
func code(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// tokenAt returns the token at the 1-based line and column of src.
func tokenAt(src []byte, line, column int) token.Token {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			return token.ILLEGAL
		}
		if p := file.Position(pos); p.Line == line && p.Column == column {
			return tok
		} else if p.Line > line {
			return token.ILLEGAL
		}
	}
}

// mutations mirrors the token swaps of gremlins v0.6.0 (internal/engine/mappings.go).
var mutations = map[string]map[token.Token]token.Token{
	"ARITHMETIC_BASE": {
		token.ADD: token.SUB, token.MUL: token.QUO, token.QUO: token.MUL,
		token.REM: token.MUL, token.SUB: token.ADD,
	},
	"CONDITIONALS_BOUNDARY": {
		token.GEQ: token.GTR, token.GTR: token.GEQ, token.LEQ: token.LSS, token.LSS: token.LEQ,
	},
	"CONDITIONALS_NEGATION": {
		token.EQL: token.NEQ, token.GEQ: token.LSS, token.GTR: token.LEQ,
		token.LEQ: token.GTR, token.LSS: token.GEQ, token.NEQ: token.EQL,
	},
	"INCREMENT_DECREMENT": {token.DEC: token.INC, token.INC: token.DEC},
	"INVERT_ASSIGNMENTS": {
		token.ADD_ASSIGN: token.SUB_ASSIGN, token.MUL_ASSIGN: token.QUO_ASSIGN,
		token.QUO_ASSIGN: token.MUL_ASSIGN, token.REM_ASSIGN: token.REM_ASSIGN,
		token.SUB_ASSIGN: token.ADD_ASSIGN,
	},
	"INVERT_BITWISE": {
		token.AND: token.OR, token.OR: token.AND, token.XOR: token.AND,
		token.AND_NOT: token.AND, token.SHL: token.SHR, token.SHR: token.SHL,
	},
	"INVERT_BWASSIGN": {
		token.AND_ASSIGN: token.OR_ASSIGN, token.OR_ASSIGN: token.AND_ASSIGN,
		token.XOR_ASSIGN: token.AND_ASSIGN, token.AND_NOT_ASSIGN: token.AND_ASSIGN,
		token.SHL_ASSIGN: token.SHR_ASSIGN, token.SHR_ASSIGN: token.SHL_ASSIGN,
	},
	"INVERT_LOGICAL":   {token.LAND: token.LOR, token.LOR: token.LAND},
	"INVERT_LOOPCTRL":  {token.BREAK: token.CONTINUE, token.CONTINUE: token.BREAK},
	"INVERT_NEGATIVES": {token.SUB: token.ADD},
	"REMOVE_SELF_ASSIGNMENTS": {
		token.ADD_ASSIGN: token.ASSIGN, token.AND_ASSIGN: token.ASSIGN,
		token.AND_NOT_ASSIGN: token.ASSIGN, token.MUL_ASSIGN: token.ASSIGN,
		token.OR_ASSIGN: token.ASSIGN, token.QUO_ASSIGN: token.ASSIGN,
		token.REM_ASSIGN: token.ASSIGN, token.SHL_ASSIGN: token.ASSIGN,
		token.SHR_ASSIGN: token.ASSIGN, token.SUB_ASSIGN: token.ASSIGN,
		token.XOR_ASSIGN: token.ASSIGN,
	},
}
