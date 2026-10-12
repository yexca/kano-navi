package workflow

import "testing"

func TestSchedulerIsDisabledByDefault(t *testing.T) {
	s := &Scheduler{}
	s.Start()
	status := s.Status()
	if status["running"] != false || status["enabled"] != false || status["tickSeconds"] != 30 {
		t.Fatalf("unexpected default scheduler status: %#v", status)
	}
	s.Stop()
}
