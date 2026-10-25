package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

var version = "dev"

const usageText = `m3filter - filter M3U/M3U8 playlists by pattern

Usage:
  m3filter [flags] [input.m3u]      (reads stdin when no file is given)

Patterns are written as [field:]pattern. Fields: name, group, id, logo, url, any.
Without a prefix the --field default (name) is used. Entries are kept when they
match ANY include pattern (or all entries if none) and NO exclude pattern.
Matching is case-insensitive unless -c is set. Regex uses Go RE2 syntax.

Examples:
  m3filter -i 'group:^Sports' -x 'name:(?i)adult|xxx' -m regex in.m3u -o out.m3u
  m3filter -m glob -i 'ESPN*' -i 'Fox*' in.m3u
  m3filter -groups in.m3u

Flags:
`

var knownFields = map[string]bool{"name": true, "group": true, "id": true, "logo": true, "url": true, "any": true}

// Entry is a single playlist item: its leading # lines (including #EXTINF) and the URL.
type Entry struct {
	Lines []string
	Attrs map[string]string
	Name  string
	Group string
	URL   string
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// Matcher tests a single value.
type Matcher func(string) bool

// Rule is a compiled pattern bound to an entry field.
type Rule struct {
	Field string
	Match Matcher
}

var attrRe = regexp.MustCompile(`([\w-]+)="([^"]*)"`)

// parseExtinf extracts attributes and the display title from an #EXTINF line.
func parseExtinf(line string) (map[string]string, string) {
	attrs := map[string]string{}
	body := strings.TrimPrefix(line, "#EXTINF:")
	inQuotes := false
	comma := -1
	for i, r := range body {
		if r == '"' {
			inQuotes = !inQuotes
		}
		if r == ',' && !inQuotes {
			comma = i
			break
		}
	}
	meta, title := body, ""
	if comma >= 0 {
		meta, title = body[:comma], body[comma+1:]
	}
	for _, m := range attrRe.FindAllStringSubmatch(meta, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	return attrs, strings.TrimSpace(title)
}

// parsePlaylist reads an M3U stream into its header line and entries.
func parsePlaylist(r io.Reader) (string, []Entry, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	header := "#EXTM3U"
	var entries []Entry
	var pending []string
	first := true

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if first {
			line = strings.TrimPrefix(line, "\xef\xbb\xbf")
			first = false
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#EXTM3U"):
			header = line
		case strings.HasPrefix(line, "#"):
			pending = append(pending, line)
		default:
			entries = append(entries, buildEntry(pending, line))
			pending = nil
		}
	}
	return header, entries, sc.Err()
}

func buildEntry(comments []string, url string) Entry {
	e := Entry{Lines: comments, Attrs: map[string]string{}, URL: url}
	for _, l := range comments {
		switch {
		case strings.HasPrefix(l, "#EXTINF:"):
			e.Attrs, e.Name = parseExtinf(l)
			e.Group = e.Attrs["group-title"]
		case strings.HasPrefix(l, "#EXTGRP:") && e.Group == "":
			e.Group = strings.TrimSpace(strings.TrimPrefix(l, "#EXTGRP:"))
		}
	}
	if e.Name == "" {
		e.Name = e.Attrs["tvg-name"]
	}
	return e
}

// fieldValues returns the strings a rule on the given field is tested against.
func (e Entry) fieldValues(field string) []string {
	switch field {
	case "name":
		return []string{e.Name}
	case "group":
		return []string{e.Group}
	case "id":
		return []string{e.Attrs["tvg-id"]}
	case "logo":
		return []string{e.Attrs["tvg-logo"]}
	case "url":
		return []string{e.URL}
	default:
		return []string{e.Name, e.Group, e.Attrs["tvg-id"], e.Attrs["tvg-name"], e.URL}
	}
}

func (r Rule) matches(e Entry) bool {
	for _, v := range e.fieldValues(r.Field) {
		if r.Match(v) {
			return true
		}
	}
	return false
}

func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return b.String()
}

