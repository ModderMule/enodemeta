package nzbmeta

// Shortfall is what a document is short of *by its own account*.
//
// It is not a statement about any server. A file whose subject says "(1/42)"
// while listing 40 segments means the indexer never saw two articles; those
// articles may well still be on every provider, but this document cannot ask for
// them. Whether the servers still hold what it *does* list is a separate
// question with a separate answer — the availability probe.
//
// The crawler uses Percent as the completion figure it ranks and publishes on,
// and LikelyRecoverable as the FlagNeedsPAR2 bit. Both are free: no network.
type Shortfall struct {
	// ListedSegments is how many articles the document lists, which is what a
	// client will actually try to fetch.
	ListedSegments int

	// MissingSegments is how many the subject counters claim exist and the
	// document does not list.
	MissingSegments int

	// MissingBytes prices MissingSegments at each file's own mean article size.
	// Estimated: the document says nothing about the size of an article it does
	// not list.
	MissingBytes uint64

	// RecoveryBytes is the encoded size of the PAR2 recovery volumes the release
	// ships. A shortfall smaller than this is very likely repairable, which is
	// why a bare percentage on its own would be alarmism.
	RecoveryBytes uint64

	// UnknownFiles counts files with no part counter, so with no opinion either
	// way. A release that is entirely these is not "100% complete" — it is
	// unknown.
	UnknownFiles int
}

// Percent is the percentage of the claimed article count that the document
// actually lists.
//
// 100 when nothing is known to be missing, *including* when nothing is knowable.
// Read it with UnknownFiles: a fully obfuscated release reports 100 because the
// question cannot be answered, and treating that as "all missing" would read
// every obfuscated release as unfetchable.
func (s Shortfall) Percent() int {
	claimed := s.ListedSegments + s.MissingSegments
	if claimed <= 0 {
		return 100
	}

	return int(int64(s.ListedSegments) * 100 / int64(claimed))
}

// Complete reports whether nothing is known to be missing.
func (s Shortfall) Complete() bool { return s.MissingSegments == 0 }

// LikelyRecoverable reports whether the shortfall is small enough for the
// shipped recovery data to plausibly cover.
//
// Byte-level and deliberately crude: the PAR2 block size is not knowable until
// the index file has been downloaded, so this is the best answer available
// without the network.
func (s Shortfall) LikelyRecoverable() bool {
	return s.MissingBytes == 0 || s.RecoveryBytes >= s.MissingBytes
}

// Shortfall sums what the document is short of, over its files.
func (d *Document) Shortfall() Shortfall {
	var out Shortfall

	for i := range d.Files {
		f := d.Files[i]

		out.ListedSegments += len(f.Segments)

		if f.PartsTotal <= 0 {
			out.UnknownFiles++
		}

		missing := f.MissingSegmentCount()
		out.MissingSegments += missing

		// Priced at this file's own mean rather than the release's: a release
		// mixes 700 KB archive volumes with a 2 KB .nfo, and one mean over all
		// of them would misprice whichever kind actually lost articles.
		out.MissingBytes += uint64(missing) * f.MeanSegmentBytes()

		if f.IsPAR2Volume() {
			out.RecoveryBytes += f.EncodedBytes()
		}
	}

	return out
}
