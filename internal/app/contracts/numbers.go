package contracts

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
)

// 64-bit quantities. Each crosses the bridge as a decimal string ("0", "18446744073709551615"):
// JavaScript numbers are exact only up to 2^53−1. Fields of these types carry ts_type:"string" so
// the generated TypeScript models agree; a test checks every DTO for it.
type (
	// ByteCount is a size or an amount of data in bytes.
	ByteCount uint64
	// BytesPerSecond is a transfer rate or a rate limit in bytes per second.
	BytesPerSecond uint64
	// DurationSeconds is a duration in whole seconds.
	DurationSeconds uint64
	// Revision orders the states of the library within one runtime.
	Revision uint64
)

// decimalText is a non-negative integer without sign, spaces, exponent or leading zeros.
var decimalText = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

var errNotDecimal = errors.New(`want a decimal string such as "0" or "1048576"`)

func marshalDecimal(v uint64) ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(v, 10) + `"`), nil
}

func unmarshalDecimal(data []byte) (uint64, error) {
	var text string
	if err := json.Unmarshal(data, &text); err != nil || !decimalText.MatchString(text) {
		return 0, errNotDecimal
	}
	return strconv.ParseUint(text, 10, 64)
}

func (v ByteCount) MarshalJSON() ([]byte, error) { return marshalDecimal(uint64(v)) }

func (v *ByteCount) UnmarshalJSON(data []byte) error {
	n, err := unmarshalDecimal(data)
	*v = ByteCount(n)
	return err
}

func (v BytesPerSecond) MarshalJSON() ([]byte, error) { return marshalDecimal(uint64(v)) }

func (v *BytesPerSecond) UnmarshalJSON(data []byte) error {
	n, err := unmarshalDecimal(data)
	*v = BytesPerSecond(n)
	return err
}

func (v DurationSeconds) MarshalJSON() ([]byte, error) { return marshalDecimal(uint64(v)) }

func (v *DurationSeconds) UnmarshalJSON(data []byte) error {
	n, err := unmarshalDecimal(data)
	*v = DurationSeconds(n)
	return err
}

func (v Revision) MarshalJSON() ([]byte, error) { return marshalDecimal(uint64(v)) }

func (v *Revision) UnmarshalJSON(data []byte) error {
	n, err := unmarshalDecimal(data)
	*v = Revision(n)
	return err
}
