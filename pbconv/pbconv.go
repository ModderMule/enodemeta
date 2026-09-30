// Package pbconv converts between the domain types in model and the generated
// protobuf types.
//
// It is the only place the two meet. That is the rule from §6.3 of the
// specification: keeping generated structs out of the rest of the code is what
// makes a second wire representation an adapter rather than a rewrite, and this
// package is that adapter's seam.
package pbconv

import (
	"math"

	"github.com/ModderMule/enodemeta/metahash"
	"github.com/ModderMule/enodemeta/model"

	metav1 "github.com/ModderMule/enodemeta/gen/enode/meta/v1"
)

// KindToProto converts a meta kind.
func KindToProto(k metahash.Kind) metav1.MetaKind {
	switch k {
	case metahash.KindBTV1:
		return metav1.MetaKind_META_KIND_BT_V1
	case metahash.KindBTV2:
		return metav1.MetaKind_META_KIND_BT_V2
	case metahash.KindNZB:
		return metav1.MetaKind_META_KIND_NZB
	default:
		return metav1.MetaKind_META_KIND_UNSPECIFIED
	}
}

// KindFromProto converts a meta kind back.
func KindFromProto(k metav1.MetaKind) metahash.Kind {
	switch k {
	case metav1.MetaKind_META_KIND_BT_V1:
		return metahash.KindBTV1
	case metav1.MetaKind_META_KIND_BT_V2:
		return metahash.KindBTV2
	case metav1.MetaKind_META_KIND_NZB:
		return metahash.KindNZB
	default:
		return metahash.KindUnspecified
	}
}

// EntryToProto converts one row.
func EntryToProto(e model.Entry) *metav1.MetaEntry {
	return &metav1.MetaEntry{
		MetaHash:          cloneBytes(e.MetaHash),
		Kind:              KindToProto(e.Kind),
		FileIndex:         e.FileIndex,
		FilePath:          e.FilePath,
		Name:              e.Name,
		Size:              e.Size,
		TotalSize:         e.TotalSize,
		Type:              e.Type,
		Seeders:           e.Seeders,
		Peers:             e.Peers,
		AgeDays:           e.AgeDays,
		Indexer:           e.Indexer,
		CatalogId:         e.CatalogID,
		Magnet:            e.Magnet,
		Flags:             e.Flags,
		Identity:          cloneBytes(e.Identity),
		FileCount:         e.FileCount,
		PathAuthoritative: e.PathAuthoritative,
	}
}

// EntryFromProto converts one row back.
func EntryFromProto(e *metav1.MetaEntry) model.Entry {
	if e == nil {
		return model.Entry{}
	}

	return model.Entry{
		MetaHash:          cloneBytes(e.GetMetaHash()),
		Kind:              KindFromProto(e.GetKind()),
		FileIndex:         e.GetFileIndex(),
		FilePath:          e.GetFilePath(),
		Name:              e.GetName(),
		Size:              e.GetSize(),
		TotalSize:         e.GetTotalSize(),
		Type:              e.GetType(),
		Seeders:           e.GetSeeders(),
		Peers:             e.GetPeers(),
		AgeDays:           e.GetAgeDays(),
		Indexer:           e.GetIndexer(),
		CatalogID:         e.GetCatalogId(),
		Magnet:            e.GetMagnet(),
		Flags:             e.GetFlags(),
		Identity:          cloneBytes(e.GetIdentity()),
		FileCount:         e.GetFileCount(),
		PathAuthoritative: e.GetPathAuthoritative(),
	}
}

// EntriesToProto converts a slice of rows.
func EntriesToProto(entries []model.Entry) []*metav1.MetaEntry {
	if entries == nil {
		return nil
	}

	out := make([]*metav1.MetaEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, EntryToProto(e))
	}

	return out
}

// EntriesFromProto converts a slice of rows back.
func EntriesFromProto(entries []*metav1.MetaEntry) []model.Entry {
	if entries == nil {
		return nil
	}

	out := make([]model.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, EntryFromProto(e))
	}

	return out
}

// MetaFileToProto converts a metafile.
func MetaFileToProto(f model.MetaFile) *metav1.MetaFile {
	return &metav1.MetaFile{
		Kind:        KindToProto(f.Kind),
		Content:     cloneBytes(f.Content),
		ContentType: f.ContentType,
	}
}

