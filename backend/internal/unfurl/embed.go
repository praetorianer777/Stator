package unfurl

import (
	"net/url"
	"regexp"
	"strings"
)

// LinkEmbed is the player an allowlisted site's page is shown in, inside the
// card's frame. Src is always one of FrameSources, so the browser's policy
// lets it load.
type LinkEmbed struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Src      string `json:"src"`
}

// EmbedKinds are what an embed shows.
var EmbedKinds = []string{"video", "design"}

type provider struct {
	name, kind, frame string
	src               func(u *url.URL) string
}

var (
	youTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	vimeoID   = regexp.MustCompile(`^[0-9]{1,12}$`)
	figmaPath = regexp.MustCompile(`^/(file|design|proto|board)/[A-Za-z0-9]{10,128}(/[^/]*)?$`)
)

// providers are the sites a link may embed: a player of their own, made
// for embedding, at an address of their own. YouTube's is its privacy
// enhanced player, which sets no cookie until the video is played.
var providers = []provider{
	{name: "YouTube", kind: "video", frame: "https://www.youtube-nocookie.com", src: youTube},
	{name: "Vimeo", kind: "video", frame: "https://player.vimeo.com", src: vimeo},
	{name: "Figma", kind: "design", frame: "https://www.figma.com", src: figma},
}

// FrameSources are the origins an embed loads from, for the
// Content-Security-Policy's frame-src.
func FrameSources() []string {
	out := make([]string, len(providers))
	for i, p := range providers {
		out[i] = p.frame
	}
	return out
}

// EmbedFor names the player for an address of an allowlisted site, or nil.
func EmbedFor(u *url.URL) *LinkEmbed {
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil
	}
	for _, p := range providers {
		if src := p.src(u); src != "" {
			return &LinkEmbed{Provider: p.name, Kind: p.kind, Src: src}
		}
	}
	return nil
}

func host(u *url.URL) string {
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func youTube(u *url.URL) string {
	var id string
	switch host(u) {
	case "youtube.com", "m.youtube.com", "youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/shorts/"), strings.HasPrefix(u.Path, "/embed/"), strings.HasPrefix(u.Path, "/live/"):
			id = u.Path[strings.LastIndexByte(u.Path, '/')+1:]
		}
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	}
	if !youTubeID.MatchString(id) {
		return ""
	}
	return "https://www.youtube-nocookie.com/embed/" + id
}

func vimeo(u *url.URL) string {
	var id string
	switch host(u) {
	case "vimeo.com":
		id = strings.TrimPrefix(u.Path, "/")
	case "player.vimeo.com":
		id = strings.TrimPrefix(u.Path, "/video/")
	}
	if !vimeoID.MatchString(id) {
		return ""
	}
	return "https://player.vimeo.com/video/" + id
}

// Figma embeds by the address of the file itself, given to its embed page.
func figma(u *url.URL) string {
	if host(u) != "figma.com" || !figmaPath.MatchString(u.Path) {
		return ""
	}
	file := "https://www.figma.com" + u.EscapedPath()
	if node := u.Query().Get("node-id"); node != "" {
		file += "?node-id=" + url.QueryEscape(node)
	}
	return "https://www.figma.com/embed?embed_host=stator&url=" + url.QueryEscape(file)
}
