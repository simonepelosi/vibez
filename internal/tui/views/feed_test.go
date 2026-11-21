package views

import (
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider"
)

func TestFeedModel_RendersStationsWithoutExtraGapOnEmptySubtitle(t *testing.T) {
	f := NewFeed()
	f.SetSize(80, 12)
	f.SetRecommendations([]provider.RecommendationGroup{
		{
			Title: "Stations for You",
			Items: []provider.RecommendationItem{
				{ID: "ra.u-1", Kind: "station", Title: "Simone's Station", Subtitle: "Apple Music"},
				{ID: "ra.disc", Kind: "station", Title: "Discovery Station"},
			},
		},
	})

	view := f.View()
	if !strings.Contains(view, "Stations for You") {
		t.Fatalf("Feed View missing group header:\n%s", view)
	}
	if !strings.Contains(view, "[station]") || !strings.Contains(view, "Discovery Station") {
		t.Fatalf("Feed View missing station items:\n%s", view)
	}
	if sel := f.SelectedItem(); sel == nil || sel.ID != "ra.u-1" || sel.Kind != "station" {
		t.Fatalf("SelectedItem = %+v, want ra.u-1 station", sel)
	}
}
