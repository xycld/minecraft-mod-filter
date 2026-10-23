package meta

import (
	"bytes"
	"encoding/json"
)

// unmarshalLenient decodes JSON as written by hand in mod jars: it tolerates
// a BOM, comments, trailing commas and raw control characters inside strings.
func unmarshalLenient(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err == nil {
		return nil
	}
	return json.Unmarshal(cleanJSON(data), v)
}

func cleanJSON(in []byte) []byte {
	in = bytes.TrimPrefix(in, []byte("\xef\xbb\xbf"))
	out := make([]byte, 0, len(in))
	inString := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inString {
			switch {
			case c == '\\' && i+1 < len(in):
				out = append(out, c, in[i+1])
				i++
			case c == '"':
				inString = false
				out = append(out, c)
			case c == '\n':
				out = append(out, '\\', 'n')
			case c == '\r':
				out = append(out, '\\', 'r')
			case c == '\t':
				out = append(out, '\\', 't')
			case c < 0x20:
				// drop other control characters
			default:
				out = append(out, c)
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			end := bytes.Index(in[i+2:], []byte("*/"))
			if end < 0 {
				i = len(in)
			} else {
				i += end + 3
			}
		case c == ',':
			// drop the comma if the next significant character closes a container
			j := i + 1
			for j < len(in) && (in[j] == ' ' || in[j] == '\n' || in[j] == '\r' || in[j] == '\t') {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// stringOrList accepts either a JSON string or an array of strings.
type stringOrList []string

func (s *stringOrList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}