// compileRule parses "[field:]pattern" into a Rule for the given mode.
func compileRule(spec, defaultField, mode string, caseSensitive bool) (Rule, error) {
	field, pattern := defaultField, spec
	if i := strings.Index(spec, ":"); i > 0 && knownFields[strings.ToLower(spec[:i])] {
		field, pattern = strings.ToLower(spec[:i]), spec[i+1:]
	}

	switch mode {
	case "contains":
		if caseSensitive {
			return Rule{field, func(s string) bool { return strings.Contains(s, pattern) }}, nil
		}
		lowered := strings.ToLower(pattern)
		return Rule{field, func(s string) bool { return strings.Contains(strings.ToLower(s), lowered) }}, nil
	case "glob", "regex":
		expr := pattern
		if mode == "glob" {
			expr = globToRegex(pattern)
		}
		if !caseSensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return Rule{}, fmt.Errorf("invalid pattern %q: %w", spec, err)
		}
		return Rule{field, re.MatchString}, nil
	}
	return Rule{}, fmt.Errorf("unknown mode %q (use contains, glob or regex)", mode)
}

func compileRules(specs []string, defaultField, mode string, caseSensitive bool) ([]Rule, error) {
	rules := make([]Rule, 0, len(specs))
	for _, s := range specs {
		r, err := compileRule(s, defaultField, mode, caseSensitive)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func anyMatch(rules []Rule, e Entry) bool {
	for _, r := range rules {
		if r.matches(e) {
			return true
		}
	}
	return false
}

// filterEntries keeps entries matching any include rule (or all) and no exclude rule.
func filterEntries(entries []Entry, include, exclude []Rule) []Entry {
	var out []Entry
	for _, e := range entries {
		if len(include) > 0 && !anyMatch(include, e) {
			continue
		}
		if anyMatch(exclude, e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func writePlaylist(w io.Writer, header string, entries []Entry) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, header)
	for _, e := range entries {
		for _, l := range e.Lines {
			fmt.Fprintln(bw, l)
		}
		fmt.Fprintln(bw, e.URL)
	}
	return bw.Flush()
}

func writeGroups(w io.Writer, entries []Entry) {
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Group]++
	}
	names := make([]string, 0, len(counts))
	for g := range counts {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		label := g
		if label == "" {
			label = "(none)"
		}
		fmt.Fprintf(w, "%6d  %s\n", counts[g], label)
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	var include, exclude stringList
	flag.Var(&include, "i", "include pattern, repeatable (alias: -include)")
	flag.Var(&include, "include", "")
	flag.Var(&exclude, "x", "exclude pattern, repeatable (alias: -exclude)")
	flag.Var(&exclude, "exclude", "")
	field := flag.String("f", "name", "default field for patterns without prefix")
	mode := flag.String("m", "contains", "pattern mode: contains, glob or regex")
	caseSensitive := flag.Bool("c", false, "case-sensitive matching")
	outPath := flag.String("o", "", "output file (default stdout)")
	groups := flag.Bool("groups", false, "list groups with entry counts and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usageText)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if !knownFields[*field] {
		fail("unknown field %q (use name, group, id, logo, url or any)", *field)
	}

	inc, err := compileRules(include, *field, *mode, *caseSensitive)
	if err != nil {
		fail("%v", err)
	}
	exc, err := compileRules(exclude, *field, *mode, *caseSensitive)
	if err != nil {
		fail("%v", err)
	}

	in := io.Reader(os.Stdin)
	if flag.NArg() > 1 {
		fail("expected at most one input file")
	}
	if flag.NArg() == 1 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			fail("%v", err)
		}
		defer f.Close()
		in = f
	}

	header, entries, err := parsePlaylist(in)
	if err != nil {
		fail("reading playlist: %v", err)
	}

	if *groups {
		writeGroups(os.Stdout, entries)
		return
	}

	kept := filterEntries(entries, inc, exc)

	out := io.Writer(os.Stdout)
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fail("%v", err)
		}
		defer f.Close()
		out = f
	}
	if err := writePlaylist(out, header, kept); err != nil {
		fail("writing playlist: %v", err)
	}
	fmt.Fprintf(os.Stderr, "kept %d of %d entries\n", len(kept), len(entries))
}
