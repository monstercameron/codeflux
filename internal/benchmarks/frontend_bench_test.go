package benchmarks

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/timeline"
)

// threadPage builds one bounded durable page as the thread service returns it.
func threadPage(
	tb testing.TB,
	threadID domain.ThreadID,
	firstSequence uint64,
	count int,
	hasOlder bool,
) timeline.MessagePage {
	tb.Helper()
	messages := make([]timeline.DurableMessage, 0, count)
	for index := range count {
		messageID, err := domain.NewMessageID()
		if err != nil {
			tb.Fatalf("new message ID: %v", err)
		}
		sequence := firstSequence + uint64(index)
		messages = append(messages, timeline.DurableMessage{
			ID:       messageID,
			ThreadID: threadID,
			Role:     "assistant",
			Body: timeline.RedactedBody{
				Text:          "A durable assistant turn describing what it changed and why.",
				OriginalBytes: 61,
			},
			Revision:  sequence,
			Sequence:  sequence,
			CreatedAt: time.UnixMicro(int64(sequence)).UTC(),
		})
	}
	page := timeline.MessagePage{
		ThreadID: threadID,
		Messages: messages,
		HasOlder: hasOlder,
	}
	if hasOlder {
		page.NextCursor = timeline.Cursor("cursor-" + itoa(int(firstSequence)))
	}
	return page
}

// olderPage stamps the request cursor the feed is waiting on. The timeline
// deliberately refuses a page that does not echo the cursor it was requested
// with, so a benchmark that skipped this would only ever measure the refusal.
func olderPage(
	tb testing.TB,
	feed timeline.MessageFeed,
	firstSequence uint64,
	count int,
) timeline.MessagePage {
	tb.Helper()
	page := threadPage(tb, feed.ThreadID, firstSequence, count, true)
	page.RequestCursor = feed.OlderCursor
	return page
}

// BenchmarkThreadRenderAndPagination is M22-083.
//
// Both halves are measured because they fail differently: the initial page is
// what a user waits for when they open a thread, while upward pagination is
// what they wait for repeatedly while scrolling back, and a join that is
// quadratic in feed length only shows up in the second.
func BenchmarkThreadRenderAndPagination(b *testing.B) {
	LogEnvironment(b)
	threadID, err := domain.NewThreadID()
	if err != nil {
		b.Fatalf("new thread ID: %v", err)
	}
	const pageSize = 50

	b.Run("initial-render", func(b *testing.B) {
		page := threadPage(b, threadID, 10_000, pageSize, true)
		var feed timeline.MessageFeed
		Measure(b, nil, func() {
			result, applyErr := timeline.ApplyNewestMessagePage(page)
			if applyErr != nil {
				b.Fatalf("apply newest page: %v", applyErr)
			}
			feed = result
		})
		if len(feed.Messages) != pageSize {
			b.Fatalf("initial feed holds %d messages, want %d", len(feed.Messages), pageSize)
		}
	})

	b.Run("upward-pagination", func(b *testing.B) {
		// The measured step prepends onto a feed that already holds many
		// pages, which is the state a user reaches after scrolling back — and
		// the state where a join whose cost grows with feed length hurts.
		const existingPages = 20
		feed, applyErr := timeline.ApplyNewestMessagePage(
			threadPage(b, threadID, 10_000, pageSize, true),
		)
		if applyErr != nil {
			b.Fatalf("seed feed: %v", applyErr)
		}
		for page := range existingPages {
			first := uint64(10_000 - (page+1)*pageSize)
			begun, beginErr := timeline.BeginOlderMessagePage(feed)
			if beginErr != nil {
				b.Fatalf("begin older page %d: %v", page, beginErr)
			}
			joined, joinErr := timeline.PrependOlderMessagePage(
				begun, olderPage(b, begun, first, pageSize),
			)
			if joinErr != nil {
				b.Fatalf("prepend older page %d: %v", page, joinErr)
			}
			feed = joined
		}
		seeded := feed
		older := olderPage(b, seeded, uint64(10_000-(existingPages+1)*pageSize), pageSize)

		var joined timeline.MessageFeed
		Measure(b, nil, func() {
			begun, beginErr := timeline.BeginOlderMessagePage(seeded)
			if beginErr != nil {
				b.Fatalf("begin older page: %v", beginErr)
			}
			result, joinErr := timeline.PrependOlderMessagePage(begun, older)
			if joinErr != nil {
				b.Fatalf("prepend older page: %v", joinErr)
			}
			joined = result
		})
		want := (existingPages + 2) * pageSize
		if len(joined.Messages) != want {
			b.Fatalf("paginated feed holds %d messages, want %d", len(joined.Messages), want)
		}
		b.ReportMetric(float64(len(joined.Messages)), "feed-messages")
	})
}

// BenchmarkSimultaneousTokenAndCostUpdates is M22-084.
//
// Streamed tokens and cost revisions arrive together during live work, and the
// interesting cost is the pair: a projection that is cheap for either alone
// but rebuilds shared state on both is only visible when they interleave.
func BenchmarkSimultaneousTokenAndCostUpdates(b *testing.B) {
	LogEnvironment(b)
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		b.Fatalf("parse currency: %v", err)
	}
	limit, err := domain.NewMoney(currency, 40_000)
	if err != nil {
		b.Fatalf("new limit: %v", err)
	}

	identities := newReplayIdentities(b)
	snapshot := replayBase(identities)
	tokens := replayStream(identities, b.N+1)

	view := taskcontrols.UsageView{
		Cost: taskcontrols.ActualCostView{UnknownReason: "provider price has not arrived"},
	}
	budget := taskcontrols.BudgetView{
		HardLimitKnown: true, HardLimit: limit,
		RemainingKnown: true, Remaining: limit,
	}

	b.ReportAllocs()
	b.ResetTimer()
	started := time.Now()
	for index := range b.N {
		// One streamed token...
		next, reduceErr := events.ReduceTaskEvents(snapshot, tokens[index:index+1])
		if reduceErr != nil {
			b.Fatalf("apply token %d: %v", index, reduceErr)
		}
		snapshot = next

		// ...and the cost revision that lands beside it.
		spent, moneyErr := domain.NewMoney(currency, int64(index))
		if moneyErr != nil {
			b.Fatalf("new spend: %v", moneyErr)
		}
		// Money exposes exact addition only, so spend is applied as a
		// negative amount rather than through float arithmetic.
		debit, debitErr := domain.NewMoney(currency, -spent.MinorUnits)
		if debitErr != nil {
			b.Fatalf("new debit: %v", debitErr)
		}
		remaining, addErr := limit.Add(debit)
		if addErr != nil {
			b.Fatalf("apply spend: %v", addErr)
		}
		budget.Remaining = remaining
		view.Cost = taskcontrols.ActualCostView{Known: true, Value: spent, PricingSnapshot: "benchmark-fixed-pricing"}
	}
	elapsed := time.Since(started)
	b.StopTimer()

	if snapshot.ThroughSequence != uint64(b.N) {
		b.Fatalf("applied %d tokens but reached sequence %d", b.N, snapshot.ThroughSequence)
	}
	if budget.Remaining == limit && !view.Cost.Known {
		b.Fatal("cost updates did not take effect")
	}
	if elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "pairs/s")
	}
}
