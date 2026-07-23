package sentry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithEventHint(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	captureCtx := context.WithValue(context.Background(), contextKey{}, "capture")

	tests := []struct {
		name    string
		options []CaptureOption
		want    any
	}{
		{
			name:    "passes hint to capture pipeline",
			options: []CaptureOption{WithEventHint(&EventHint{Data: "hint"})},
			want:    "hint",
		},
		{
			name: "last hint wins",
			options: []CaptureOption{
				WithEventHint(&EventHint{Data: "first"}),
				WithEventHint(&EventHint{Data: "second"}),
			},
			want: "second",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got *EventHint
			client, err := newClient(ClientOptions{
				Transport: &MockTransport{},
				Integrations: func([]Integration) []Integration {
					return nil
				},
				BeforeSend: func(event *Event, hint *EventHint) *Event {
					got = hint
					return event
				},
			})
			require.NoError(t, err)

			id := client.CaptureMessage(captureCtx, "message", test.options...)
			require.NotNil(t, id)
			require.NotNil(t, got)
			assert.Equal(t, test.want, got.Data)
			assert.Equal(t, captureCtx, got.Context)
		})
	}
}

func TestWithEventModifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modifiers   []EventModifier
		wantID      bool
		wantMessage string
	}{
		{
			name: "applies modifiers in order",
			modifiers: []EventModifier{
				testEventModifier(func(event *Event, _ *EventHint, _ Client) *Event {
					event.Message += "-first"
					return event
				}),
				nil,
				testEventModifier(func(event *Event, _ *EventHint, _ Client) *Event {
					event.Message += "-second"
					return event
				}),
			},
			wantID:      true,
			wantMessage: "message-first-second",
		},
		{
			name: "nil event drops capture and stops the chain",
			modifiers: []EventModifier{
				testEventModifier(func(_ *Event, _ *EventHint, _ Client) *Event { return nil }),
				testEventModifier(func(event *Event, _ *EventHint, _ Client) *Event {
					event.Message = "not reached"
					return event
				}),
			},
			wantID: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transport := &MockTransport{}
			client, err := newClient(ClientOptions{
				Transport: transport,
				Integrations: func([]Integration) []Integration {
					return nil
				},
			})
			require.NoError(t, err)

			options := make([]CaptureOption, 0, len(test.modifiers))
			for _, modifier := range test.modifiers {
				options = append(options, WithEventModifier(modifier))
			}
			id := client.CaptureMessage(context.Background(), "message", options...)
			if !test.wantID {
				assert.Nil(t, id)
				assert.Nil(t, transport.lastEvent)
				return
			}
			require.NotNil(t, id)
			require.NotNil(t, transport.lastEvent)
			assert.Equal(t, test.wantMessage, transport.lastEvent.Message)
		})
	}
}

type testEventModifier func(*Event, *EventHint, Client) *Event

func (f testEventModifier) ApplyToEvent(event *Event, hint *EventHint, client Client) *Event {
	return f(event, hint, client)
}
