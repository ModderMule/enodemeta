// Package searchform reads a search from a URL query and writes it back.
//
// Both crawlers serve a browser search page with the same filters, and both
// have to carry a search from one page to the next — a pager link is the whole
// search plus a page number. Keeping the parameter names here means the two
// pages spell a filter the same way, and a link built by one parses on the
// other.
//
// Parsing is forgiving by design: a value that does not parse is dropped rather
// than refused, because a hand-edited URL with one bad number should still
// search with the rest. Limit and Offset are not form fields; paging is the
// page's business.
package searchform

import (
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/ModderMule/enodemeta/metahash"
	"github.com/ModderMule/enodemeta/model"
)

// The parameter names.
const (
	ParamQuery      = "q"
	ParamExclude    = "exclude"
	ParamSort       = "sort"
	ParamAscending  = "asc"
	ParamType       = "type"
	ParamKind       = "kind"
	ParamMinSize    = "minsize"
	ParamMaxSize    = "maxsize"
	ParamMaxAge     = "maxage"
	ParamMinFiles   = "minfiles"
	ParamMaxFiles   = "maxfiles"
	ParamCategory   = "cat"
	ParamGroup      = "group"
	ParamCompletion = "completion"
	ParamGrabs      = "grabs"
	ParamIndexed    = "indexed"
	ParamSeeders    = "seeders"
	ParamLeechers   = "leechers"
	ParamPopularity = "popularity"
	ParamAlive      = "alive"
	ParamSeen       = "seen"
)

// MiB is the unit the size fields are entered in: a visitor types 700 for a CD
// image, not 734003200.
const MiB = 1 << 20

// Parse reads a search from a URL query.
func Parse(values url.Values) model.SearchQuery {
	q := model.SearchQuery{
		Query:   strings.TrimSpace(values.Get(ParamQuery)),
		Exclude: words(values.Get(ParamExclude)),
		Type:    strings.TrimSpace(values.Get(ParamType)),

		MinSize:    parseSize(values.Get(ParamMinSize)),
		MaxSize:    parseSize(values.Get(ParamMaxSize)),
		MaxAgeDays: parseUint32(values.Get(ParamMaxAge)),
		MinFiles:   parseUint32(values.Get(ParamMinFiles)),
		MaxFiles:   parseUint32(values.Get(ParamMaxFiles)),

		Groups:            list(values[ParamGroup]),
		MinCompletion:     parsePercent(values.Get(ParamCompletion)),
		MinGrabs:          parseUint32(values.Get(ParamGrabs)),
		IndexedWithinDays: parseUint32(values.Get(ParamIndexed)),

		MinSeeders:     parseUint32(values.Get(ParamSeeders)),
		MinLeechers:    parseUint32(values.Get(ParamLeechers)),
		MinPopularity:  parseUint64(values.Get(ParamPopularity)),
		Alive:          values.Get(ParamAlive) != "",
		SeenWithinDays: parseUint32(values.Get(ParamSeen)),
	}

	if sort, ok := model.ParseSort(values.Get(ParamSort)); ok {
		q.Sort = sort
	}
	q.Ascending = values.Get(ParamAscending) != "" && !q.Sort.Ranked()

	for _, raw := range list(values[ParamCategory]) {
		if id, err := strconv.ParseUint(raw, 10, 16); err == nil && id > 0 {
			q.Categories = append(q.Categories, uint16(id))
		}
	}

	for _, raw := range list(values[ParamKind]) {
		if kind, ok := parseKind(raw); ok {
			q.Kinds = append(q.Kinds, kind)
		}
	}

	return q
}

