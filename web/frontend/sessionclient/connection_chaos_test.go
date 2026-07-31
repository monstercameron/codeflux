package sessionclient

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGeneratedDurableEventCategoriesReconnectBeforeDuringAndAfterDelivery(t *testing.T) {
	identity := testSessionIdentity()
	for _, descriptor := range events.Registry {
		kindName := "SESSION_EVENT_KIND_" + strings.ToUpper(strings.ReplaceAll(
			strings.TrimPrefix(descriptor.Name, "session."), ".", "_",
		))
		kindValue, ok := codefluxv1.SessionEventKind_value[kindName]
		if !ok {
			t.Fatalf("generated event %q has no protobuf kind %q", descriptor.Name, kindName)
		}
		for _, phase := range []string{"before", "during", "after"} {
			t.Run(descriptor.Name+"/"+phase, func(t *testing.T) {
				event := testResponse(identity, 1)
				event.Event.Kind = codefluxv1.SessionEventKind(kindValue)
				firstItems, secondItems, wantAfter := chaosStreamsForPhase(phase, event)
				connector := &fakeConnector{connections: []Connection{
					&fakeConnection{stream: &scriptedStream{items: firstItems}},
					&fakeConnection{stream: &scriptedStream{items: secondItems}},
				}}
				var applied []codefluxv1.SessionEventKind
				client := newTestClient(t, Config{
					Connector: connector, SessionID: identity,
					Retry: RetryPolicy{MaxAttempts: 3, InitialDelay: time.Millisecond},
					Apply: func(_ context.Context, value *codefluxv1.SessionEvent) error {
						applied = append(applied, value.GetKind())
						return nil
					},
					WaitForRetry: noRetryWait,
				})
				if err := client.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := client.Wait(); status.Code(err) != codes.PermissionDenied {
					t.Fatalf("terminal error = %v", err)
				}
				if !reflect.DeepEqual(applied, []codefluxv1.SessionEventKind{codefluxv1.SessionEventKind(kindValue)}) ||
					!reflect.DeepEqual(connector.afterSequences(), wantAfter) ||
					client.Status().LastSequence != 1 {
					t.Fatalf("applied=%v after=%v status=%+v", applied, connector.afterSequences(), client.Status())
				}
			})
		}
	}
}

func chaosStreamsForPhase(
	phase string,
	event *codefluxv1.SubscribeSessionResponse,
) ([]streamItem, []streamItem, []uint64) {
	terminal := streamItem{err: status.Error(codes.PermissionDenied, "chaos terminal")}
	switch phase {
	case "before":
		return []streamItem{{err: io.ErrUnexpectedEOF}},
			[]streamItem{{response: event}, {response: testReplayBoundary(1)}, terminal}, []uint64{0, 0}
	case "during":
		return []streamItem{{response: event}, {err: io.ErrUnexpectedEOF}},
			[]streamItem{{response: event}, {response: testReplayBoundary(1)}, terminal}, []uint64{0, 1}
	case "after":
		return []streamItem{{response: event}, {response: testReplayBoundary(1)}, {err: io.EOF}},
			[]streamItem{{response: testReplayBoundary(1)}, terminal}, []uint64{0, 1}
	default:
		panic("unknown chaos phase")
	}
}
