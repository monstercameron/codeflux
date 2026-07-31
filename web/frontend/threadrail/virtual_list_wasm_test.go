//go:build js && wasm

package threadrail

import (
	"fmt"
	"testing"
	"time"
)

func TestWASMOneThousandRowVirtualListContractHasStableBoundedIdentity(t *testing.T) {
	fixture := newRailFixture(t)
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	state, _ := BeginFirstPage(fixture.state(t))
	for pageIndex := 0; pageIndex < 20; pageIndex++ {
		threads := make([]Thread, 50)
		for index := range threads {
			absolute := pageIndex*50 + index
			threads[index] = fixture.thread(
				t, fmt.Sprintf("Thread %04d", absolute+1), base.Add(-time.Duration(absolute)*time.Second),
				TaskStateNone, AttentionNone, 1, false,
			)
		}
		hasMore := pageIndex < 19
		requestCursor, nextCursor := Cursor(""), Cursor("")
		if pageIndex > 0 {
			requestCursor = Cursor(fmt.Sprintf("page-%02d", pageIndex))
		}
		if hasMore {
			nextCursor = Cursor(fmt.Sprintf("page-%02d", pageIndex+1))
		}
		var err error
		state, err = ApplyPage(state, Page{
			RequestCursor: requestCursor, Threads: threads, NextCursor: nextCursor, HasMore: hasMore,
		})
		if err != nil {
			t.Fatal(err)
		}
		if hasMore {
			var ok bool
			state, _, ok = BeginNextPage(state)
			if !ok {
				t.Fatalf("page %d did not request next", pageIndex)
			}
		}
	}
	contract, err := NewVirtualListContract(state, 560)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[RowKey]struct{}, len(contract.Rows))
	for _, row := range contract.Rows {
		if row.Key() == "" {
			t.Fatal("virtual row has no stable key")
		}
		if _, duplicate := seen[row.Key()]; duplicate {
			t.Fatalf("duplicate virtual key %q", row.Key())
		}
		seen[row.Key()] = struct{}{}
	}
	if len(contract.Rows) != 1000 || contract.Height != 560 || contract.ShouldRequestNextPage(999) {
		t.Fatalf("WASM contract rows=%d height=%v hasMore=%v", len(contract.Rows), contract.Height, contract.HasMore)
	}
}
