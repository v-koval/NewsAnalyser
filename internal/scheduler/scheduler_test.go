package scheduler

import "testing"

func TestTriggerReportsFullQueue(t *testing.T) {
	s := New(nil, nil, "")
	for i := 0; i < 32; i++ {
		if !s.Trigger("some-id") {
			t.Fatalf("trigger %d rejected before the queue is full", i)
		}
	}
	if s.Trigger("some-id") {
		t.Fatal("trigger accepted on a full queue")
	}
}
