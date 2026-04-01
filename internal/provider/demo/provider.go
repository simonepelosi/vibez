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
	{ID: "d1", Title: "Nights", Artist: "Frank Ocean", Album: "Blonde", Duration: dur(5, 7)},
	{ID: "d2", Title: "Pyramids", Artist: "Frank Ocean", Album: "channel ORANGE", Duration: dur(9, 2)},
	{ID: "d3", Title: "Novacane", Artist: "Frank Ocean", Album: "nostalgia, ULTRA", Duration: dur(5, 7)},
	{ID: "d4", Title: "Redbone", Artist: "Childish Gambino", Album: "Awaken, My Love!", Duration: dur(5, 27)},
	{ID: "d5", Title: "Me and Your Mama", Artist: "Childish Gambino", Album: "Awaken, My Love!", Duration: dur(4, 40)},
	{ID: "d6", Title: "See You Again", Artist: "Tyler, The Creator", Album: "Flower Boy", Duration: dur(3, 1)},
	{ID: "d7", Title: "Garden Shed", Artist: "Tyler, The Creator", Album: "Flower Boy", Duration: dur(3, 32)},
	{ID: "d8", Title: "Kill Bill", Artist: "SZA", Album: "SOS", Duration: dur(2, 33)},
	{ID: "d9", Title: "Good Days", Artist: "SZA", Album: "Good Days", Duration: dur(4, 39)},
	{ID: "d10", Title: "After The Storm", Artist: "Kali Uchis", Album: "Isolation", Duration: dur(3, 57)},
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

func (Provider) CreatePlaylist(_ context.Context, name string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{ID: "dp-new-" + name, Name: name}, nil
}

func (Provider) LoveSong(_ context.Context, _ string, _ bool) error { return nil }

func (Provider) GetSongRating(_ context.Context, _ string) (bool, error) { return false, nil }
