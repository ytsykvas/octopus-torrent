package contracts

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// fieldProblem is one shape problem of a decoded request.
type fieldProblem struct{ field, reason string }

// shape holds the shape problems found while decoding a request. encoding/json (which Wails uses
// for arguments) would give a missing key its zero value — a missing "wanted" would read as false —
// and would ignore unknown keys, so requests record them here and Validate rejects them.
type shape []fieldProblem

// problem returns the first shape problem as request.invalid, its field prefixed with prefix, or
// nil when the request was well formed.
func (s shape) problem(prefix string) *AppError {
	if len(s) == 0 {
		return nil
	}
	return InvalidRequest(prefix+s[0].field, s[0].reason)
}

var errNotObject = errors.New("a request must be a JSON object")

// decodeFields decodes a JSON object into the struct target points to, one field at a time. Every
// json-tagged exported field is required; null is allowed only for pointer fields. Missing, null,
// unknown and badly typed keys become shape problems (in field order, unknown keys last) instead of
// failing the whole value, so the request is rejected naming its field. Only a payload that is not
// a JSON object is an error.
func decodeFields(data []byte, target any) (shape, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, errNotObject
	}
	value := reflect.ValueOf(target).Elem()
	known := map[string]bool{}
	var problems shape
	for i := range value.NumField() {
		field := value.Type().Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "" {
			continue
		}
		known[name] = true
		encoded, present := raw[name]
		switch {
		case !present:
			problems = append(problems, fieldProblem{name, ReasonMissing})
		case string(encoded) == "null" && field.Type.Kind() != reflect.Pointer:
			problems = append(problems, fieldProblem{name, ReasonNull})
		default:
			if err := json.Unmarshal(encoded, value.Field(i).Addr().Interface()); err != nil {
				problems = append(problems, fieldProblem{name, ReasonType})
			}
		}
	}
	for _, name := range sortedKeys(raw) {
		if !known[name] {
			problems = append(problems, fieldProblem{name, ReasonUnknown})
		}
	}
	return problems, nil
}
