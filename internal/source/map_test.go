package source

import "testing"

func TestSourceMapExactRangesAndOwnership(t *testing.T) {
	entries := []Mapping{
		{Generated: ByteSpan{0, 30}, Kind: Synthetic},
		{Generated: ByteSpan{2, 20}, Source: Location{"input.gox", ByteSpan{40, 80}}, Kind: Derived},
		{Generated: ByteSpan{5, 9}, Source: Location{"input.gox", ByteSpan{50, 54}}, Kind: Exact, Expansions: []Expansion{{ID: 1}}},
		{Generated: ByteSpan{11, 15}, Source: Location{"input.gox", ByteSpan{50, 54}}, Kind: Exact},
		{Generated: ByteSpan{16, 19}, Kind: Synthetic},
	}
	mapping := NewMap(entries)
	entries[2].Expansions[0].ID = 8
	entry, ok := mapping.Generated(ByteSpan{6, 8})
	if !ok || entry.Kind != Exact || entry.Source.Span != (ByteSpan{51, 53}) || entry.Expansions[0].ID != 1 {
		t.Fatalf("exact subrange = %+v", entry)
	}
	entry.Expansions[0].ID = 9
	entry, _ = mapping.Generated(ByteSpan{6, 8})
	if entry.Expansions[0].ID != 1 {
		t.Fatal("query mutated map")
	}
	entry, _ = mapping.Generated(ByteSpan{8, 12})
	if entry.Kind != Derived || entry.Source.Span != (ByteSpan{40, 80}) {
		t.Fatalf("cross-token range = %+v", entry)
	}
	entry, _ = mapping.Generated(ByteSpan{16, 19})
	if entry.Kind != Synthetic || entry.Source.File != "" {
		t.Fatalf("synthetic child inherited source: %+v", entry)
	}
	occurrences := mapping.Original(Location{"input.gox", ByteSpan{51, 53}})
	if len(occurrences) != 2 || occurrences[0].Generated != (ByteSpan{6, 8}) || occurrences[1].Generated != (ByteSpan{12, 14}) {
		t.Fatalf("reverse exact ranges = %+v", occurrences)
	}
	if len(mapping.Original(Location{"input.gox", ByteSpan{49, 53}})) != 0 {
		t.Fatal("reverse query snapped to overlapping identifier")
	}
	all := mapping.Entries()
	all[0].Kind = Exact
	if mapping.Entries()[0].Kind != Synthetic {
		t.Fatal("entries mutated map")
	}
}