// MetaFileFromProto converts a metafile back.
func MetaFileFromProto(f *metav1.MetaFile) model.MetaFile {
	if f == nil {
		return model.MetaFile{}
	}

	return model.MetaFile{
		Kind:        KindFromProto(f.GetKind()),
		Content:     cloneBytes(f.GetContent()),
		ContentType: f.GetContentType(),
	}
}

// ChangeToProto converts one release change.
func ChangeToProto(c model.ReleaseChange) *metav1.ReleaseChange {
	return &metav1.ReleaseChange{
		Seq:       c.Seq,
		CatalogId: c.CatalogID,
		Op:        opToProto(c.Op),
		Entries:   EntriesToProto(c.Entries),
		Reason:    reasonToProto(c.Reason),
	}
}

// ChangeFromProto converts one release change back.
func ChangeFromProto(c *metav1.ReleaseChange) model.ReleaseChange {
	if c == nil {
		return model.ReleaseChange{}
	}

	return model.ReleaseChange{
		Seq:       c.GetSeq(),
		CatalogID: c.GetCatalogId(),
		Op:        opFromProto(c.GetOp()),
		Entries:   EntriesFromProto(c.GetEntries()),
		Reason:    reasonFromProto(c.GetReason()),
	}
}

// SearchQueryToProto converts a query.
func SearchQueryToProto(q model.SearchQuery) *metav1.SearchRequest {
	var kinds []metav1.MetaKind
	for _, k := range q.Kinds {
		kinds = append(kinds, KindToProto(k))
	}

	return &metav1.SearchRequest{
		Query:      q.Query,
		Exclude:    append([]string(nil), q.Exclude...),
		Kinds:      kinds,
		MinSize:    q.MinSize,
		MaxSize:    q.MaxSize,
		MinSeeders: q.MinSeeders,
		MaxAgeDays: q.MaxAgeDays,
		Type:       q.Type,
		Limit:      q.Limit,
		Offset:     q.Offset,

		Sort:              metav1.SearchSort(q.Sort),
		SortAscending:     q.Ascending,
		Categories:        categoriesToProto(q.Categories),
		Groups:            cloneStrings(q.Groups),
		MinCompletion:     uint32(q.MinCompletion),
		MinGrabs:          q.MinGrabs,
		IndexedWithinDays: q.IndexedWithinDays,
		Alive:             q.Alive,
		MinLeechers:       q.MinLeechers,
		MinPopularity:     q.MinPopularity,
		SeenWithinDays:    q.SeenWithinDays,
		MinFiles:          q.MinFiles,
		MaxFiles:          q.MaxFiles,
	}
}

// SearchQueryFromProto converts a query back.
func SearchQueryFromProto(r *metav1.SearchRequest) model.SearchQuery {
	if r == nil {
		return model.SearchQuery{}
	}

	var kinds []metahash.Kind
	for _, k := range r.GetKinds() {
		kinds = append(kinds, KindFromProto(k))
	}

	// A sort from a newer peer that this build does not know is relevance,
	// which is what the contract says a daemon answers for a sort it lacks.
	sort := model.SearchSort(r.GetSort())
	if r.GetSort() < 0 || !sort.Valid() {
		sort = model.SortUnspecified
	}

	return model.SearchQuery{
		Query:      r.GetQuery(),
		Exclude:    cloneStrings(r.GetExclude()),
		Kinds:      kinds,
		MinSize:    r.GetMinSize(),
		MaxSize:    r.GetMaxSize(),
		MinSeeders: r.GetMinSeeders(),
		MaxAgeDays: r.GetMaxAgeDays(),
		Type:       r.GetType(),
		Limit:      r.GetLimit(),
		Offset:     r.GetOffset(),

		Sort:              sort,
		Ascending:         r.GetSortAscending(),
		Categories:        categoriesFromProto(r.GetCategories()),
		Groups:            cloneStrings(r.GetGroups()),
		MinCompletion:     uint16(min(r.GetMinCompletion(), maxCompletion)),
		MinGrabs:          r.GetMinGrabs(),
		IndexedWithinDays: r.GetIndexedWithinDays(),
		Alive:             r.GetAlive(),
		MinLeechers:       r.GetMinLeechers(),
		MinPopularity:     r.GetMinPopularity(),
		SeenWithinDays:    r.GetSeenWithinDays(),
		MinFiles:          r.GetMinFiles(),
		MaxFiles:          r.GetMaxFiles(),
	}
}

