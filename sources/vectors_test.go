package sources_test

import (
	"testing"

	"github.com/rootxkit/uspace-core/sources"
	"github.com/rootxkit/uspace-core/vectors"
)

type vecControl struct {
	SourceType string  `json:"source_type"`
	InstanceID *string `json:"instance_id"`
	Enabled    bool    `json:"enabled"`
}

type vecInput struct {
	Controls    []vecControl `json:"controls"`
	DefaultDeny bool         `json:"default_deny"`
	Query       struct {
		SourceType string  `json:"source_type"`
		InstanceID *string `json:"instance_id"`
	} `json:"query"`
}

type vecExpected struct {
	Enabled     bool    `json:"enabled"`
	WhyDisabled *string `json:"why_disabled"`
}

func whyString(d sources.Decision) *string {
	if d.WhyDisabled == nil {
		return nil
	}
	s := string(*d.WhyDisabled)
	return &s
}

// TestVectorsSourceControl runs each case on State.Query and on a Follower
// that has applied the same state.
func TestVectorsSourceControl(t *testing.T) {
	f := vectors.Load(t, "source_control.json")
	ran := 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vecInput
		var exp vecExpected
		c.Decode(t, &in, &exp)
		st := sources.State{DefaultDeny: in.DefaultDeny, Version: 1, Epoch: "vectors"}
		for _, rc := range in.Controls {
			st.Controls = append(st.Controls, sources.Control{SourceType: rc.SourceType, InstanceID: rc.InstanceID, Enabled: rc.Enabled})
		}
		fol := sources.NewFollower()
		if !fol.Apply(st) {
			t.Fatal("the first state was not applied")
		}
		for name, d := range map[string]sources.Decision{
			"state":    st.Query(in.Query.SourceType, in.Query.InstanceID),
			"follower": fol.Query(in.Query.SourceType, in.Query.InstanceID),
		} {
			if d.Enabled != exp.Enabled {
				t.Errorf("%s: enabled %v, want %v", name, d.Enabled, exp.Enabled)
			}
			vectors.EqualStrPtr(t, name+" why_disabled", whyString(d), exp.WhyDisabled)
		}
		ran++
	})
	if ran != 8 {
		t.Errorf("ran %d cases, want 8", ran)
	}
}
