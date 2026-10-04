package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchRetriesWithoutOverSpecificAlbum(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("artist_name") != "周杰伦" || r.URL.Query().Get("track_name") != "七里香" {
			t.Error("localized names lost")
		}
		if r.URL.Query().Get("album_name") != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"syncedLyrics":"[00:01.00]甲：第一句\n[00:01.00]乙：第二句"}`))
	}))
	defer server.Close()
	client := &Client{http: server.Client(), baseURL: server.URL}
	res, err := client.Fetch(context.Background(), "周杰伦", "七里香", "Deluxe version", time.Minute)
	if err != nil || !res.Synced || len(res.Lines) != 2 || calls != 2 {
		t.Fatalf("fallback failed: %v calls:%d", err, calls)
	}
}

func TestFetchBilingualDuetMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/get" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("track_name") != "对等关系" {
			t.Error("feature suffix not stripped")
		}
		if r.URL.Query().Get("artist_name") != "" {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`[{"trackName":"对等关系 - Equivalence Relation (feat. aMEI)","artistName":"Ronghao Li","duration":328,"syncedLyrics":"[00:01.00][00:03.00]甲：测试一\n[00:02.00]乙：测试二"}]`))
	}))
	defer server.Close()
	c := &Client{http: server.Client(), baseURL: server.URL}
	res, err := c.Fetch(context.Background(), "李荣浩", "对等关系 (feat. 张惠妹)", "纵横四海", 327*time.Second)
	if err != nil || !res.Synced || len(res.Lines) != 3 || res.Lines[2].Start != 3*time.Second || res.Lines[0].Speaker != "甲" {
		t.Fatalf("bilingual/multi-timestamp lookup failed: %+v %v", res, err)
	}
}
func TestSearchRejectsAmbiguousArtistAndWrongDuration(t *testing.T) {
	for _, data := range []string{
		`[{"trackName":"测试","artistName":"甲","duration":60,"plainLyrics":"示例"},{"trackName":"测试","artistName":"乙","duration":60,"plainLyrics":"示例"}]`,
		`[{"trackName":"测试","artistName":"甲","duration":120,"plainLyrics":"示例"}]`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/get" {
				w.WriteHeader(404)
				return
			}
			w.Write([]byte(data))
		}))
		c := &Client{http: server.Client(), baseURL: server.URL}
		res, err := c.Fetch(context.Background(), "歌手", "测试", "", time.Minute)
		server.Close()
		if err == nil || res != nil {
			t.Fatal("unrelated recording selected")
		}
	}
}
