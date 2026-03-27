package core

import "testing"

func TestEpistemicTypeConstants(t *testing.T) {
	if EpistemicAssertion != "assertion" {
		t.Errorf("EpistemicAssertion = %q, want %q", EpistemicAssertion, "assertion")
	}
	if EpistemicObservation != "observation" {
		t.Errorf("EpistemicObservation = %q, want %q", EpistemicObservation, "observation")
	}
	if EpistemicInference != "inference" {
		t.Errorf("EpistemicInference = %q, want %q", EpistemicInference, "inference")
	}

	n := Node{EpistemicType: EpistemicAssertion}
	if n.EpistemicType != "assertion" {
		t.Errorf("Node.EpistemicType = %q, want %q", n.EpistemicType, "assertion")
	}

	n.EpistemicType = EpistemicObservation
	if n.EpistemicType != "observation" {
		t.Errorf("Node.EpistemicType = %q after update, want %q", n.EpistemicType, "observation")
	}
}