// SearchResultToProto converts a result.
func SearchResultToProto(res model.SearchResult) *metav1.SearchResponse {
	return &metav1.SearchResponse{
		Entries:    EntriesToProto(res.Entries),
		Total:      res.Total,
		NextOffset: res.NextOffset,
		TotalExact: res.TotalExact,
		Window:     res.Window,
	}
}

// SearchResultFromProto converts a result back.
func SearchResultFromProto(res *metav1.SearchResponse) model.SearchResult {
	if res == nil {
		return model.SearchResult{}
	}

	return model.SearchResult{
		Entries:    EntriesFromProto(res.GetEntries()),
		Total:      res.GetTotal(),
		NextOffset: res.GetNextOffset(),
		TotalExact: res.GetTotalExact(),
		Window:     res.GetWindow(),
	}
}

// DaemonInfoToProto converts a daemon description.
func DaemonInfoToProto(info model.DaemonInfo) *metav1.GetInfoResponse {
	var kinds []metav1.MetaKind
	for _, k := range info.Kinds {
		kinds = append(kinds, KindToProto(k))
	}

	return &metav1.GetInfoResponse{
		Daemon:           info.Daemon,
		Version:          info.Version,
		ContractVersion:  info.ContractVersion,
		Kinds:            kinds,
		LastSeq:          info.LastSeq,
		PurgedThroughSeq: info.PurgedThroughSeq,
		Published:        info.Published,
		Catalogued:       info.Catalogued,
		Files:            info.Files,
		Indexer:          info.Indexer,
		SearchAvailable:  info.SearchAvailable,

		EnhancedSearchAvailable: info.EnhancedSearchAvailable,
	}
}

// DaemonInfoFromProto converts a daemon description back.
func DaemonInfoFromProto(res *metav1.GetInfoResponse) model.DaemonInfo {
	if res == nil {
		return model.DaemonInfo{}
	}

	var kinds []metahash.Kind
	for _, k := range res.GetKinds() {
		kinds = append(kinds, KindFromProto(k))
	}

	return model.DaemonInfo{
		Daemon:           res.GetDaemon(),
		Version:          res.GetVersion(),
		ContractVersion:  res.GetContractVersion(),
		Kinds:            kinds,
		LastSeq:          res.GetLastSeq(),
		PurgedThroughSeq: res.GetPurgedThroughSeq(),
		Published:        res.GetPublished(),
		Catalogued:       res.GetCatalogued(),
		Files:            res.GetFiles(),
		Indexer:          res.GetIndexer(),
		SearchAvailable:  res.GetSearchAvailable(),

		EnhancedSearchAvailable: res.GetEnhancedSearchAvailable(),
	}
}

// EnhancedSearchQueryToProto converts an enhanced query.
func EnhancedSearchQueryToProto(q model.EnhancedSearchQuery) *metav1.EnhancedSearchRequest {
	return &metav1.EnhancedSearchRequest{
		Search: SearchQueryToProto(q.Search),
		Work:   WorkFilterToProto(q.Work),
	}
}

// EnhancedSearchQueryFromProto converts an enhanced query back.
func EnhancedSearchQueryFromProto(r *metav1.EnhancedSearchRequest) model.EnhancedSearchQuery {
	if r == nil {
		return model.EnhancedSearchQuery{}
	}

	return model.EnhancedSearchQuery{
		Search: SearchQueryFromProto(r.GetSearch()),
		Work:   WorkFilterFromProto(r.GetWork()),
	}
}

// EnhancedSearchResultToProto converts an enhanced result.
func EnhancedSearchResultToProto(res model.EnhancedSearchResult) *metav1.EnhancedSearchResponse {
	var works map[string]*metav1.WorkInfo

	if len(res.Works) > 0 {
		works = make(map[string]*metav1.WorkInfo, len(res.Works))
		for id, info := range res.Works {
			works[id] = WorkInfoToProto(info)
		}
	}

	return &metav1.EnhancedSearchResponse{
		Result: SearchResultToProto(res.Result),
		Works:  works,
	}
}

// EnhancedSearchResultFromProto converts an enhanced result back.
func EnhancedSearchResultFromProto(res *metav1.EnhancedSearchResponse) model.EnhancedSearchResult {
	if res == nil {
		return model.EnhancedSearchResult{}
	}

	out := model.EnhancedSearchResult{Result: SearchResultFromProto(res.GetResult())}

	if len(res.GetWorks()) > 0 {
		out.Works = make(map[string]model.WorkInfo, len(res.GetWorks()))
		for id, info := range res.GetWorks() {
			out.Works[id] = WorkInfoFromProto(info)
		}
	}

	return out
}

