package apple

import (
	"context"
	"net/http"
	"testing"

	"github.com/simone-vibes/vibez/internal/config"
)

func TestMetadataLanguageKeepsStorefrontAndPagination(t *testing.T) {
	p := New(&config.Config{AppleLanguage: "zh-Hans-CN"})
	for _, makeRequest := range []func(context.Context, string, string) (*http.Request, error){p.newRequest, p.newCatalogRequest} {
		req, err := makeRequest(context.Background(), http.MethodGet, "/catalog/us/songs?ids=1721450037&offset=25")
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.Query().Get("l") != "zh-Hans-CN" || req.URL.Query().Get("ids") != "1721450037" || req.URL.Query().Get("offset") != "25" {
			t.Fatalf("language or existing query lost: %s", req.URL)
		}
		if req.URL.Path != "/v1/catalog/us/songs" {
			t.Fatal("language changed storefront")
		}
	}
}
