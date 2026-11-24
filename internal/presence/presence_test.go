package presence

import (
	"reflect"
	"testing"
)

func TestMemoryTracker(t *testing.T) {
	tracker := NewMemoryTracker()
	defer tracker.Close()

	_ = tracker.SetGlobalOnline("alice")
	_ = tracker.SetGlobalOnline("alice") // second tab
	_ = tracker.SetGlobalOnline("bob")

	global, _ := tracker.GlobalOnline()
	if !reflect.DeepEqual(global, []string{"alice", "bob"}) {
		t.Fatalf("unexpected global online: %v", global)
	}

	_ = tracker.SetGlobalOffline("alice") // first tab closes; still online
	global, _ = tracker.GlobalOnline()
	if !reflect.DeepEqual(global, []string{"alice", "bob"}) {
		t.Fatalf("expected alice to remain online, got: %v", global)
	}

	_ = tracker.SetGlobalOffline("alice") // second tab closes
	global, _ = tracker.GlobalOnline()
	if !reflect.DeepEqual(global, []string{"bob"}) {
		t.Fatalf("expected only bob online, got: %v", global)
	}

	_ = tracker.SetOnline(1, "bob")
	_ = tracker.SetOnline(1, "charlie")
	inRoom, _ := tracker.OnlineInRoom(1)
	if !reflect.DeepEqual(inRoom, []string{"bob", "charlie"}) {
		t.Fatalf("unexpected room online: %v", inRoom)
	}

	_ = tracker.SetOffline(1, "bob")
	inRoom, _ = tracker.OnlineInRoom(1)
	if !reflect.DeepEqual(inRoom, []string{"charlie"}) {
		t.Fatalf("unexpected room online after bob left: %v", inRoom)
	}
}