// WorkFilterToProto converts a work filter.
func WorkFilterToProto(f model.WorkFilter) *metav1.WorkFilter {
	var kinds []metav1.WorkKind
	for _, k := range f.Kinds {
		kinds = append(kinds, metav1.WorkKind(k))
	}

	return &metav1.WorkFilter{
		Kinds:              kinds,
		ImdbId:             f.IMDbID,
		TmdbMovieId:        f.TMDBMovieID,
		TmdbTvId:           f.TMDBTVID,
		TvdbId:             f.TVDBID,
		TvmazeId:           f.TVMazeID,
		TpdbId:             f.TPDBID,
		Upc:                f.UPC,
		Season:             uint32(f.Season),
		Episode:            uint32(f.Episode),
		MinYear:            uint32(f.MinYear),
		MaxYear:            uint32(f.MaxYear),
		MinImdbScore:       uint32(f.MinIMDbScore),
		MinImdbVotes:       f.MinIMDbVotes,
		MinRtScore:         uint32(f.MinRTScore),
		MinMetacriticScore: uint32(f.MinMetacriticScore),
		MinRuntime:         uint32(f.MinRuntime),
		MaxRuntime:         uint32(f.MaxRuntime),
		Performers:         cloneStrings(f.Performers),
		LinkedOnly:         f.LinkedOnly,
	}
}

// WorkFilterFromProto converts a work filter back. A kind this build does not know is
// dropped, and a number past what the model holds is clamped: a score to 100, the rest to
// their type's range.
func WorkFilterFromProto(f *metav1.WorkFilter) model.WorkFilter {
	if f == nil {
		return model.WorkFilter{}
	}

	var kinds []model.WorkKind
	for _, k := range f.GetKinds() {
		if kind := model.WorkKind(k); k > 0 && kind.Valid() {
			kinds = append(kinds, kind)
		}
	}

	return model.WorkFilter{
		Kinds:              kinds,
		IMDbID:             f.GetImdbId(),
		TMDBMovieID:        f.GetTmdbMovieId(),
		TMDBTVID:           f.GetTmdbTvId(),
		TVDBID:             f.GetTvdbId(),
		TVMazeID:           f.GetTvmazeId(),
		TPDBID:             f.GetTpdbId(),
		UPC:                f.GetUpc(),
		Season:             narrow16(f.GetSeason()),
		Episode:            narrow16(f.GetEpisode()),
		MinYear:            narrow16(f.GetMinYear()),
		MaxYear:            narrow16(f.GetMaxYear()),
		MinIMDbScore:       score(f.GetMinImdbScore()),
		MinIMDbVotes:       f.GetMinImdbVotes(),
		MinRTScore:         score(f.GetMinRtScore()),
		MinMetacriticScore: score(f.GetMinMetacriticScore()),
		MinRuntime:         narrow16(f.GetMinRuntime()),
		MaxRuntime:         narrow16(f.GetMaxRuntime()),
		Performers:         cloneStrings(f.GetPerformers()),
		LinkedOnly:         f.GetLinkedOnly(),
	}
}

// WorkInfoToProto converts a work's description.
func WorkInfoToProto(w model.WorkInfo) *metav1.WorkInfo {
	var ratings []*metav1.Rating
	for _, r := range w.Ratings {
		ratings = append(ratings, &metav1.Rating{Source: r.Source, Score: uint32(r.Score), Votes: r.Votes})
	}

	return &metav1.WorkInfo{
		Kind:           metav1.WorkKind(w.Kind),
		Title:          w.Title,
		OriginalTitle:  w.OriginalTitle,
		Year:           uint32(w.Year),
		ImdbId:         w.IMDbID,
		TmdbId:         w.TMDBID,
		TvdbId:         w.TVDBID,
		TvmazeId:       w.TVMazeID,
		TpdbId:         w.TPDBID,
		Season:         uint32(w.Season),
		Episode:        uint32(w.Episode),
		EpisodeTitle:   w.EpisodeTitle,
		Ratings:        ratings,
		RuntimeMinutes: uint32(w.RuntimeMinutes),
		Performers:     cloneStrings(w.Performers),
		Upc:            w.UPC,
	}
}

