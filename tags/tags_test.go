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
		"FTMetaNetwork":   FTMetaNetwork,
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
		0xAB: "ST_IPV6_STATUS (eMuleQt, eNode-go)",
		0xAD: "CT_MOD_YOUR_IP (eNode-go)",
		0xAE: "CT_MOD_IPV6 (eNode-go)",
		0xAF: "CT_MOD_SVR_IPV6 (eNode-go)",
	}

	ours := map[string]int{
		"STMetaAPIFingerprint": STMetaAPIFingerprint,
		"STMetaAPI":            STMetaAPI,
		"STMetaAPIVersion":     STMetaAPIVersion,

		"STServerSearch":            STServerSearch,
		"STServerSearchFingerprint": STServerSearchFingerprint,
	}

	seen := map[int]string{}
	for name, id := range ours {
		t.Logf("input:  %-26s 0x%02X", name, id)

		if other, clash := taken[id]; clash {
			t.Errorf("%s is 0x%02X, which is %s", name, id, other)
		}
		if other, clash := seen[id]; clash {
			t.Errorf("%s and %s are both 0x%02X", name, other, id)
		}
		seen[id] = name
	}
	t.Logf("output: no clash with the numbers the three trees already use")
}

// TestCapabilityBitsDoNotClash pins the two capability words against the bits
// their neighbours hold.
func TestCapabilityBitsDoNotClash(t *testing.T) {
	const (
		srvCapNatLugdunum = 0x1000 // Lugdunum eserver / NeoLoader NAT traversal
		srvCapIPv6Neo     = 0x2000 // NeoLoader's SRVCAP_IPV6
		srvCapHighestMFC  = 0x0800 // SRVCAP_REQUIRECRYPT, the last bit MFC eMule defines
		enodeHighestFlag  = 0x8000 // eNode-go's FlagNatRendezvous
	)

	t.Logf("input:  SrvCapMetaSearch=0x%05X FlagMetaSearch=0x%05X SrvCapUDPMetaSearch=0x%02X",
		SrvCapMetaSearch, FlagMetaSearch, SrvCapUDPMetaSearch)

	if SrvCapMetaSearch == srvCapNatLugdunum {
		t.Error("SrvCapMetaSearch collides with Lugdunum's NAT-traversal bit")
	}
	if SrvCapMetaSearch == srvCapIPv6Neo {
		t.Error("SrvCapMetaSearch collides with NeoLoader's IPv6 bit")
	}
	if SrvCapMetaSearch <= srvCapHighestMFC {
		t.Errorf("SrvCapMetaSearch 0x%X must be above the MFC login bits (up to 0x%X)",
			SrvCapMetaSearch, srvCapHighestMFC)
	}
	if FlagMetaSearch <= enodeHighestFlag {
		t.Errorf("FlagMetaSearch 0x%X must be above eNode-go's highest existing flag 0x%X",
			FlagMetaSearch, enodeHighestFlag)
	}

	// The server-search bit is a server flag like FlagMetaSearch, and must be a
	// bit of its own above it.
	t.Logf("input:  FlagServerSearch=0x%05X", FlagServerSearch)
	if FlagServerSearch <= FlagMetaSearch || FlagServerSearch&(FlagServerSearch-1) != 0 {
		t.Errorf("FlagServerSearch 0x%X must be a single bit above FlagMetaSearch 0x%X",
			FlagServerSearch, FlagMetaSearch)
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
