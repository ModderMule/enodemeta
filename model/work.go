package model

// WorkKind is what a linked work is. The values are the wire enum's, so converting is a
// cast.
type WorkKind uint8

// The kinds of work.
const (
	WorkUnspecified WorkKind = iota
	WorkMovie
	WorkSeries
	WorkEpisode
	WorkAdultMovie
	WorkAdultScene

	workKindCount
)

// Valid reports whether k is a known kind other than WorkUnspecified.
func (k WorkKind) Valid() bool {
	return k > WorkUnspecified && k < workKindCount
}

// The rating sources a WorkInfo and a WorkFilter name.
const (
	RatingIMDb           = "imdb"
	RatingRottenTomatoes = "rottentomatoes"
	RatingMetacritic     = "metacritic"
)

// WorkFilter narrows an enhanced search to releases by their linked work. Every field set
// must hold; a zero field is no filter. The ids are a union among themselves — a client sends
// every id it has for one title — and an id that names no work matches nothing. A bound on a
// value the daemon does not know for a release fails that release. Scores are on a 0–100
// scale and runtimes in minutes.
type WorkFilter struct {
	Kinds []WorkKind

	IMDbID      string
	TMDBMovieID uint64
	TMDBTVID    uint64
	TVDBID      uint64
	TVMazeID    uint64
	TPDBID      string
	UPC         string

	// Season and Episode narrow a series named by an id.
	Season  uint16
	Episode uint16

	MinYear uint16
	MaxYear uint16

	MinIMDbScore       uint16
	MinIMDbVotes       uint32
	MinRTScore         uint16
	MinMetacriticScore uint16

	MinRuntime uint16
	MaxRuntime uint16

	// Performers must all be in the work's cast.
	Performers []string

	// LinkedOnly keeps a release linked to any work.
	LinkedOnly bool
}

// IsZero reports whether the filter narrows nothing.
func (f WorkFilter) IsZero() bool {
	return len(f.Kinds) == 0 && !f.HasIDs() && f.Season == 0 && f.Episode == 0 &&
		f.MinYear == 0 && f.MaxYear == 0 &&
		f.MinIMDbScore == 0 && f.MinIMDbVotes == 0 && f.MinRTScore == 0 && f.MinMetacriticScore == 0 &&
		f.MinRuntime == 0 && f.MaxRuntime == 0 && len(f.Performers) == 0 && !f.LinkedOnly
}

// HasIDs reports whether the filter names works by an id.
func (f WorkFilter) HasIDs() bool {
	return f.IMDbID != "" || f.TMDBMovieID != 0 || f.TMDBTVID != 0 || f.TVDBID != 0 || f.TVMazeID != 0 || f.TPDBID != "" || f.UPC != ""
}

// Rating is one source's score of a work, on a 0–100 scale, and how many voted where the
// source says.
type Rating struct {
	Source string
	Score  uint16
	Votes  uint32
}

// WorkInfo describes the work a release is linked to. For an episode the ids, title, year and
// ratings are its series', and EpisodeTitle, Season and Episode its own. TMDBID is a film's
// for a film and a show's otherwise.
type WorkInfo struct {
	Kind WorkKind

	Title         string
	OriginalTitle string
	Year          uint16

	IMDbID   string
	TMDBID   uint64
	TVDBID   uint64
	TVMazeID uint64
	TPDBID   string

	Season       uint16
	Episode      uint16
	EpisodeTitle string

	Ratings []Rating

	// RuntimeMinutes is zero when unknown.
	RuntimeMinutes uint16

	// Performers is the cast in its source's order.
	Performers []string

	UPC string
}

// EnhancedSearchQuery is a plain search narrowed by the work.
type EnhancedSearchQuery struct {
	Search SearchQuery
	Work   WorkFilter
}

// EnhancedSearchResult is a plain search's answer with the works described, keyed by the
// entries' CatalogID. A release with no work is absent.
type EnhancedSearchResult struct {
	Result SearchResult
	Works  map[string]WorkInfo
}
