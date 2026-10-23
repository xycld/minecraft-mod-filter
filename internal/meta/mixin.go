package meta

// MixinConfig is a parsed mixin configuration (*.mixins.json).
type MixinConfig struct {
	Package string
	Common  []string // "mixins": applied on both sides
	Client  []string
	Server  []string
}

func ParseMixinConfig(data []byte) (*MixinConfig, error) {
	var raw struct {
		Package string   `json:"package"`
		Mixins  []string `json:"mixins"`
		Client  []string `json:"client"`
		Server  []string `json:"server"`
	}
	if err := unmarshalLenient(data, &raw); err != nil {
		return nil, err
	}
	return &MixinConfig{Package: raw.Package, Common: raw.Mixins, Client: raw.Client, Server: raw.Server}, nil
}

// ClassPath returns the class file path of a mixin listed in this config.
func (c *MixinConfig) ClassPath(mixin string) string {
	name := mixin
	if c.Package != "" {
		name = c.Package + "." + mixin
	}
	b := []byte(name)
	for i, ch := range b {
		if ch == '.' {
			b[i] = '/'
		}
	}
	return string(b) + ".class"
}
