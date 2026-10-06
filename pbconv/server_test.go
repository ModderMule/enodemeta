package pbconv

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ModderMule/enodemeta/model"

	metav1 "github.com/ModderMule/enodemeta/gen/enode/meta/v1"
)

// TestServerFileRoundTripLosesNothing fills every field of model.ServerFile by
// reflection, so a field added to the struct without a converter fails here.
func TestServerFileRoundTripLosesNothing(t *testing.T) {
	var file model.ServerFile
	fillStruct(t, reflect.ValueOf(&file).Elem())
	t.Logf("input:  %+v", file)

	back := ServerFileFromProto(ServerFileToProto(file))
	t.Logf("output: %+v", back)

	compareFields(t, file, back)
}

// TestServerFileIdentifiesNoClient pins the privacy rule of the contract: the
// wire message carries exactly these fields, none of which says who shares the
// file. Adding a field means editing this list, which is the moment to ask
// whether it could identify a client.
func TestServerFileIdentifiesNoClient(t *testing.T) {
	want := []string{
		"album", "artist", "bitrate", "codec", "complete_sources", "hash",
		"name", "runtime_seconds", "size", "sources", "title", "type",
	}

	fields := (&metav1.ServerFile{}).ProtoReflect().Descriptor().Fields()
	got := make([]string, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		got = append(got, string(fields.Get(i).Name()))
	}
	sort.Strings(got)
	t.Logf("input:  %v", want)
	t.Logf("output: %v", got)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ServerFile fields changed:\n got: %v\nwant: %v", got, want)
	}
}

func TestServerInfoRoundTripLosesNothing(t *testing.T) {
	var info model.ServerInfo
	fillStruct(t, reflect.ValueOf(&info).Elem())
	t.Logf("input:  %+v", info)

	back := ServerInfoFromProto(ServerInfoToProto(info))
	t.Logf("output: %+v", back)

	compareFields(t, info, back)
}

func TestServerSearchRoundTripLosesNothing(t *testing.T) {
	var query model.ServerSearchQuery
	fillStruct(t, reflect.ValueOf(&query).Elem())
	query.Exclude = []string{"sample", "trailer"}
	t.Logf("input:  %+v", query)

	gotQuery := ServerSearchQueryFromProto(ServerSearchQueryToProto(query))
	t.Logf("output: %+v", gotQuery)
	compareFields(t, query, gotQuery)

	var result model.ServerSearchResult
	fillStruct(t, reflect.ValueOf(&result).Elem())
	result.Files = serverFiles(t, 2)
	t.Logf("input:  %+v", result)

	gotResult := ServerSearchResultFromProto(ServerSearchResultToProto(result))
	t.Logf("output: %+v", gotResult)
	compareFields(t, result, gotResult)
}

func TestBrowseRoundTripLosesNothing(t *testing.T) {
	var query model.BrowseQuery
	fillStruct(t, reflect.ValueOf(&query).Elem())
	t.Logf("input:  %+v", query)

	gotQuery := BrowseQueryFromProto(BrowseQueryToProto(query))
	t.Logf("output: %+v", gotQuery)
	compareFields(t, query, gotQuery)

	var page model.BrowsePage
	fillStruct(t, reflect.ValueOf(&page).Elem())
	page.Files = serverFiles(t, 3)
	t.Logf("input:  %+v", page)

	gotPage := BrowsePageFromProto(BrowsePageToProto(page))
	t.Logf("output: %+v", gotPage)
	compareFields(t, page, gotPage)
}

// TestBrowsePageDone pins when a walk is complete: a reset page has no token
// either, and must not be mistaken for the last page.
func TestBrowsePageDone(t *testing.T) {
	cases := []struct {
		name string
		page model.BrowsePage
		want bool
	}{
		{"more pages", model.BrowsePage{NextPageToken: []byte{1}}, false},
		{"last page", model.BrowsePage{}, true},
		{"reset", model.BrowsePage{Reset: true}, false},
	}

	for _, c := range cases {
		got := c.page.Done()
		t.Logf("input:  %s %+v", c.name, c.page)
		t.Logf("output: %v", got)

		if got != c.want {
			t.Errorf("%s: Done() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestServerNilInputs(t *testing.T) {
	if got := ServerFileFromProto(nil); !reflect.DeepEqual(got, model.ServerFile{}) {
		t.Errorf("a nil file must convert to the zero value, got %+v", got)
	}
	if got := ServerInfoFromProto(nil); !reflect.DeepEqual(got, model.ServerInfo{}) {
		t.Errorf("a nil info must convert to the zero value, got %+v", got)
	}
	if got := ServerSearchQueryFromProto(nil); !reflect.DeepEqual(got, model.ServerSearchQuery{}) {
		t.Errorf("a nil query must convert to the zero value, got %+v", got)
	}
	if got := ServerSearchResultFromProto(nil); !reflect.DeepEqual(got, model.ServerSearchResult{}) {
		t.Errorf("a nil result must convert to the zero value, got %+v", got)
	}
	if got := BrowseQueryFromProto(nil); !reflect.DeepEqual(got, model.BrowseQuery{}) {
		t.Errorf("a nil browse query must convert to the zero value, got %+v", got)
	}
	if got := BrowsePageFromProto(nil); !reflect.DeepEqual(got, model.BrowsePage{}) {
		t.Errorf("a nil page must convert to the zero value, got %+v", got)
	}
	t.Logf("output: every nil message converts to its zero value")
}

// TestServerBytesAreCopied pins that a converted file does not alias the hash
// or the token of the message it came from.
func TestServerBytesAreCopied(t *testing.T) {
	file := model.ServerFile{Hash: []byte{1, 2, 3, 4}}
	page := model.BrowsePage{NextPageToken: []byte{5, 6, 7, 8}}

	pbFile, pbPage := ServerFileToProto(file), BrowsePageToProto(page)
	file.Hash[0] = 0xFF
	page.NextPageToken[0] = 0xFF
	t.Logf("input:  mutated the source after converting")
	t.Logf("output: hash=%v token=%v", pbFile.GetHash(), pbPage.GetNextPageToken())

	if pbFile.GetHash()[0] == 0xFF || pbPage.GetNextPageToken()[0] == 0xFF {
		t.Error("the converted message must not alias the source slices")
	}
}

// -- internals ---------------------------------------------------------------

// serverFiles returns n fully filled files that differ by hash.
func serverFiles(t *testing.T, n int) []model.ServerFile {
	t.Helper()

	files := make([]model.ServerFile, n)
	for i := range files {
		fillStruct(t, reflect.ValueOf(&files[i]).Elem())
		files[i].Hash = []byte{byte(i + 1), 0xAA, 0xBB}
	}

	return files
}
