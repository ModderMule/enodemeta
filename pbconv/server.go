package pbconv

import (
	"github.com/ModderMule/enodemeta/model"

	metav1 "github.com/ModderMule/enodemeta/gen/enode/meta/v1"
)

// ServerFileToProto converts one catalogue file.
func ServerFileToProto(f model.ServerFile) *metav1.ServerFile {
	return &metav1.ServerFile{
		Hash:            cloneBytes(f.Hash),
		Size:            f.Size,
		Name:            f.Name,
		Type:            f.Type,
		Sources:         f.Sources,
		CompleteSources: f.CompleteSources,
		Title:           f.Title,
		Artist:          f.Artist,
		Album:           f.Album,
		RuntimeSeconds:  f.RuntimeSeconds,
		Bitrate:         f.Bitrate,
		Codec:           f.Codec,
	}
}

// ServerFileFromProto converts one catalogue file back.
func ServerFileFromProto(f *metav1.ServerFile) model.ServerFile {
	if f == nil {
		return model.ServerFile{}
	}

	return model.ServerFile{
		Hash:            cloneBytes(f.GetHash()),
		Size:            f.GetSize(),
		Name:            f.GetName(),
		Type:            f.GetType(),
		Sources:         f.GetSources(),
		CompleteSources: f.GetCompleteSources(),
		Title:           f.GetTitle(),
		Artist:          f.GetArtist(),
		Album:           f.GetAlbum(),
		RuntimeSeconds:  f.GetRuntimeSeconds(),
		Bitrate:         f.GetBitrate(),
		Codec:           f.GetCodec(),
	}
}

// ServerFilesToProto converts a page of catalogue files.
func ServerFilesToProto(files []model.ServerFile) []*metav1.ServerFile {
	if len(files) == 0 {
		return nil
	}

	out := make([]*metav1.ServerFile, 0, len(files))
	for _, f := range files {
		out = append(out, ServerFileToProto(f))
	}

	return out
}

// ServerFilesFromProto converts a page of catalogue files back.
func ServerFilesFromProto(files []*metav1.ServerFile) []model.ServerFile {
	if len(files) == 0 {
		return nil
	}

	out := make([]model.ServerFile, 0, len(files))
	for _, f := range files {
		out = append(out, ServerFileFromProto(f))
	}

	return out
}

// ServerInfoToProto converts a server description.
func ServerInfoToProto(info model.ServerInfo) *metav1.ServerInfo {
	return &metav1.ServerInfo{
		ContractVersion:          info.ContractVersion,
		Name:                     info.Name,
		Files:                    info.Files,
		SearchAvailable:          info.SearchAvailable,
		BrowseAvailable:          info.BrowseAvailable,
		MaxSearchLimit:           info.MaxSearchLimit,
		MaxBrowseLimit:           info.MaxBrowseLimit,
		BrowseMinIntervalSeconds: info.BrowseMinIntervalSeconds,
		CatalogEpoch:             info.CatalogEpoch,
	}
}

// ServerInfoFromProto converts a server description back.
func ServerInfoFromProto(info *metav1.ServerInfo) model.ServerInfo {
	if info == nil {
		return model.ServerInfo{}
	}

	return model.ServerInfo{
		ContractVersion:          info.GetContractVersion(),
		Name:                     info.GetName(),
		Files:                    info.GetFiles(),
		SearchAvailable:          info.GetSearchAvailable(),
		BrowseAvailable:          info.GetBrowseAvailable(),
		MaxSearchLimit:           info.GetMaxSearchLimit(),
		MaxBrowseLimit:           info.GetMaxBrowseLimit(),
		BrowseMinIntervalSeconds: info.GetBrowseMinIntervalSeconds(),
		CatalogEpoch:             info.GetCatalogEpoch(),
	}
}

// ServerSearchQueryToProto converts a server-to-server query.
func ServerSearchQueryToProto(q model.ServerSearchQuery) *metav1.ServerSearchRequest {
	return &metav1.ServerSearchRequest{
		Query:      q.Query,
		Exclude:    cloneStrings(q.Exclude),
		Type:       q.Type,
		MinSize:    q.MinSize,
		MaxSize:    q.MaxSize,
		MinSources: q.MinSources,
		Limit:      q.Limit,
		Offset:     q.Offset,
	}
}

// ServerSearchQueryFromProto converts a server-to-server query back.
func ServerSearchQueryFromProto(r *metav1.ServerSearchRequest) model.ServerSearchQuery {
	if r == nil {
		return model.ServerSearchQuery{}
	}

	return model.ServerSearchQuery{
		Query:      r.GetQuery(),
		Exclude:    cloneStrings(r.GetExclude()),
		Type:       r.GetType(),
		MinSize:    r.GetMinSize(),
		MaxSize:    r.GetMaxSize(),
		MinSources: r.GetMinSources(),
		Limit:      r.GetLimit(),
		Offset:     r.GetOffset(),
	}
}

// ServerSearchResultToProto converts a server-to-server result.
func ServerSearchResultToProto(res model.ServerSearchResult) *metav1.ServerSearchResponse {
	return &metav1.ServerSearchResponse{
		Files:      ServerFilesToProto(res.Files),
		Total:      res.Total,
		NextOffset: res.NextOffset,
	}
}

// ServerSearchResultFromProto converts a server-to-server result back.
func ServerSearchResultFromProto(res *metav1.ServerSearchResponse) model.ServerSearchResult {
	if res == nil {
		return model.ServerSearchResult{}
	}

	return model.ServerSearchResult{
		Files:      ServerFilesFromProto(res.GetFiles()),
		Total:      res.GetTotal(),
		NextOffset: res.GetNextOffset(),
	}
}

// BrowseQueryToProto converts a request for one page of a catalogue walk.
func BrowseQueryToProto(q model.BrowseQuery) *metav1.BrowseFilesRequest {
	return &metav1.BrowseFilesRequest{
		PageToken: cloneBytes(q.PageToken),
		Limit:     q.Limit,
	}
}

// BrowseQueryFromProto converts a request for one page of a catalogue walk back.
func BrowseQueryFromProto(r *metav1.BrowseFilesRequest) model.BrowseQuery {
	if r == nil {
		return model.BrowseQuery{}
	}

	return model.BrowseQuery{
		PageToken: cloneBytes(r.GetPageToken()),
		Limit:     r.GetLimit(),
	}
}

// BrowsePageToProto converts one page of a catalogue walk.
func BrowsePageToProto(p model.BrowsePage) *metav1.BrowseFilesResponse {
	return &metav1.BrowseFilesResponse{
		Files:         ServerFilesToProto(p.Files),
		NextPageToken: cloneBytes(p.NextPageToken),
		CatalogEpoch:  p.CatalogEpoch,
		Reset_:        p.Reset,
		Total:         p.Total,
	}
}

// BrowsePageFromProto converts one page of a catalogue walk back.
func BrowsePageFromProto(p *metav1.BrowseFilesResponse) model.BrowsePage {
	if p == nil {
		return model.BrowsePage{}
	}

	return model.BrowsePage{
		Files:         ServerFilesFromProto(p.GetFiles()),
		NextPageToken: cloneBytes(p.GetNextPageToken()),
		CatalogEpoch:  p.GetCatalogEpoch(),
		Reset:         p.GetReset_(),
		Total:         p.GetTotal(),
	}
}
