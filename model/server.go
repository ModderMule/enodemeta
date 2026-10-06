package model

// ServerContractVersion is the major version of the server-to-server search
// contract this module describes.
const ServerContractVersion = 1

// ServerFile is one file of a server's own eD2K catalogue, as another server
// sees it.
//
// It has no field that identifies who shares the file, and must never grow one:
// a user chose the server they logged into, not the servers it talks to.
type ServerFile struct {
	// Hash is the file's 16-byte eD2K MD4. With Size it is the file's identity.
	Hash []byte
	Size uint64
	Name string

	// Type is the eD2K file type, empty when unknown.
	Type string

	// Sources is how many users of the answering server share the file, and
	// CompleteSources how many of them have all of it. The same file from
	// several servers must not have them added up.
	Sources         uint32
	CompleteSources uint32

	Title          string
	Artist         string
	Album          string
	RuntimeSeconds uint32
	Bitrate        uint32
	Codec          string
}

// ServerInfo describes a server and the limits it applies to other servers.
type ServerInfo struct {
	ContractVersion uint32
	Name            string

	// Files is how many files the catalogue holds.
	Files uint64

	SearchAvailable bool
	BrowseAvailable bool

	// MaxSearchLimit and MaxBrowseLimit are the most files one answer carries.
	MaxSearchLimit uint32
	MaxBrowseLimit uint32

	// BrowseMinIntervalSeconds is how long to wait between two browse calls.
	BrowseMinIntervalSeconds uint32

	// CatalogEpoch changes when the catalogue was rebuilt rather than edited.
	CatalogEpoch uint64
}

// ServerSearchQuery is a keyword search over another server's catalogue.
type ServerSearchQuery struct {
	// Query is the keywords, separated by spaces. All must match.
	Query   string
	Exclude []string

	Type    string
	MinSize uint64
	MaxSize uint64

	MinSources uint32

	Limit  uint32
	Offset uint32
}

// ServerSearchResult is what a server-to-server search returned.
type ServerSearchResult struct {
	Files []ServerFile

	// Total is how many files the search reached, across all its pages.
	Total uint64

	// NextOffset is where the next page starts. Zero means there is none.
	NextOffset uint32
}

// BrowseQuery asks for one page of a catalogue walk.
type BrowseQuery struct {
	// PageToken is empty for the first page, then the previous page's
	// NextPageToken, unchanged.
	PageToken []byte
	Limit     uint32
}

// BrowsePage is one page of a catalogue walk.
type BrowsePage struct {
	Files []ServerFile

	// NextPageToken continues the walk. Empty means the walk is complete,
	// unless Reset is set.
	NextPageToken []byte

	CatalogEpoch uint64

	// Reset says the token is no longer usable and the walk must start again.
	Reset bool

	// Total is how many files the catalogue holds now.
	Total uint64
}

// Done reports whether the page ends a walk that ran to completion. A reset
// page ends nothing: the walk it belongs to was abandoned.
func (p BrowsePage) Done() bool {
	return !p.Reset && len(p.NextPageToken) == 0
}
