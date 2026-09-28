package containeranalysis

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// occurrenceFilter matches ListOccurrences / vulnerabilitySummary filter expressions.
type occurrenceFilter interface {
	match(o store.ContainerOccurrence) bool
}

type allFilter struct{}

func (allFilter) match(store.ContainerOccurrence) bool { return true }

type andFilter struct{ parts []occurrenceFilter }

func (f andFilter) match(o store.ContainerOccurrence) bool {
	for _, p := range f.parts {
		if !p.match(o) {
			return false
		}
	}
	return true
}

type orFilter struct{ parts []occurrenceFilter }

func (f orFilter) match(o store.ContainerOccurrence) bool {
	for _, p := range f.parts {
		if p.match(o) {
			return true
		}
	}
	return false
}

type kindFilter struct{ kind string }

func (f kindFilter) match(o store.ContainerOccurrence) bool {
	return strings.EqualFold(strings.TrimSpace(o.Kind), strings.TrimSpace(f.kind))
}

type uriEqualsFilter struct{ uri string }

func (f uriEqualsFilter) match(o store.ContainerOccurrence) bool {
	return resourceURIEqual(o.ResourceURI, f.uri)
}

type uriPrefixFilter struct{ prefix string }

func (f uriPrefixFilter) match(o store.ContainerOccurrence) bool {
	return resourceURIHasPrefix(o.ResourceURI, f.prefix)
}

type noteIDFilter struct{ noteID string }

func (f noteIDFilter) match(o store.ContainerOccurrence) bool {
	want := strings.TrimSpace(f.noteID)
	if want == "" {
		return true
	}
	note := strings.TrimSpace(o.NoteName)
	if note == want {
		return true
	}
	if i := strings.LastIndex(note, "/"); i >= 0 {
		return note[i+1:] == want
	}
	return false
}

func resourceURIEqual(stored, want string) bool {
	a := normalizeResourceURI(stored)
	b := normalizeResourceURI(want)
	if a == "" && b == "" {
		return true
	}
	return a == b
}

func resourceURIHasPrefix(stored, prefix string) bool {
	a := normalizeResourceURI(stored)
	b := normalizeResourceURI(prefix)
	if b == "" {
		return true
	}
	return strings.HasPrefix(a, b)
}

func normalizeResourceURI(uri string) string {
	uri = strings.TrimSpace(uri)
	lower := strings.ToLower(uri)
	switch {
	case strings.HasPrefix(lower, "https://"):
		uri = uri[len("https://"):]
	case strings.HasPrefix(lower, "http://"):
		uri = uri[len("http://"):]
	}
	// gcloud artifacts often uses @sha256-<hex>; Grafeas/CA stores @sha256:<hex>.
	if i := strings.Index(uri, "@sha256-"); i >= 0 {
		uri = uri[:i] + "@sha256:" + uri[i+len("@sha256-"):]
	}
	return uri
}

// parseOccurrenceFilter parses a Container Analysis / Grafeas-style filter.
// Supports kind, resourceUrl/resourceUri equality, noteId, has_prefix(resourceUrl|resourceUri,"…"),
// AND/OR, and parentheses. Empty filter matches all.
func parseOccurrenceFilter(raw string) (occurrenceFilter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return allFilter{}, nil
	}
	p := &filterParser{s: raw}
	f, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos < len(p.s) {
		return nil, fmt.Errorf("unexpected trailing filter text at %d", p.pos)
	}
	return f, nil
}

type filterParser struct {
	s   string
	pos int
}

func (p *filterParser) skipSpace() {
	for p.pos < len(p.s) && unicode.IsSpace(rune(p.s[p.pos])) {
		p.pos++
	}
}

