package tui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestChineseInterfaceAndIndependentEnglishModel(t *testing.T) {
	cfg := testCfg()
	cfg.UILanguage = "zh-CN"
	cfg.InlineLyrics = true
	m := New(cfg, &mockProvider{}, nil, Options{})
	m.width, m.height = 140, 45
	m.introStep = introDone
	text := ansi.Strip(m.View().Content)
	for _, label := range []string{"播放队列", "队列为空", "音乐氛围", "描述你想听的氛围", "等待播放"} {
		if !strings.Contains(text, label) {
			t.Errorf("missing %s", label)
		}
	}
	for _, label := range []string{"Queue", "Vibe", "describe your vibe", "silence is not"} {
		if strings.Contains(text, label) {
			t.Errorf("untranslated %s", label)
		}
	}
	m.mode = modeCommand
	m.cmdBuf = "save 夜晚"
	if !strings.Contains(ansi.Strip(m.View().Content), "save 夜晚") {
		t.Fatal("command token or text translated")
	}
	english := newModel(nil)
	english.width, english.height = 140, 45
	english.introStep = introDone
	if !strings.Contains(ansi.Strip(english.View().Content), "Queue") {
		t.Fatal("language leaked to other model")
	}
}
