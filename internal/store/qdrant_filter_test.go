package store

import "testing"

func TestBuildSearchFilterBuildsDocumentAndSourceTypeConditions(t *testing.T) {
	filter := buildSearchFilter(SearchFilter{
		DocumentIDs: []uint{7, 8},
		SourceTypes: []string{"policy", "faq"},
	})

	if filter == nil {
		t.Fatal("filter = nil, want conditions")
	}
	if len(filter.Must) != 2 {
		t.Fatalf("must condition count = %d, want 2", len(filter.Must))
	}

	docMatch := filter.Must[0].GetField().GetMatch()
	docIDs := docMatch.GetIntegers().GetIntegers()
	if len(docIDs) != 2 || docIDs[0] != 7 || docIDs[1] != 8 {
		t.Fatalf("document_id match = %#v, want [7 8]", docIDs)
	}

	sourceMatch := filter.Must[1].GetField().GetMatch()
	keywords := sourceMatch.GetKeywords().GetStrings()
	if len(keywords) != 2 || keywords[0] != "faq" || keywords[1] != "policy" {
		t.Fatalf("source_type keywords = %#v, want [faq policy]", keywords)
	}
}

func TestBuildSearchFilterReturnsNilForEmptyFilter(t *testing.T) {
	if got := buildSearchFilter(SearchFilter{}); got != nil {
		t.Fatalf("filter = %#v, want nil", got)
	}
}
