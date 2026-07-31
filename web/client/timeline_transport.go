package main

import (
	"sort"

	"codeflux.dev/codeflux/web/frontend/threadrail"
	"codeflux.dev/codeflux/web/frontend/timeline"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
)

func authoritativeTimelineCards(
	thread threadrail.Thread,
	feed timeline.MessageFeed,
	stream timeline.State,
) []timelinecard.Card {
	byKey := make(map[string]timelinecard.Card, len(feed.Messages)+len(stream.Items))
	if feed.ThreadID == thread.ID() {
		for _, message := range feed.Messages {
			card := timelinecard.Card{
				Kind: timelinecard.KindMessage, Sequence: message.Sequence,
				StableKey: "message:" + message.ID.String(), OccurredAt: message.CreatedAt,
				Message: &timelinecard.Message{ID: message.ID.String(), Role: message.Role,
					Body: message.Body.Text, Status: timelinecard.MessageComplete, OccurredAt: message.CreatedAt},
			}
			byKey[card.StableKey] = card
		}
	}
	for _, item := range stream.Items {
		if len(item.Events) == 0 {
			continue
		}
		card, err := timelinecard.Project(item.Events[len(item.Events)-1])
		if err != nil {
			continue
		}
		if item.Message != nil && !item.Message.Final {
			card.Message.Body = item.Message.Body
			card.Message.Status = timelinecard.MessageProvisional
		}
		byKey[card.StableKey] = card
	}
	cards := make([]timelinecard.Card, 0, len(byKey))
	for _, card := range byKey {
		cards = append(cards, card)
	}
	sort.Slice(cards, func(left, right int) bool {
		if cards[left].Sequence == cards[right].Sequence {
			return cards[left].StableKey < cards[right].StableKey
		}
		return cards[left].Sequence < cards[right].Sequence
	})
	return cards
}
