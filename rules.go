package main

import "github.com/wwricu/bad-proxy-core/router"

// The "bypass blocked" routing mode sends only a curated set of commonly
// blocked services through the proxy and everything else direct. The list is
// deliberately short — a preset, not a full geosite import.
//
// Each entry is a core router rule. "domain:" is label-boundary suffix
// matching (google.com also matches www.google.com, NOT notgoogle.com), the
// same semantics as an unprefixed line in v2fly/domain-list-community. Add a
// service by its registrable domain; use "full:" for an exact host.
var blockedDomainRules = []string{
	// google + the services that ride on it
	"domain:google.com",
	"domain:googleapis.com",
	"domain:googleusercontent.com",
	"domain:gstatic.com",
	"domain:ggpht.com",
	"domain:youtube.com",
	"domain:youtu.be",
	"domain:googlevideo.com",
	"domain:blogger.com",
	"domain:blogspot.com",

	// meta (facebook / instagram / whatsapp)
	"domain:facebook.com",
	"domain:fbcdn.net",
	"domain:messenger.com",
	"domain:instagram.com",
	"domain:whatsapp.com",
	"domain:whatsapp.net",

	// x / twitter
	"domain:twitter.com",
	"domain:x.com",
	"domain:t.co",

	// messaging & chat
	"domain:telegram.org",
	"domain:t.me",
	"domain:discord.com",
	"domain:discordapp.com",
	"domain:discord.gg",
	"domain:line.me",

	// video & streaming
	"domain:netflix.com",
	"domain:nflxext.com",
	"domain:nflximg.net",
	"domain:twitch.tv",
	"domain:pornhub.com",

	// code & knowledge
	"domain:github.com",
	"domain:githubusercontent.com",
	"domain:wikipedia.org",
	"domain:wikimedia.org",
	"domain:medium.com",
	"domain:stackoverflow.net",

	// press
	"domain:bbc.com",
	"domain:nytimes.com",
	"domain:theguardian.com",
	"domain:bloomberg.com",
	"domain:wsj.com",

	// misc infra commonly blocked
	"domain:duckduckgo.com",
	"domain:dropbox.com",
	"domain:binance.com",
	"domain:coingecko.com",
}

// proxyRouterTag is the outbound tag the bypass rules route to; the node is
// registered under it (instead of the empty default tag) so unmatched traffic
// falls back to the direct (freedom) default outbound.
const proxyRouterTag = "proxy"

// blockedRouter builds the router section for bypass mode: hosts hitting any
// blockedDomainRules leave through the proxy-tagged outbound.
func blockedRouter() []router.Config {
	rules := make([]string, len(blockedDomainRules))
	copy(rules, blockedDomainRules)
	return []router.Config{{Tag: proxyRouterTag, Rules: rules}}
}
