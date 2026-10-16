package fixture

import (
	"bytes"
	"maps"
	"slices"
	"strconv"
)

// raw is an already encoded bencode value, such as the info dictionary inside a metainfo file.
type raw []byte

// encode returns v in bencode. Values are int64, string, raw, []any and map[string]any; dictionary
// keys are written in byte order, as BEP 3 requires. The generator writes its own bencode so every
// byte of a fixture, invalid ones included, is decided here and not by the engine's encoder.
func encode(v any) []byte {
	var buf bytes.Buffer
	write(&buf, v)
	return buf.Bytes()
}

func write(buf *bytes.Buffer, v any) {
	switch v := v.(type) {
	case int64:
		buf.WriteString("i" + strconv.FormatInt(v, 10) + "e")
	case string:
		buf.WriteString(strconv.Itoa(len(v)) + ":" + v)
	case raw:
		buf.Write(v)
	case []any:
		buf.WriteByte('l')
		for _, item := range v {
			write(buf, item)
		}
		buf.WriteByte('e')
	default:
		dict := v.(map[string]any)
		buf.WriteByte('d')
		for _, key := range slices.Sorted(maps.Keys(dict)) {
			write(buf, key)
			write(buf, dict[key])
		}
		buf.WriteByte('e')
	}
}