// Encode writes a search as a URL query, leaving out everything at its zero
// value so a link carries only what the visitor set. Parse(Encode(q)) is q for
// any q Parse can produce, less Limit and Offset.
func Encode(q model.SearchQuery) url.Values {
	values := url.Values{}

	set := func(name, value string) {
		if value != "" {
			values.Set(name, value)
		}
	}

	set(ParamQuery, q.Query)
	set(ParamExclude, strings.Join(q.Exclude, " "))
	set(ParamSort, q.Sort.String())
	set(ParamType, q.Type)
	set(ParamMinSize, formatSize(q.MinSize))
	set(ParamMaxSize, formatSize(q.MaxSize))
	set(ParamMaxAge, formatUint(uint64(q.MaxAgeDays)))
	set(ParamMinFiles, formatUint(uint64(q.MinFiles)))
	set(ParamMaxFiles, formatUint(uint64(q.MaxFiles)))
	set(ParamGroup, strings.Join(q.Groups, ","))
	set(ParamCompletion, FormatPercent(q.MinCompletion))
	set(ParamGrabs, formatUint(uint64(q.MinGrabs)))
	set(ParamIndexed, formatUint(uint64(q.IndexedWithinDays)))
	set(ParamSeeders, formatUint(uint64(q.MinSeeders)))
	set(ParamLeechers, formatUint(uint64(q.MinLeechers)))
	set(ParamPopularity, formatUint(q.MinPopularity))
	set(ParamSeen, formatUint(uint64(q.SeenWithinDays)))

	if q.Ascending && !q.Sort.Ranked() {
		values.Set(ParamAscending, "1")
	}
	if q.Alive {
		values.Set(ParamAlive, "1")
	}

	categories := make([]string, 0, len(q.Categories))
	for _, c := range q.Categories {
		categories = append(categories, strconv.FormatUint(uint64(c), 10))
	}
	set(ParamCategory, strings.Join(categories, ","))

	kinds := make([]string, 0, len(q.Kinds))
	for _, k := range q.Kinds {
		if name := kindName(k); name != "" {
			kinds = append(kinds, name)
		}
	}
	set(ParamKind, strings.Join(kinds, ","))

	return values
}

// Filtered reports whether a search narrows or orders by anything beyond its
// keywords, which is when a page shows its filter panel open.
func Filtered(q model.SearchQuery) bool {
	values := Encode(q)
	values.Del(ParamQuery)

	return len(values) > 0
}

// FormatSize is a byte count in the MiB the size fields take, "" for zero.
func FormatSize(bytes uint64) string {
	return formatSize(bytes)
}

// FormatPercent is a completion floor in hundredths as the percentage the form
// takes — 9990 is "99.9" — and "" for zero.
func FormatPercent(hundredths uint16) string {
	if hundredths == 0 {
		return ""
	}

	return strconv.FormatFloat(float64(hundredths)/100, 'f', -1, 64)
}

// FormatCount is a count for a numeric field, "" for zero so an unset field
// reads as empty rather than as a floor of nothing.
func FormatCount(n uint64) string {
	return formatUint(n)
}

// -- internals ---------------------------------------------------------------

// kindNames are the kind spellings a form uses. They are short because they
// are what a visitor sees in the address bar.
var kindNames = map[metahash.Kind]string{
	metahash.KindBTV1: "v1",
	metahash.KindBTV2: "v2",
	metahash.KindNZB:  "nzb",
}

func kindName(k metahash.Kind) string {
	return kindNames[k]
}

func parseKind(raw string) (metahash.Kind, bool) {
	raw = strings.ToLower(raw)
	for kind, name := range kindNames {
		if name == raw {
			return kind, true
		}
	}

	return metahash.KindUnspecified, false
}

// words splits on whitespace, nil when there is nothing: an absent field and a
// blank one are the same search.
func words(raw string) []string {
	out := strings.Fields(raw)
	if len(out) == 0 {
		return nil
	}

	return out
}

// list reads a parameter that may be repeated, comma-separated, or both.
func list(raw []string) []string {
	var out []string

	for _, value := range raw {
		for part := range strings.SplitSeq(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}

	return out
}

func parseUint32(raw string) uint32 {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0
	}

	return uint32(n)
}

func parseUint64(raw string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}

	return n
}

// parseSize reads MiB, fractions allowed, into bytes. Anything negative, not a
// number, or too large for a byte count is no bound.
func parseSize(raw string) uint64 {
	mib, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || mib <= 0 || math.IsNaN(mib) || math.IsInf(mib, 0) {
		return 0
	}

	bytes := mib * MiB
	if bytes >= math.MaxUint64 {
		return 0
	}

	return uint64(bytes)
}

func formatSize(bytes uint64) string {
	if bytes == 0 {
		return ""
	}

	return strconv.FormatFloat(float64(bytes)/MiB, 'f', -1, 64)
}

// parsePercent reads a percentage into hundredths, rounded, and at most 100%.
func parsePercent(raw string) uint16 {
	percent, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || percent <= 0 || math.IsNaN(percent) {
		return 0
	}

	return uint16(math.Round(min(percent, 100) * 100))
}

func formatUint(n uint64) string {
	if n == 0 {
		return ""
	}

	return strconv.FormatUint(n, 10)
}
