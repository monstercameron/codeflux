package main

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"codeflux.dev/codeflux/web/frontend/timeline"
)

func TestAuthoritativeTimelineCardsJoinPageAndSessionReplayByMessageIdentity(t *testing.T) {
	repositoryID, _ := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ab")
	workspaceID, _ := domain.ParseWorkspaceID("wsp_01890f3c-4a00-7abc-8def-0123456789ab")
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	sessionID, _ := domain.ParseSessionID("ses_01890f3c-4a00-7abc-8def-0123456789ab")
	messageID, _ := domain.ParseMessageID("msg_01890f3c-4a00-7abc-8def-0123456789ab")
	thread, err := threadrail.NewThread(threadrail.ThreadInput{ID: threadID, SessionID: sessionID, RepositoryID: repositoryID, WorkspaceID: workspaceID, Title: "Thread", TaskState: threadrail.TaskStateNone, Attention: threadrail.AttentionNone, Revision: 1, UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	feed := timeline.MessageFeed{ThreadID: threadID, Messages: []timeline.DurableMessage{{ID: messageID, ThreadID: threadID, Role: "user", Body: timeline.RedactedBody{Text: "page"}, Sequence: 1, Revision: 1, CreatedAt: time.Unix(1, 0).UTC()}}}
	event, err := (events.NewSessionEvent{SessionID: sessionID, ThreadID: threadID, Kind: events.KindMessageFinal, PayloadVersion: 1, Payload: events.Payload{MessageFinal: &events.MessageFinal{MessageID: messageID, Role: "user", RedactedBody: "replayed"}}}).Build(2, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := timeline.MergeThreadPage(timeline.State{}, timeline.Page{Events: []events.SessionEvent{event}})
	if err != nil {
		t.Fatal(err)
	}
	cards := authoritativeTimelineCards(thread, feed, stream)
	if len(cards) != 1 || cards[0].Message == nil || cards[0].Message.Body != "replayed" || cards[0].StableKey != "message:"+messageID.String() {
		t.Fatalf("authoritative cards = %#v", cards)
	}
}
