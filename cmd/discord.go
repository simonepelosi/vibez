package cmd

import (
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/discord"
	playerpkg "github.com/simone-vibes/vibez/internal/player"
)

func startDiscordRPC(cfg *config.Config, player interface {
	Subscribe() <-chan playerpkg.State
}, log func(string)) *discord.Service {
	if noDiscord || !cfg.DiscordRPCEnabled() {
		return nil
	}
	clientID := cfg.DiscordClientID
	if clientID == "" {
		clientID = discord.DefaultClientID
	}
	return discord.Start(clientID, player, log)
}
