package supervisor

import "testing"

type fakeControlPeer struct {
	messages []ControlMessage
}

func (f *fakeControlPeer) Send(message ControlMessage) error {
	f.messages = append(f.messages, message)
	return nil
}

func TestSwitchCoordinatorPromotesStandbyAfterShutdownOldWorker(t *testing.T) {
	coordinator := NewSwitchCoordinator()
	oldWorker := &fakeControlPeer{}
	newWorker := &fakeControlPeer{}

	coordinator.SetActive("old", oldWorker)

	if err := coordinator.BeginPromotion("new", newWorker); err != nil {
		t.Fatalf("expected begin promotion to succeed, got %v", err)
	}
	if len(oldWorker.messages) != 1 || oldWorker.messages[0].Type != MessageTypeShutdown {
		t.Fatalf("expected old worker to receive shutdown first, got %#v", oldWorker.messages)
	}
	if len(newWorker.messages) != 0 {
		t.Fatalf("expected standby worker to stay idle before finalize, got %#v", newWorker.messages)
	}

	if err := coordinator.CompletePromotion(); err != nil {
		t.Fatalf("expected complete promotion to succeed, got %v", err)
	}
	if len(newWorker.messages) != 1 || newWorker.messages[0].Type != MessageTypeActivate {
		t.Fatalf("expected standby worker to receive activate after finalize, got %#v", newWorker.messages)
	}
	if coordinator.ActiveWorkerID() != "new" {
		t.Fatalf("expected active worker to be new, got %q", coordinator.ActiveWorkerID())
	}
}
