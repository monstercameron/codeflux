//go:build js && wasm

package main

import (
	"context"

	"codeflux.dev/codeflux/web/frontend/timeline"
)

type timelinePageLease struct {
	client timeline.PageClient
	close  func() error
}

func fetchNewestTimelinePage(ctx context.Context, lease timelinePageLease) (timeline.MessageFeed, error) {
	if lease.close == nil {
		return timeline.MessageFeed{}, timeline.ErrInvalidThreadPage
	}
	defer lease.close()
	page, err := lease.client.FetchNewest(ctx)
	if err != nil {
		return timeline.MessageFeed{}, err
	}
	return timeline.ApplyNewestMessagePage(page)
}

func fetchOlderTimelinePage(
	ctx context.Context,
	feed timeline.MessageFeed,
	lease timelinePageLease,
) (timeline.MessageFeed, error) {
	if lease.close == nil {
		return feed, timeline.ErrInvalidThreadPage
	}
	defer lease.close()
	loading, err := timeline.BeginOlderMessagePage(feed)
	if err != nil {
		return feed, err
	}
	page, err := lease.client.FetchOlder(ctx, loading.OlderCursor)
	if err != nil {
		return timeline.FailOlderMessagePage(loading), err
	}
	return timeline.PrependOlderMessagePage(loading, page)
}
