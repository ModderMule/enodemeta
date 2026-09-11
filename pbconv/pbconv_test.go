package pbconv

import (
	"reflect"
	"testing"

	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/metahash"
	"github.com/ModderMule/torrent-crawler/pkg/enodemeta/model"
)

// TestEntryRoundTripLosesNothing is the test the specification asks for in §11:
// a schema addition that forgets this layer must fail loudly.
//
// Rather than listing fields — which is what rots — it fills every field of
// model.Entry by reflection, converts both ways, and compares. A new field added
// to the struct is therefore covered the moment it exists.
func TestEntryRoundTripLosesNothing(t *testing.T) {
	var entry model.Entry
	fillStruct(t, reflect.ValueOf(&entry).Elem())

	// The identity has to be the right length for its kind, since Validate and
	// Mint both check it, and reflection cannot know that.
	entry.Kind = metahash.KindBTV1
	entry.Identity = make([]byte, 20)
	for i := range entry.Identity {
		entry.Identity[i] = byte(i + 1)
	}
	t.Logf("input:  %+v", entry)

	back := EntryFromProto(EntryToProto(entry))
	t.Logf("output: %+v", back)

	if !reflect.DeepEqual(entry, back) {
		t.Errorf("a field was lost in conversion:\n in: %+v\nout: %+v", entry, back)
	}

	// Field by field, so a failure names the field rather than dumping two
	// structs and leaving the reader to diff them.
	inValue, outValue := reflect.ValueOf(entry), reflect.ValueOf(back)
	for i := 0; i < inValue.NumField(); i++ {
		name := inValue.Type().Field(i).Name
		if !reflect.DeepEqual(inValue.Field(i).Interface(), outValue.Field(i).Interface()) {
			t.Errorf("%s: got %v, want %v", name, outValue.Field(i), inValue.Field(i))
		}
	}
}

func TestKindRoundTrip(t *testing.T) {
	kinds := []metahash.Kind{
		metahash.KindUnspecified,
		metahash.KindBTV1,
		metahash.KindBTV2,
		metahash.KindNZB,
	}

	for _, k := range kinds {
		t.Logf("input:  %s", k)

		got := KindFromProto(KindToProto(k))
		t.Logf("output: %s", got)

		if got != k {
			t.Errorf("kind %s round-tripped to %s", k, got)
		}
	}

	// An unknown kind from a newer peer must not become a valid one.
	if got := KindFromProto(99); got != metahash.KindUnspecified {
		t.Errorf("an unknown wire kind must map to unspecified, got %s", got)
	}
}

func TestChangeRoundTrip(t *testing.T) {
	change := model.ReleaseChange{
		Seq:       42,
		CatalogID: "bt:v1:" + "0102030405060708090A0B0C0D0E0F1011121314",
		Op:        model.OpUpsert,
		Reason:    model.ReasonBelowThreshold,
		Entries: []model.Entry{
			{
				Kind:      metahash.KindBTV1,
				Identity:  make([]byte, 20),
				Name:      "a release",
				CatalogID: "bt:v1:AA",
				FileCount: 3,
				FileIndex: metahash.FileIndexWholeSet32,
			},
		},
	}
	t.Logf("input:  seq=%d op=%s entries=%d", change.Seq, change.Op, len(change.Entries))

	back := ChangeFromProto(ChangeToProto(change))
	t.Logf("output: seq=%d op=%s entries=%d reason=%s", back.Seq, back.Op, len(back.Entries), back.Reason)

	if !reflect.DeepEqual(change, back) {
		t.Errorf("a release change lost something:\n in: %+v\nout: %+v", change, back)
	}
}

func TestOpAndReasonRoundTrip(t *testing.T) {
	for _, op := range []model.ChangeOp{model.OpUnspecified, model.OpUpsert, model.OpRetract} {
		if got := opFromProto(opToProto(op)); got != op {
			t.Errorf("op %s round-tripped to %s", op, got)
		}
		t.Logf("output: op %s survives", op)
	}

	reasons := []model.RetractReason{
		model.ReasonUnspecified,
		model.ReasonExpired,
		model.ReasonBlocked,
		model.ReasonBelowThreshold,
	}
	for _, r := range reasons {
		if got := reasonFromProto(reasonToProto(r)); got != r {
			t.Errorf("reason %s round-tripped to %s", r, got)
		}
		t.Logf("output: reason %s survives", r)
	}
}

