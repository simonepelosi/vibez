package provider

import (
	"context"
	"time"
)

// ArtworkPalette holds the dominant background and text colors extracted by
// Apple Music for a track's artwork, formatted as "#rrggbb" hex strings.
type ArtworkPalette struct {
	BgColor    string
	TextColor1 string
	TextColor2 string
	TextColor3 string
	TextColor4 string
}

// HasColors reports whether the palette contains at least one usable accent color.
func (p ArtworkPalette) HasColors() bool {
	return p.TextColor1 != "" || p.TextColor2 != "" || p.TextColor3 != "" || p.TextColor4 != ""
}

type Track struct {
	ID         string
	CatalogID  string // catalog ID for playback; set for library tracks where ID is "i.XXXXX"
	Title      string
	Artist     string
	Album      string
	Duration   time.Duration
	ArtworkURL string
	PreviewURL string
	Genres     []string
	// DiscNumber and TrackNumber give the track's position on its album.
	// Zero means unknown.
	DiscNumber  int
	TrackNumber int
	// Extended catalog metadata populated when available from Apple Music.
	ContentRating        string         // "explicit", "clean", or ""
	IsAppleDigitalMaster bool           // true when mastered as an Apple Digital Master
	ReleaseDate          string         // ISO release date ("YYYY-MM-DD" or "YYYY")
	ArtworkPalette       ArtworkPalette // per-artwork color palette from Apple Music
	// AudioVariants lists the formats Apple Music holds for the song, such as
	// "lossless", "hi-res-lossless" or "dolby-atmos". It describes the
	// catalog, not what vibez outputs: streaming is always AAC.
	AudioVariants []string
	// ISRC is the International Standard Recording Code (12 uppercase ASCII
	// alphanumeric characters, hyphens stripped), when available from the
	// catalog or embedded file tags.
	ISRC string
	// LocalPath is the filesystem path of a local audio file matched for hybrid
	// playback (or scanned in local mode). When set on an Apple Music track,
	// playback reads the local lossless file while keeping the Apple Music ID
	// and metadata intact.
	LocalPath string
}

// NormalizeISRC strips hyphens and whitespace from an ISRC and returns the
// uppercase 12-character alphanumeric code, or "" if s does not look like a
// valid ISRC.
func NormalizeISRC(s string) string {
	var buf [12]byte
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '-' || c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == 0:
			continue
		case c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
		default:
			return ""
		}
		if n >= len(buf) {
			return ""
		}
		buf[n] = c
		n++
	}
	if n != len(buf) {
		return ""
	}
	return string(buf[:])
}

// ReleaseYear returns the 4-digit release year from ReleaseDate, or "" if unavailable.
func (t Track) ReleaseYear() string {
	if len(t.ReleaseDate) < 4 {
		return ""
	}
	year := t.ReleaseDate[:4]
	for i := range 4 {
		if year[i] < '0' || year[i] > '9' {
			return ""
		}
	}
	return year
}

// RecommendationItem is a single album or playlist entry inside a recommendation group.
type RecommendationItem struct {
	ID       string
	Kind     string // "album" or "playlist"
	Title    string
	Subtitle string // artist name for albums; curator name for playlists
}

// RecommendationGroup is a titled set of recommended albums or playlists.
type RecommendationGroup struct {
	Title string
	Items []RecommendationItem
}

type Album struct {
	ID         string
	CatalogID  string // non-empty for library albums (l. prefix): catalog ID to fetch full track list
	Title      string
	Artist     string
	ArtworkURL string
	TrackCount int
	// ReleaseDate is "YYYY-MM-DD", or "YYYY" when Apple knows only the year.
	// Empty means unknown.
	ReleaseDate string
}

type Artist struct {
	ID   string
	Name string
}

type Playlist struct {
	ID         string
	Name       string
	TrackCount int
	ArtworkURL string
	// ReadOnly playlists are browsable system playlists but must not appear in add-to-playlist pickers.
	ReadOnly bool
}

type SearchResult struct {
	Tracks    []Track
	Albums    []Album
	Artists   []Artist
	Playlists []Playlist

	// Warnings carries non-fatal failures from providers that query several
	// backends in parallel and return whatever succeeded. Without it a caller
	// cannot distinguish "nothing matched the query" from "one backend is
	// down and these results are incomplete".
	Warnings []string
}

// Rating is the user's rating for a song. Apple Music stores love and
// dislike ("Suggest Less") as a single value, so a song is in exactly one of
// these states at a time. The numeric values match the Apple Music API.
type Rating int

const (
	RatingNone     Rating = 0
	RatingLoved    Rating = 1
	RatingDisliked Rating = -1
)

type Provider interface {
	Name() string
	Search(ctx context.Context, query string) (*SearchResult, error)
	GetLibraryTracks(ctx context.Context) ([]Track, error)
	GetLibraryPlaylists(ctx context.Context) ([]Playlist, error)
	GetPlaylistTracks(ctx context.Context, playlistID string) ([]Track, error)
	GetAlbumTracks(ctx context.Context, albumID string) ([]Track, error)
	// GetLibraryAlbumTracks fetches tracks for an album in the user's library.
	// Use this when the album ID starts with "l." (library album); catalog album
	// IDs (numeric) should use GetAlbumTracks instead.
	GetLibraryAlbumTracks(ctx context.Context, albumID string) ([]Track, error)
	// GetArtistAlbums returns every catalog album by the artist, singles and
	// EPs included, newest first.
	GetArtistAlbums(ctx context.Context, artistID string) ([]Album, error)
	// GetCatalogPlaylistTracks fetches tracks for a catalog playlist (e.g. from
	// the recommendations feed). Unlike GetPlaylistTracks, this targets the
	// catalog endpoint and does not require a library playlist ID.
	GetCatalogPlaylistTracks(ctx context.Context, playlistID string) ([]Track, error)
	// CreatePlaylist creates a new playlist in the user's library with the given
	// name and the supplied track IDs (library or catalog IDs). It returns the
	// newly created Playlist on success.
	CreatePlaylist(ctx context.Context, name string, trackIDs []string) (Playlist, error)
	// SetSongRating sets the user's rating for a song. RatingLoved adds the
	// song to the user's library and marks it as "Loved"; RatingDisliked marks
	// it as "Suggest Less"; RatingNone clears any rating. Love and dislike share
	// a single rating, so setting one replaces the other.
	SetSongRating(ctx context.Context, catalogID string, rating Rating) error
	// GetSongRating returns the user's rating for the given song. Returns
	// RatingNone (no error) when the song is not rated or the provider does
	// not support ratings.
	GetSongRating(ctx context.Context, catalogID string) (Rating, error)
	// GetRecommendations returns personalised recommendation groups (albums and
	// playlists) from Apple Music based on the user's library and history.
	GetRecommendations(ctx context.Context) ([]RecommendationGroup, error)
	// GetStationTracks fetches a batch of tracks for an Apple Music radio
	// station seeded by the given song. The seed may be a catalog song ID or a
	// library ID ("i.XXXX"); a library ID is resolved to its catalog ID first,
	// since a station can only be seeded by a catalog ID.
	GetStationTracks(ctx context.Context, seed string) ([]Track, error)
	// AddToPlaylist adds a track to an existing library playlist.
	// playlistID must be a library playlist ID (e.g. "p.XXXXX").
	// trackID should be a catalog song ID or a library song ID ("i.XXXXX").
	AddToPlaylist(ctx context.Context, playlistID, trackID string) error
	IsAuthenticated() bool
}
