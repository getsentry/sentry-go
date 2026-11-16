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

// change is one distinct edit that survived on a line. Different mutators can
// make the same edit, such as ARITHMETIC_BASE and INVERT_NEGATIVES on -n.
type change struct {
	edit    string
	covered bool
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
	changes := map[location][]change{}
	total := 0
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
		for _, m := range ms {
			c := change{describe(m, src, loc.line), m.covered}
			if !slices.Contains(changes[loc], c) {
				changes[loc] = append(changes[loc], c)
				total++
			}
		}
	}

	var b strings.Builder
	b.WriteString(marker + "\n## Mutation testing\n\n")
	if total == 0 {
		switch caught {
		case 0:
			b.WriteString("Nothing on the lines this PR touches can be mutated.\n")
		case 1:
			b.WriteString("The 1 change on lines this PR touches is caught by the tests.\n")
		default:
			fmt.Fprintf(&b, "All %d changes on lines this PR touches are caught by the tests.\n", caught)
		}
		footer(&b, sha, run)
		return b.String(), nil
	}

	b.WriteString(survivorsMarker + "\n")
	fmt.Fprintf(&b, "The tests did not catch %s on lines this PR touches.", plural(total, "change"))
	if caught > 0 {
		fmt.Fprintf(&b, " %s caught.", plural(caught, "other change was", "other changes were"))
	}
	b.WriteString("\n")

	for _, loc := range locs {
		writeLocation(&b, loc, changes[loc], survivors[loc][0].kind, sources[loc.path], sha)
	}

	b.WriteString("\n<details><summary>What this means</summary>\n\n")
	b.WriteString("Each change is a small edit to the code, like flipping a comparison, that a test should fail on.\n\n")
	b.WriteString("- **not caught**: tests run the line, but no assertion depends on it.\n")
	b.WriteString("- **not run**: no test executes the line.\n\n")
	b.WriteString("Run locally with `make mutation MUTATION_BASE=origin/<base-branch>`.\n</details>\n")
	footer(&b, sha, run)
	return b.String(), nil
}

func writeLocation(b *strings.Builder, loc location, changes []change, kind string, src []byte, sha string) {
	name := fmt.Sprintf("%s:%d", loc.path, loc.line)
	if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" && sha != "" {
		server := os.Getenv("GITHUB_SERVER_URL")
		if server == "" {
			server = "https://github.com"
		}
		name = fmt.Sprintf("[`%s`](%s/%s/blob/%s/%s#L%d)", name, server, repo, sha, loc.path, loc.line)
	} else {
		name = "`" + name + "`"
	}
	fmt.Fprintf(b, "\n### %s\n\n", name)

	if code := sourceLine(src, loc.line); code != "" {
		fmt.Fprintf(b, "```go\n%s\n```\n\n", code)
	}

	b.WriteString("| Change | Result |\n| --- | --- |\n")
	covered := false
	for _, c := range changes {
		result := "not run"
		if c.covered {
			result = "not caught"
			covered = true
		}
		fmt.Fprintf(b, "| %s | %s |\n", c.edit, result)
	}
	b.WriteString("\n")
	if covered {
		b.WriteString(hint(kind))
	} else {
		b.WriteString("No test runs this line. Add one that does and asserts on its result.")
	}
	b.WriteString("\n")
}

// describe prints a mutant as the exact edit, such as `>` → `>=`.
func describe(m mutant, src []byte, line int) string {
	tok := tokenAt(src, line, m.column)
	if to, ok := mutations[m.kind][tok]; ok {
		return fmt.Sprintf("%s → %s", cell(tok.String()), cell(to.String()))
	}
	if tok.IsOperator() || tok.IsKeyword() {
		return fmt.Sprintf("%s (%s)", cell(tok.String()), strings.ToLower(m.kind))
	}
	return strings.ToLower(m.kind)
}

// hint says what kind of test catches mutants of the given type.
func hint(kind string) string {
	switch kind {
	case "CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION":
		return "Tests run this line, but none fail when the comparison changes. Add cases exactly at the boundary and on each side of it, and assert on what each one does."
	case "ARITHMETIC_BASE":
		return "Tests run this line, but none check the computed value. Assert on the exact result."
	case "INCREMENT_DECREMENT":
		return "Tests run this line, but none check the counter. Assert on the resulting count or index."
	case "INVERT_NEGATIVES":
		return "Tests run this line, but none check the sign. Assert on the exact result."
	case "INVERT_LOGICAL":
		return "Tests run this line, but none fail when the condition changes. Add a case where only one side of it is true."
	case "INVERT_LOOPCTRL":
		return "Tests run this line, but none check what the loop does next. Assert on the iterations after this one."
	default:
		return "Tests run this line, but none fail when it changes. Add an assertion that depends on its result."
	}
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

func sourceLine(src []byte, line int) string {
	lines := strings.Split(string(src), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// cell formats code for a Markdown table cell, where | must be escaped.
func cell(code string) string {
	return "`" + strings.ReplaceAll(code, "|", `\|`) + "`"
}

func plural(n int, forms ...string) string {
	one, many := forms[0], forms[0]+"s"
	if len(forms) > 1 {
		many = forms[1]
	}
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func footer(b *strings.Builder, sha, run string) {
	var parts []string
	if sha != "" {
		parts = append(parts, "Commit "+sha[:min(len(sha), 7)])
	}
	if run != "" {
		parts = append(parts, fmt.Sprintf("[run log](%s)", run))
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, "\n<sub>%s</sub>\n", strings.Join(parts, " · "))
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