// WorkInfoFromProto converts a work's description back. A kind this build does not know is
// WorkUnspecified.
func WorkInfoFromProto(w *metav1.WorkInfo) model.WorkInfo {
	if w == nil {
		return model.WorkInfo{}
	}

	var ratings []model.Rating
	for _, r := range w.GetRatings() {
		ratings = append(ratings, model.Rating{Source: r.GetSource(), Score: score(r.GetScore()), Votes: r.GetVotes()})
	}

	kind := model.WorkKind(w.GetKind())
	if w.GetKind() < 0 || !kind.Valid() {
		kind = model.WorkUnspecified
	}

	return model.WorkInfo{
		Kind:           kind,
		Title:          w.GetTitle(),
		OriginalTitle:  w.GetOriginalTitle(),
		Year:           narrow16(w.GetYear()),
		IMDbID:         w.GetImdbId(),
		TMDBID:         w.GetTmdbId(),
		TVDBID:         w.GetTvdbId(),
		TVMazeID:       w.GetTvmazeId(),
		TPDBID:         w.GetTpdbId(),
		Season:         narrow16(w.GetSeason()),
		Episode:        narrow16(w.GetEpisode()),
		EpisodeTitle:   w.GetEpisodeTitle(),
		Ratings:        ratings,
		RuntimeMinutes: narrow16(w.GetRuntimeMinutes()),
		Performers:     cloneStrings(w.GetPerformers()),
		UPC:            w.GetUpc(),
	}
}

// -- internals ---------------------------------------------------------------

func opToProto(o model.ChangeOp) metav1.ChangeOp {
	switch o {
	case model.OpUpsert:
		return metav1.ChangeOp_CHANGE_OP_UPSERT
	case model.OpRetract:
		return metav1.ChangeOp_CHANGE_OP_RETRACT
	default:
		return metav1.ChangeOp_CHANGE_OP_UNSPECIFIED
	}
}

func opFromProto(o metav1.ChangeOp) model.ChangeOp {
	switch o {
	case metav1.ChangeOp_CHANGE_OP_UPSERT:
		return model.OpUpsert
	case metav1.ChangeOp_CHANGE_OP_RETRACT:
		return model.OpRetract
	default:
		return model.OpUnspecified
	}
}

func reasonToProto(r model.RetractReason) metav1.RetractReason {
	switch r {
	case model.ReasonExpired:
		return metav1.RetractReason_RETRACT_REASON_EXPIRED
	case model.ReasonBlocked:
		return metav1.RetractReason_RETRACT_REASON_BLOCKED
	case model.ReasonBelowThreshold:
		return metav1.RetractReason_RETRACT_REASON_BELOW_THRESHOLD
	default:
		return metav1.RetractReason_RETRACT_REASON_UNSPECIFIED
	}
}

func reasonFromProto(r metav1.RetractReason) model.RetractReason {
	switch r {
	case metav1.RetractReason_RETRACT_REASON_EXPIRED:
		return model.ReasonExpired
	case metav1.RetractReason_RETRACT_REASON_BLOCKED:
		return model.ReasonBlocked
	case metav1.RetractReason_RETRACT_REASON_BELOW_THRESHOLD:
		return model.ReasonBelowThreshold
	default:
		return model.ReasonUnspecified
	}
}

// cloneBytes copies a slice so the two representations never alias one another.
// A converted entry outlives the message it came from, and protobuf reuses
// buffers.
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}

	return append([]byte(nil), b...)
}

// maxCompletion is 100% in hundredths: a higher floor is one nothing can meet,
// and clamping it keeps it from wrapping round in the narrower model field.
const maxCompletion = 10000

// categoriesToProto widens category ids for the wire.
func categoriesToProto(categories []uint16) []uint32 {
	if categories == nil {
		return nil
	}

	out := make([]uint32, 0, len(categories))
	for _, c := range categories {
		out = append(out, uint32(c))
	}

	return out
}

// categoriesFromProto narrows category ids from the wire. An id too wide for a
// newznab category is no category at all and is dropped rather than wrapped
// into one that exists.
func categoriesFromProto(categories []uint32) []uint16 {
	if categories == nil {
		return nil
	}

	out := make([]uint16, 0, len(categories))
	for _, c := range categories {
		if c <= math.MaxUint16 {
			out = append(out, uint16(c))
		}
	}

	return out
}

// cloneStrings copies a slice, keeping nil as nil so a round trip is exact.
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}

	return append([]string(nil), in...)
}

// narrow16 clamps a wire number into a uint16.
func narrow16(n uint32) uint16 {
	return uint16(min(n, 0xFFFF))
}

// score clamps a wire score into the 0–100 scale.
func score(n uint32) uint16 {
	return uint16(min(n, 100))
}
