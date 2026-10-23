package meta

import (
	"encoding/json"
	"sort"
)

// FabricMod is the subset of fabric.mod.json and quilt.mod.json that matters
// for side detection. Quilt metadata is normalised into the same shape.
type FabricMod struct {
	ID          string
	Version     string
	Name        string
	Environment string              // "*", "client" or "server" ("" when absent)
	Entrypoints map[string][]string // entrypoint key -> class names
	Mixins      []MixinRef
	Depends     []string // mandatory dependencies
	Provides    []string
	Jars        []string // nested jar paths
	Quilt       bool
}

// MixinRef is a mixin config file referenced from mod metadata.
type MixinRef struct {
	Config      string
	Environment string // "*", "client" or "server"
}

type fabricEntrypoint struct{ Value string }

func (e *fabricEntrypoint) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &e.Value); err == nil {
		return nil
	}
	var obj struct{ Value string }
	err := json.Unmarshal(b, &obj)
	e.Value = obj.Value
	return err
}

type fabricMixin MixinRef

func (m *fabricMixin) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &m.Config); err == nil {
		m.Environment = "*"
		return nil
	}
	var obj struct {
		Config      string
		Environment string
	}
	err := json.Unmarshal(b, &obj)
	m.Config, m.Environment = obj.Config, obj.Environment
	if m.Environment == "" {
		m.Environment = "*"
	}
	return err
}

// ParseFabric parses fabric.mod.json.
func ParseFabric(data []byte) (*FabricMod, error) {
	var raw struct {
		ID          string
		Version     string
		Name        string
		Environment string
		Entrypoints map[string][]fabricEntrypoint
		Mixins      []fabricMixin
		Depends     map[string]json.RawMessage
		Provides    []string
		Jars        []struct{ File string }
	}
	if err := unmarshalLenient(data, &raw); err != nil {
		return nil, err
	}
	m := &FabricMod{
		ID:          raw.ID,
		Version:     raw.Version,
		Name:        raw.Name,
		Environment: raw.Environment,
		Entrypoints: map[string][]string{},
		Provides:    raw.Provides,
	}
	for key, eps := range raw.Entrypoints {
		for _, ep := range eps {
			m.Entrypoints[key] = append(m.Entrypoints[key], ep.Value)
		}
	}
	for _, mx := range raw.Mixins {
		m.Mixins = append(m.Mixins, MixinRef(mx))
	}
	for id := range raw.Depends {
		m.Depends = append(m.Depends, id)
	}
	sort.Strings(m.Depends)
	for _, j := range raw.Jars {
		m.Jars = append(m.Jars, j.File)
	}
	return m, nil
}

type quiltDep struct {
	ID       string
	Optional bool
}

func (d *quiltDep) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &d.ID); err == nil {
		return nil
	}
	var obj struct {
		ID       string
		Optional bool
	}
	err := json.Unmarshal(b, &obj)
	d.ID, d.Optional = obj.ID, obj.Optional
	return err
}

// ParseQuilt parses quilt.mod.json into the Fabric shape.
func ParseQuilt(data []byte) (*FabricMod, error) {
	var raw struct {
		QuiltLoader struct {
			ID          string
			Version     string
			Metadata    struct{ Name string }
			Entrypoints map[string]json.RawMessage
			Depends     []json.RawMessage
			Provides    []json.RawMessage
			Jars        []string
		} `json:"quilt_loader"`
		Mixin     stringOrList
		Minecraft struct{ Environment string }
	}
	if err := unmarshalLenient(data, &raw); err != nil {
		return nil, err
	}
	ql := raw.QuiltLoader
	m := &FabricMod{
		ID:          ql.ID,
		Version:     ql.Version,
		Name:        ql.Metadata.Name,
		Entrypoints: map[string][]string{},
		Jars:        ql.Jars,
		Quilt:       true,
	}
	switch raw.Minecraft.Environment {
	case "client":
		m.Environment = "client"
	case "dedicated_server":
		m.Environment = "server"
	case "*":
		m.Environment = "*"
	}
	quiltKeys := map[string]string{"init": "main", "client_init": "client", "server_init": "server"}
	for key, rawEps := range ql.Entrypoints {
		var eps []fabricEntrypoint
		if err := json.Unmarshal(rawEps, &eps); err != nil {
			var one fabricEntrypoint
			if json.Unmarshal(rawEps, &one) != nil {
				continue
			}
			eps = []fabricEntrypoint{one}
		}
		if k, ok := quiltKeys[key]; ok {
			key = k
		}
		for _, ep := range eps {
			m.Entrypoints[key] = append(m.Entrypoints[key], ep.Value)
		}
	}
	for _, rawDep := range ql.Depends {
		var d quiltDep
		if json.Unmarshal(rawDep, &d) == nil && d.ID != "" && !d.Optional {
			m.Depends = append(m.Depends, d.ID)
		}
	}
	for _, rawProv := range ql.Provides {
		var d quiltDep
		if json.Unmarshal(rawProv, &d) == nil && d.ID != "" {
			m.Provides = append(m.Provides, d.ID)
		}
	}
	for _, cfg := range raw.Mixin {
		m.Mixins = append(m.Mixins, MixinRef{Config: cfg, Environment: "*"})
	}
	return m, nil
}
