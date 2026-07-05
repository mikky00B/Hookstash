package stream

import (
	"testing"
	"time"
)

func TestBrokerPublishesToSubscribers(t *testing.T) {
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe()
	defer unsubscribe()

	want := Event{Type: "request.created", Data: []byte(`{"id":"req_test"}`)}
	broker.Publish(want)

	select {
	case got := <-events:
		if got.Type != want.Type || string(got.Data) != string(want.Data) {
			t.Fatalf("event = %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBrokerDropsEventsForSlowSubscribers(t *testing.T) {
	broker := NewBroker()
	_, unsubscribe := broker.Subscribe()
	defer unsubscribe()

	for i := 0; i < 100; i++ {
		broker.Publish(Event{Type: "request.created", Data: []byte(`{}`)})
	}
}
