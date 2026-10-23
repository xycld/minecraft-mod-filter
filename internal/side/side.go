// Package side models where a mod has to be installed.
package side

import "fmt"

// Need says whether one side (client or server) needs a mod.
type Need uint8

const (
	Unknown Need = iota
	Unsupported
	Optional
	Required
)

var needNames = [...]string{"unknown", "unsupported", "optional", "required"}

func (n Need) String() string { return needNames[n] }

func ParseNeed(s string) (Need, error) {
	for i, name := range needNames {
		if name == s {
			return Need(i), nil
		}
	}
	return Unknown, fmt.Errorf("invalid side need %q", s)
}

func (n Need) MarshalText() ([]byte, error) { return []byte(n.String()), nil }

func (n *Need) UnmarshalText(b []byte) error {
	v, err := ParseNeed(string(b))
	*n = v
	return err
}

// Keep reports whether a side should have the mod installed.
func (n Need) Keep() bool { return n == Required || n == Optional || n == Unknown }

// Env is the pair of client and server needs for one mod.
type Env struct {
	Client Need `json:"client"`
	Server Need `json:"server"`
}

func (e Env) String() string { return e.Category() }

// Category is a short human-oriented summary of an Env.
func (e Env) Category() string {
	switch {
	case e.Client == Unknown && e.Server == Unknown:
		return "unknown"
	case e.Server == Unsupported && e.Client == Unsupported:
		return "unsupported"
	case e.Server == Unsupported:
		return "client-only"
	case e.Client == Unsupported:
		return "server-only"
	case e.Client == Required && e.Server == Required:
		return "both"
	case e.Client == Required && e.Server == Optional:
		return "client (server optional)"
	case e.Server == Required && e.Client == Optional:
		return "server (client optional)"
	case e.Client == Optional && e.Server == Optional:
		return "either"
	case e.Server == Unknown:
		return "client: " + e.Client.String()
	case e.Client == Unknown:
		return "server: " + e.Server.String()
	}
	return e.Client.String() + "/" + e.Server.String()
}

// FromModrinth converts a Modrinth version "environment" value.
func FromModrinth(env string) (Env, bool) {
	switch env {
	case "client_and_server":
		return Env{Required, Required}, true
	case "client_only":
		return Env{Required, Unsupported}, true
	case "client_only_server_optional":
		return Env{Required, Optional}, true
	case "singleplayer_only":
		return Env{Required, Unsupported}, true
	case "server_only":
		return Env{Optional, Required}, true
	case "server_only_client_optional":
		return Env{Optional, Required}, true
	case "dedicated_server_only":
		return Env{Unsupported, Required}, true
	case "client_or_server":
		return Env{Optional, Optional}, true
	case "client_or_server_prefers_both":
		return Env{Optional, Optional}, true
	}
	return Env{}, false
}

// FromLegacyModrinth converts the project-level client_side/server_side pair.
func FromLegacyModrinth(client, server string) (Env, bool) {
	c, err1 := ParseNeed(client)
	s, err2 := ParseNeed(server)
	if err1 != nil || err2 != nil || (c == Unknown && s == Unknown) {
		return Env{}, false
	}
	return Env{c, s}, true
}
