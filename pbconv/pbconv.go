// Package pbconv converts between the domain types in model and the generated
// protobuf types.
//
// It is the only place the two meet. That is the rule from §6.3 of the
// specification: keeping generated structs out of the rest of the code is what
// makes a second wire representation an adapter rather than a rewrite, and this
// package is that adapter's seam.
package pbconv

import (
	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/metahash"
	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/model"

	metav1 "github.com/ModderMule/torrent-crawler/pkg/enodemeta/gen/enode/meta/v1"
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

	var exclude []string
	if r.GetExclude() != nil {
		exclude = append([]string(nil), r.GetExclude()...)
	}

	return model.SearchQuery{
		Query:      r.GetQuery(),
		Exclude:    exclude,
		Kinds:      kinds,
		MinSize:    r.GetMinSize(),
		MaxSize:    r.GetMaxSize(),
		MinSeeders: r.GetMinSeeders(),
		MaxAgeDays: r.GetMaxAgeDays(),
		Type:       r.GetType(),
		Limit:      r.GetLimit(),
	}
}

// SearchResultToProto converts a result.
func SearchResultToProto(res model.SearchResult) *metav1.SearchResponse {
	return &metav1.SearchResponse{
		Entries: EntriesToProto(res.Entries),
		Total:   res.Total,
	}
}

// SearchResultFromProto converts a result back.
func SearchResultFromProto(res *metav1.SearchResponse) model.SearchResult {
	if res == nil {
		return model.SearchResult{}
	}

	return model.SearchResult{
		Entries: EntriesFromProto(res.GetEntries()),
		Total:   res.GetTotal(),
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
		Indexer:          info.Indexer,
		SearchAvailable:  info.SearchAvailable,
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
		Indexer:          res.GetIndexer(),
		SearchAvailable:  res.GetSearchAvailable(),
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
