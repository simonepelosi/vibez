package lyrics

import (
	"regexp"
	"strings"
)

var speakerLabel = regexp.MustCompile(`^(?:【([^】]{1,24})】|\[([^\]]{1,24})\]|([^:：]{1,24})[:：])\s*(.*)$`)

func splitSpeaker(text string) (string, string) {
	match := speakerLabel.FindStringSubmatch(text)
	if match == nil {
		return "", text
	}
	label := ""
	for _, candidate := range match[1:4] {
		if candidate != "" {
			label = strings.TrimSpace(candidate)
			break
		}
	}
	switch label {
	case "作词", "作曲", "编曲", "词", "曲", "制作人", "Lyrics", "Composer":
		return "", text
	}
	return label, match[4]
}

// IsChorus identifies explicit shared vocal labels; unlabeled lyrics remain single voice.
func IsChorus(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "合", "合唱", "齐唱", "男女合", "all", "both", "chorus":
		return true
	}
	return false
}
