package cdp

import (
	"encoding/json"
	"fmt"
)

// buildSetQueueJS returns the JS expression that calls vibezSetQueue.
// The IDs are JSON-encoded and passed as a JSON string literal so the
// browser side can JSON.parse() them without a second round-trip.
func buildSetQueueJS(ids []string) (string, error) {
	b, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("cdp: marshal queue ids: %w", err)
	}
	js, err := json.Marshal(string(b))
	if err != nil {
		return "", fmt.Errorf("cdp: marshal queue json string: %w", err)
	}
	return fmt.Sprintf(`window.vibezSetQueue && window.vibezSetQueue(%s)`, js), nil
}

// buildSetPlaylistJS returns the JS expression that calls vibezSetPlaylist.
func buildSetPlaylistJS(playlistID string, startIdx int) (string, error) {
	js, err := json.Marshal(playlistID)
	if err != nil {
		return "", fmt.Errorf("cdp: marshal playlist id: %w", err)
	}
	return fmt.Sprintf(`window.vibezSetPlaylist && window.vibezSetPlaylist(%s,%d)`, js, startIdx), nil
}

// buildAppendQueueJS returns the JS expression that calls vibezAppendQueue.
func buildAppendQueueJS(ids []string) (string, error) {
	b, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("cdp: marshal append ids: %w", err)
	}
	js, err := json.Marshal(string(b))
	if err != nil {
		return "", fmt.Errorf("cdp: marshal append json string: %w", err)
	}
	return fmt.Sprintf(`window.vibezAppendQueue && window.vibezAppendQueue(%s)`, js), nil
}
