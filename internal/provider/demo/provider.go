// Package demo provides an in-memory Provider that returns realistic fake
// data. No Apple credentials or network access are required.
package demo

import (
	"context"
	"strings"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

// Tracks is the built-in demo library shared with the demo Player.
var Tracks = []provider.Track{
	{ID: "d1", Title: "Nights", Artist: "Frank Ocean", Album: "Blonde", Duration: dur(5, 7), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music115/v4/92/03/16/920316ad-387d-3912-2a38-9def9f8c6bf0/18UMGIM18102.rgb.jpg/300x300bb.jpg"},
	{ID: "d2", Title: "Pyramids", Artist: "Frank Ocean", Album: "channel ORANGE", Duration: dur(9, 2), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/21/12/c5/2112c5dd-7cbc-077a-c86f-19821679fa57/12UMGIM40298.rgb.jpg/300x300bb.jpg"},
	{ID: "d3", Title: "Novacane", Artist: "Frank Ocean", Album: "nostalgia, ULTRA", Duration: dur(5, 7), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/dc/cb/ec/dccbec6b-3e33-7a93-adbf-bf81e4ee42e0/11UMGIM17475.rgb.jpg/300x300bb.jpg"},
	{ID: "d4", Title: "Redbone", Artist: "Childish Gambino", Album: "Awaken, My Love!", Duration: dur(5, 27), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music122/v4/e1/f0/c9/e1f0c9e4-dbe6-4982-f4cc-353a405cd06e/16UMGIM77860.rgb.jpg/300x300bb.jpg"},
	{ID: "d5", Title: "Me and Your Mama", Artist: "Childish Gambino", Album: "Awaken, My Love!", Duration: dur(4, 40), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music122/v4/e1/f0/c9/e1f0c9e4-dbe6-4982-f4cc-353a405cd06e/16UMGIM77860.rgb.jpg/300x300bb.jpg"},
	{ID: "d6", Title: "See You Again", Artist: "Tyler, The Creator", Album: "Flower Boy", Duration: dur(3, 1), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/5a/cf/1d/5acf1dbd-9d9c-8e77-06ac-c563f7ef629d/886446632373.jpg/300x300bb.jpg"},
	{ID: "d7", Title: "Garden Shed", Artist: "Tyler, The Creator", Album: "Flower Boy", Duration: dur(3, 32), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/5a/cf/1d/5acf1dbd-9d9c-8e77-06ac-c563f7ef629d/886446632373.jpg/300x300bb.jpg"},
	{ID: "d8", Title: "Kill Bill", Artist: "SZA", Album: "SOS", Duration: dur(2, 33), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music112/v4/f9/e3/5d/f9e35d35-6c7c-a358-910d-17ff6394a38e/196589564931.jpg/300x300bb.jpg"},
	{ID: "d9", Title: "Good Days", Artist: "SZA", Album: "Good Days", Duration: dur(4, 39), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music115/v4/b1/c6/9a/b1c69a2f-d81e-1f76-dd28-5ec7146f9cd6/886448817637.jpg/300x300bb.jpg"},
	{ID: "d10", Title: "After The Storm", Artist: "Kali Uchis", Album: "Isolation", Duration: dur(3, 57), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/19/f8/9e/19f89ed7-0c2a-7c92-99ff-241146c0a771/17UMGIM98371.rgb.jpg/300x300bb.jpg"},
}

func dur(m, s int) time.Duration {
	return time.Duration(m)*time.Minute + time.Duration(s)*time.Second
}

// Provider is the demo Provider implementation.
type Provider struct{}

func (Provider) Name() string          { return "Demo" }
func (Provider) IsAuthenticated() bool { return true }

var demoPlaylists = []provider.Playlist{
	{ID: "dp1", Name: "Late Night Coding", TrackCount: 4},
	{ID: "dp2", Name: "Chill Session", TrackCount: 3},
	{ID: "dp3", Name: "Energy Boost", TrackCount: 3},
}

var playlistTracks = map[string][]provider.Track{
	"dp1": {Tracks[0], Tracks[5], Tracks[6], Tracks[7]},
	"dp2": {Tracks[3], Tracks[8], Tracks[9]},
	"dp3": {Tracks[1], Tracks[2], Tracks[4]},
}

func (Provider) Search(_ context.Context, query string) (*provider.SearchResult, error) {
	q := strings.ToLower(query)
	var tracks []provider.Track
	var albums []provider.Album
	seen := map[string]bool{}

	for _, t := range Tracks {
		if strings.Contains(strings.ToLower(t.Title), q) ||
			strings.Contains(strings.ToLower(t.Artist), q) ||
			strings.Contains(strings.ToLower(t.Album), q) {
			tracks = append(tracks, t)
			if !seen[t.Album] {
				seen[t.Album] = true
				albums = append(albums, provider.Album{
					ID:     "da-" + t.ID,
					Title:  t.Album,
					Artist: t.Artist,
				})
			}
		}
	}
	return &provider.SearchResult{Tracks: tracks, Albums: albums}, nil
}

func (Provider) GetLibraryTracks(_ context.Context) ([]provider.Track, error) {
	out := make([]provider.Track, len(Tracks))
	copy(out, Tracks)
	return out, nil
}

func (Provider) GetLibraryPlaylists(_ context.Context) ([]provider.Playlist, error) {
	out := make([]provider.Playlist, len(demoPlaylists))
	copy(out, demoPlaylists)
	return out, nil
}

func (Provider) GetPlaylistTracks(_ context.Context, id string) ([]provider.Track, error) {
	if tracks, ok := playlistTracks[id]; ok {
		out := make([]provider.Track, len(tracks))
		copy(out, tracks)
		return out, nil
	}
	return nil, nil
}

func (Provider) GetAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	out := make([]provider.Track, len(Tracks))
	copy(out, Tracks)
	return out, nil
}

func (Provider) GetLibraryAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	out := make([]provider.Track, len(Tracks))
	copy(out, Tracks)
	return out, nil
}

func (Provider) GetCatalogPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	out := make([]provider.Track, len(Tracks))
	copy(out, Tracks)
	return out, nil
}

func (Provider) CreatePlaylist(_ context.Context, name string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{ID: "dp-new-" + name, Name: name}, nil
}

func (Provider) LoveSong(_ context.Context, _ string, _ bool) error { return nil }

func (Provider) GetSongRating(_ context.Context, _ string) (bool, error) { return false, nil }

func (Provider) AddToPlaylist(_ context.Context, _, _ string) error { return nil }

func (Provider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	return []provider.RecommendationGroup{
		{
			Title: "Recommended for You",
			Items: []provider.RecommendationItem{
				{ID: "demo-album-1", Kind: "album", Title: "A Colours Trilogy", Subtitle: "Jon Hopkins"},
				{ID: "demo-album-2", Kind: "album", Title: "Immunity", Subtitle: "Jon Hopkins"},
			},
		},
		{
			Title: "New Releases",
			Items: []provider.RecommendationItem{
				{ID: "demo-pl-1", Kind: "playlist", Title: "New Music Mix", Subtitle: "Apple Music"},
			},
		},
	}, nil
}
