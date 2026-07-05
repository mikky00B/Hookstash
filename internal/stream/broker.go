package stream

import "sync"

type Event struct {
	Type string
	Data []byte
}

type Broker struct {
	mu          sync.Mutex
	subscribers map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[chan Event]struct{}),
	}
}

func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 8)

	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		if _, ok := b.subscribers[ch]; ok {
			delete(b.subscribers, ch)
			close(ch)
		}
		b.mu.Unlock()
	}

	return ch, unsubscribe
}

func (b *Broker) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
