package provider

import "testing"

func TestCheckOptions(t *testing.T) {
	specs := []OptionSpec{{Name: "category_id", Kind: "string"}, {Name: "share_to_feed", Kind: "bool"}}
	if err := CheckOptions(specs, map[string]any{"category_id": "22", "share_to_feed": false}); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
	if err := CheckOptions(specs, nil); err != nil {
		t.Fatalf("nil options rejected: %v", err)
	}
	for _, bad := range []map[string]any{{"published": true}, {"category_id": 22}, {"share_to_feed": "true"}} {
		if err := CheckOptions(specs, bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestPhaseAtLeast(t *testing.T) {
	if !PhaseAtLeast(PhaseProcessing, PhaseCommitting) || PhaseAtLeast(PhaseTransferring, PhaseCommitting) || PhaseAtLeast("", PhaseTransferring) {
		t.Fatal("phase ordering wrong")
	}
}