func TestSearchRoundTrip(t *testing.T) {
	query := model.SearchQuery{
		Query:      "ubuntu 24.04",
		Exclude:    []string{"cam", "ts"},
		Kinds:      []metahash.Kind{metahash.KindBTV1, metahash.KindBTV2},
		MinSize:    1 << 20,
		MaxSize:    1 << 40,
		MinSeeders: 3,
		MaxAgeDays: 365,
		Type:       "Iso",
		Limit:      50,
	}
	t.Logf("input:  %+v", query)

	back := SearchQueryFromProto(SearchQueryToProto(query))
	t.Logf("output: %+v", back)

	if !reflect.DeepEqual(query, back) {
		t.Errorf("a query lost something:\n in: %+v\nout: %+v", query, back)
	}

	result := model.SearchResult{
		Entries: []model.Entry{{Kind: metahash.KindBTV2, Identity: make([]byte, 32), Name: "x"}},
		Total:   1234,
	}
	if got := SearchResultFromProto(SearchResultToProto(result)); !reflect.DeepEqual(result, got) {
		t.Errorf("a result lost something:\n in: %+v\nout: %+v", result, got)
	}
}

func TestDaemonInfoRoundTrip(t *testing.T) {
	info := model.DaemonInfo{
		Daemon:           "torrent-crawler-1",
		Version:          "v0.1.0",
		ContractVersion:  1,
		Kinds:            []metahash.Kind{metahash.KindBTV1},
		LastSeq:          9000,
		PurgedThroughSeq: 100,
		Published:        50000,
		Catalogued:       1200000,
		Indexer:          "dht",
		SearchAvailable:  true,
	}
	t.Logf("input:  %+v", info)

	back := DaemonInfoFromProto(DaemonInfoToProto(info))
	t.Logf("output: %+v", back)

	if !reflect.DeepEqual(info, back) {
		t.Errorf("daemon info lost something:\n in: %+v\nout: %+v", info, back)
	}
}

func TestNilInputs(t *testing.T) {
	// A message a peer left empty must convert to a zero value rather than
	// panicking: an older peer that does not set a field is the normal case in
	// a protocol meant to evolve.
	if got := EntryFromProto(nil); !reflect.DeepEqual(got, model.Entry{}) {
		t.Errorf("a nil entry must convert to the zero value, got %+v", got)
	}
	if got := ChangeFromProto(nil); !reflect.DeepEqual(got, model.ReleaseChange{}) {
		t.Errorf("a nil change must convert to the zero value, got %+v", got)
	}
	if got := MetaFileFromProto(nil); !reflect.DeepEqual(got, model.MetaFile{}) {
		t.Errorf("a nil metafile must convert to the zero value, got %+v", got)
	}
	if got := SearchQueryFromProto(nil); !reflect.DeepEqual(got, model.SearchQuery{}) {
		t.Errorf("a nil query must convert to the zero value, got %+v", got)
	}
	if got := DaemonInfoFromProto(nil); !reflect.DeepEqual(got, model.DaemonInfo{}) {
		t.Errorf("a nil info must convert to the zero value, got %+v", got)
	}
	t.Logf("output: every nil message converts to its zero value")
}

// TestBytesAreCopied pins that the two representations never alias: a converted
// entry outlives the message it came from, and protobuf reuses buffers.
func TestBytesAreCopied(t *testing.T) {
	entry := model.Entry{
		Kind:     metahash.KindBTV1,
		Identity: []byte{1, 2, 3, 4},
		MetaHash: []byte{5, 6, 7, 8},
	}

	pb := EntryToProto(entry)
	entry.Identity[0] = 0xFF
	entry.MetaHash[0] = 0xFF
	t.Logf("input:  mutated the source after converting")

	if pb.GetIdentity()[0] == 0xFF || pb.GetMetaHash()[0] == 0xFF {
		t.Error("the converted message must not alias the source slices")
	}
	t.Logf("output: identity=%v metaHash=%v", pb.GetIdentity(), pb.GetMetaHash())
}

// -- internals ---------------------------------------------------------------

// fillStruct writes a distinctive non-zero value into every field, so a field
// that conversion forgets shows up as a zero on the way back.
func fillStruct(t *testing.T, v reflect.Value) {
	t.Helper()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString("value-" + v.Type().Field(i).Name)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			field.SetUint(uint64(i + 1))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(int64(i + 1))
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.Uint8 {
				field.SetBytes([]byte{byte(i + 1), 0xAA, 0xBB})
			}
		default:
			t.Fatalf("fillStruct does not handle %s (field %s) — extend it",
				field.Kind(), v.Type().Field(i).Name)
		}
	}
}
