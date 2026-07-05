package protocol

import "testing"

func TestRoomIDDeterministic(t *testing.T) {
	a := RoomID("final-project", "")
	b := RoomID("final-project", "")
	if a != b {
		t.Fatalf("RoomID not deterministic: %q != %q", a, b)
	}
	if RoomID("final-project", "secret") == a {
		t.Fatal("passphrase should change room_id")
	}
	if RoomID("other", "") == a {
		t.Fatal("different room name should change room_id")
	}
}

func TestNewPeerIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id, err := NewPeerID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("duplicate peer id %q", id)
		}
		seen[id] = true
	}
}

func TestNewMsgIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id, err := NewMsgID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("duplicate msg id %q", id)
		}
		seen[id] = true
	}
}
