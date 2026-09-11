package tags

import "testing"

// TestMetaTagsStayInRange pins the audit that justified these numbers: the
// range 0x60-0x6F was verified clean across eMule's srchybrid, eMuleQt and
// eNode-go. A tag added outside it would collide with something one of those
// trees already uses, and the symptom would be a misread row rather than an
// error.
func TestMetaTagsStayInRange(t *testing.T) {
	tags := map[string]int{
		"FTMetaKind":      FTMetaKind,
		"FTMetaVersion":   FTMetaVersion,
		"FTMetaFileIndex": FTMetaFileIndex,
		"FTMetaFilePath":  FTMetaFilePath,
		"FTMetaTotalSize": FTMetaTotalSize,
		"FTMetaID":        FTMetaID,
		"FTMetaReserved":  FTMetaReserved,
		"FTMetaSeeders":   FTMetaSeeders,
		"FTMetaPeers":     FTMetaPeers,
		"FTMetaAge":       FTMetaAge,
		"FTMetaIndexer":   FTMetaIndexer,
		"FTMetaFlags":     FTMetaFlags,
		"FTMetaMagnet":    FTMetaMagnet,
	}

	seen := make(map[int]string, len(tags))
	for name, id := range tags {
		t.Logf("input:  %-16s 0x%02X", name, id)

		if id < MetaTagRangeStart || id > MetaTagRangeEnd {
			t.Errorf("%s is 0x%02X, outside the reserved range 0x%02X-0x%02X",
				name, id, MetaTagRangeStart, MetaTagRangeEnd)
		}
		if other, dup := seen[id]; dup {
			t.Errorf("%s and %s share 0x%02X", name, other, id)
		}
		seen[id] = name
	}
	t.Logf("output: %d tags, all distinct and inside the range", len(seen))
}

// TestServerTagsAvoidTakenNumbers pins the other audit: srchybrid stops at
// 0x98, eMuleQt adds 0x99, and eNode-go's ST_NAT_PORT is 0x9D.
func TestServerTagsAvoidTakenNumbers(t *testing.T) {
	taken := map[int]string{
		0x98: "ST_UDPPORTOBFUSCATION (srchybrid)",
		0x99: "ST_IPV6 (eMuleQt)",
		0x9D: "ST_NAT_PORT (eNode-go)",
	}

	ours := map[string]int{
		"STMetaAPIFingerprint": STMetaAPIFingerprint,
		"STMetaAPI":            STMetaAPI,
		"STMetaAPIVersion":     STMetaAPIVersion,
	}

	for name, id := range ours {
		t.Logf("input:  %-22s 0x%02X", name, id)

		if other, clash := taken[id]; clash {
			t.Errorf("%s is 0x%02X, which is %s", name, id, other)
		}
	}
	t.Logf("output: no clash with the numbers the three trees already use")
}

// TestCapabilityBitsDoNotClash pins the two capability words against the bits
// their neighbours hold.
func TestCapabilityBitsDoNotClash(t *testing.T) {
	const (
		srvCapIPv6EMuleQt = 0x1000 // eMuleQt's SRVCAP_IPV6
		enodeHighestFlag  = 0x8000 // eNode-go's FlagNatRendezvous
	)

	t.Logf("input:  SrvCapMetaSearch=0x%04X FlagMetaSearch=0x%05X SrvCapUDPMetaSearch=0x%02X",
		SrvCapMetaSearch, FlagMetaSearch, SrvCapUDPMetaSearch)

	if SrvCapMetaSearch == srvCapIPv6EMuleQt {
		t.Error("SrvCapMetaSearch collides with eMuleQt's IPv6 bit")
	}
	if SrvCapMetaSearch != srvCapIPv6EMuleQt<<1 {
		t.Errorf("SrvCapMetaSearch should be the next bit after IPv6's 0x%04X, got 0x%04X",
			srvCapIPv6EMuleQt, SrvCapMetaSearch)
	}
	if FlagMetaSearch <= enodeHighestFlag {
		t.Errorf("FlagMetaSearch 0x%X must be above eNode-go's highest existing flag 0x%X",
			FlagMetaSearch, enodeHighestFlag)
	}

	// The UDP opt-in shares a value with SRVCAP_UDP_NEWTAGS_LARGEFILES (0x01),
	// so it must be the next bit rather than that one.
	if SrvCapUDPMetaSearch == 0x01 {
		t.Error("SrvCapUDPMetaSearch collides with SRVCAP_UDP_NEWTAGS_LARGEFILES")
	}
	t.Logf("output: every capability bit is clear of its neighbours")
}

// TestMaxSourcesStaysUnderTheSpamHeuristic pins the number to the reason for it:
// eMule's heuristic fires above 100 sources, so a catalogue row reporting more
// could be filtered as spam by the client it was sent to.
func TestMaxSourcesStaysUnderTheSpamHeuristic(t *testing.T) {
	const eMuleSpamThreshold = 100

	t.Logf("input:  MaxSources=%d, eMule's heuristic fires above %d", MaxSources, eMuleSpamThreshold)

	if MaxSources >= eMuleSpamThreshold {
		t.Errorf("MaxSources %d would trip the spam heuristic at %d", MaxSources, eMuleSpamThreshold)
	}
	t.Logf("output: %d leaves a margin of %d", MaxSources, eMuleSpamThreshold-MaxSources)
}