func (p *filterParser) peekKeyword(kw string) bool {
	p.skipSpace()
	if p.pos+len(kw) > len(p.s) {
		return false
	}
	frag := p.s[p.pos : p.pos+len(kw)]
	if !strings.EqualFold(frag, kw) {
		return false
	}
	end := p.pos + len(kw)
	if end < len(p.s) {
		r := rune(p.s[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return false
		}
	}
	return true
}

func (p *filterParser) consumeKeyword(kw string) bool {
	if !p.peekKeyword(kw) {
		return false
	}
	p.pos += len(kw)
	return true
}

func (p *filterParser) parseOr() (occurrenceFilter, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	parts := []occurrenceFilter{left}
	for p.consumeKeyword("OR") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return orFilter{parts: parts}, nil
}

func (p *filterParser) parseAnd() (occurrenceFilter, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	parts := []occurrenceFilter{left}
	for {
		p.skipSpace()
		if p.consumeKeyword("AND") {
			right, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			parts = append(parts, right)
			continue
		}
		// Implicit AND when another primary follows (kind="A" resourceUrl="B").
		if p.pos < len(p.s) && p.s[p.pos] != ')' && !p.peekKeyword("OR") {
			if looksLikePrimaryStart(p.s[p.pos:]) {
				right, err := p.parsePrimary()
				if err != nil {
					return nil, err
				}
				parts = append(parts, right)
				continue
			}
		}
		break
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return andFilter{parts: parts}, nil
}

func looksLikePrimaryStart(s string) bool {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" {
		return false
	}
	if s[0] == '(' {
		return true
	}
	lower := strings.ToLower(s)
	for _, prefix := range []string{"kind", "resourceurl", "resourceuri", "noteid", "has_prefix"} {
		if strings.HasPrefix(lower, prefix) {
			rest := s[len(prefix):]
			rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
			if rest == "" {
				return false
			}
			return rest[0] == '=' || rest[0] == '('
		}
	}
	return false
}

func (p *filterParser) parsePrimary() (occurrenceFilter, error) {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of filter")
	}
	if p.s[p.pos] == '(' {
		p.pos++
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return inner, nil
	}
	if p.consumeKeyword("has_prefix") {
		return p.parseHasPrefix()
	}
	field, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos >= len(p.s) || p.s[p.pos] != '=' {
		return nil, fmt.Errorf("expected = after %s", field)
	}
	p.pos++
	val, err := p.parseQuotedOrBare()
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(field) {
	case "kind":
		return kindFilter{kind: val}, nil
	case "resourceurl", "resourceuri":
		return uriEqualsFilter{uri: val}, nil
	case "noteid":
		return noteIDFilter{noteID: val}, nil
	default:
		return nil, fmt.Errorf("unsupported filter field %q", field)
	}
}

func (p *filterParser) parseHasPrefix() (occurrenceFilter, error) {
	p.skipSpace()
	if p.pos >= len(p.s) || p.s[p.pos] != '(' {
		return nil, fmt.Errorf("has_prefix expects (")
	}
	p.pos++
	field, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos >= len(p.s) || p.s[p.pos] != ',' {
		return nil, fmt.Errorf("has_prefix expects comma")
	}
	p.pos++
	prefix, err := p.parseQuotedOrBare()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos >= len(p.s) || p.s[p.pos] != ')' {
		return nil, fmt.Errorf("has_prefix expects )")
	}
	p.pos++
	switch strings.ToLower(field) {
	case "resourceurl", "resourceuri":
		return uriPrefixFilter{prefix: prefix}, nil
	default:
		return nil, fmt.Errorf("has_prefix only supports resourceUrl/resourceUri")
	}
}

func (p *filterParser) parseIdent() (string, error) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.s) {
		r := rune(p.s[p.pos])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", fmt.Errorf("expected identifier at %d", p.pos)
	}
	return p.s[start:p.pos], nil
}

func (p *filterParser) parseQuotedOrBare() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return "", fmt.Errorf("expected value")
	}
	quote := p.s[p.pos]
	if quote == '"' || quote == '\'' {
		p.pos++
		start := p.pos
		for p.pos < len(p.s) && p.s[p.pos] != quote {
			p.pos++
		}
		if p.pos >= len(p.s) {
			return "", fmt.Errorf("unterminated string")
		}
		val := p.s[start:p.pos]
		p.pos++
		return val, nil
	}
	start := p.pos
	for p.pos < len(p.s) {
		r := rune(p.s[p.pos])
		if unicode.IsSpace(r) || r == ')' || r == '(' || r == ',' {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("expected value")
	}
	return p.s[start:p.pos], nil
}

func applyFilter(list []store.ContainerOccurrence, f occurrenceFilter) []store.ContainerOccurrence {
	if f == nil {
		return list
	}
	if _, ok := f.(allFilter); ok {
		return list
	}
	out := make([]store.ContainerOccurrence, 0, len(list))
	for i := range list {
		if f.match(list[i]) {
			out = append(out, list[i])
		}
	}
	return out
}
