package cmd

import (
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/discord"
	playerpkg "github.com/simone-vibes/vibez/internal/player"
)

func startDiscordRPC(cfg *config.Config, player interface {
	Subscribe() <-chan playerpkg.State
}, log func(string)) func() {
	if noDiscord || !cfg.DiscordRPCEnabled() {
		return func() {}
	}
	clientID := cfg.DiscordClientID
	if clientID == "" {
		clientID = discord.DefaultClientID
	}
	svc := discord.Start(clientID, player, log)
	return func() {
		_ = svc.Close()
	}
}
