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
	{ID: "d1", Title: "Nights", Artist: "Frank Ocean", Album: "Blonde", Duration: dur(5, 7), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music115/v4/bb/45/68/bb4568f3-68cd-619d-fbcb-4e179916545d/BlondCover-Final.jpg/600x600bb.jpg"},
	{ID: "d2", Title: "Pyramids", Artist: "Frank Ocean", Album: "channel ORANGE", Duration: dur(9, 2), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music115/v4/bb/45/68/bb4568f3-68cd-619d-fbcb-4e179916545d/BlondCover-Final.jpg/600x600bb.jpg"},
	{ID: "d3", Title: "Novacane", Artist: "Frank Ocean", Album: "nostalgia, ULTRA", Duration: dur(5, 7), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music124/v4/8d/76/23/8d76234b-5101-fa9b-58b3-5e17645d5b05/00602527744209.rgb.jpg/600x600bb.jpg"},
	{ID: "d4", Title: "Redbone", Artist: "Childish Gambino", Album: "Awaken, My Love!", Duration: dur(5, 27), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music211/v4/f1/3c/d7/f13cd7ab-7319-028a-8807-5991d0b308d4/0044003187658_Cover.jpg/600x600bb.jpg"},
	{ID: "d5", Title: "Me and Your Mama", Artist: "Childish Gambino", Album: "Awaken, My Love!", Duration: dur(4, 40), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music211/v4/f1/3c/d7/f13cd7ab-7319-028a-8807-5991d0b308d4/0044003187658_Cover.jpg/600x600bb.jpg"},
	{ID: "d6", Title: "See You Again", Artist: "Tyler, The Creator", Album: "Flower Boy", Duration: dur(3, 1), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/fd/fd/8c/fdfd8c26-b8f9-4768-41d3-b24773250c65/886446605814.jpg/600x600bb.jpg"},
	{ID: "d7", Title: "Garden Shed", Artist: "Tyler, The Creator", Album: "Flower Boy", Duration: dur(3, 32), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music125/v4/fd/fd/8c/fdfd8c26-b8f9-4768-41d3-b24773250c65/886446605814.jpg/600x600bb.jpg"},
	{ID: "d8", Title: "Kill Bill", Artist: "SZA", Album: "SOS", Duration: dur(2, 33), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music122/v4/62/93/13/6293132e-20ff-67ab-3d1f-96bb6797a6ba/196589564955.jpg/600x600bb.jpg"},
	{ID: "d9", Title: "Good Days", Artist: "SZA", Album: "Good Days", Duration: dur(4, 39), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music115/v4/0e/5c/6e/0e5c6e76-928a-bb8c-ec7d-b323f6079f67/886449007394.jpg/600x600bb.jpg"},
	{ID: "d10", Title: "After The Storm", Artist: "Kali Uchis", Album: "Isolation", Duration: dur(3, 57), ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music126/v4/de/e1/8b/dee18be4-276a-1e90-7aec-883c91a5a43e/17UM1IM08185.rgb.jpg/600x600bb.jpg"},
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

func (Provider) GetStationTracks(_ context.Context, seedCatalogID string) ([]provider.Track, error) {
	out := make([]provider.Track, 0, len(Tracks))
	for _, t := range Tracks {
		if t.ID == seedCatalogID || t.CatalogID == seedCatalogID {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

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
