package platform_test

import (
	"encoding/json"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

func msg(id int64) platform.Message {
	return platform.Message{ID: id, Event: "history", Data: json.RawMessage(`{}`)}
}

func TestHubDeliversToTheUsersStreamsOnly(t *testing.T) {
	h := platform.NewHub()
	a1, a2, b := h.Subscribe(1), h.Subscribe(1), h.Subscribe(2)
	h.Send(1, msg(7))
	for _, s := range []*platform.Subscription{a1, a2} {
		if m := <-s.Messages(); m.ID != 7 {
			t.Fatalf("got %+v", m)
		}
	}
	select {
	case m := <-b.Messages():
		t.Fatalf("user 2 got user 1's message %+v", m)
	default:
	}
}

func TestHubDropsAStreamThatFellBehind(t *testing.T) {
	h := platform.NewHub()
	slow, ok := h.Subscribe(1), h.Subscribe(1)
	for i := range platform.SubscriberBuffer + 1 {
		h.Send(1, msg(int64(i)))
		<-ok.Messages()
	}
	n := 0
	for range slow.Messages() { // closed after the buffered ones
		n++
	}
	if n != platform.SubscriberBuffer {
		t.Fatalf("slow stream got %d messages before closing, want %d", n, platform.SubscriberBuffer)
	}
	h.Send(1, msg(99))
	if m := <-ok.Messages(); m.ID != 99 {
		t.Fatalf("the stream that kept up was dropped too: %+v", m)
	}
}

func TestClosingTheHubEndsEveryStream(t *testing.T) {
	h := platform.NewHub()
	s := h.Subscribe(1)
	s2 := h.Subscribe(1)
	s2.Close()
	s2.Close() // twice is fine
	h.Close()
	if _, open := <-s.Messages(); open {
		t.Fatal("stream still open after Close")
	}
	if _, open := <-h.Subscribe(1).Messages(); open {
		t.Fatal("a closed hub accepted a subscriber")
	}
	h.Send(1, msg(1)) // no subscribers, no panic
}

func TestCryptoRandomPermIsAPermutation(t *testing.T) {
	for _, n := range []int{0, 1, 7} {
		p := platform.CryptoRandom{}.Perm(n)
		seen := map[int]bool{}
		for _, v := range p {
			if v < 0 || v >= n || seen[v] {
				t.Fatalf("Perm(%d) = %v", n, p)
			}
			seen[v] = true
		}
		if len(p) != n {
			t.Fatalf("Perm(%d) = %v", n, p)
		}
	}
}
