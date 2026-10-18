package infocheck

import (
	"errors"
	"fmt"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// ErrMalformed is info bytes the engine must not parse: not one bencoded value, nested too deep for
// its recursive decoder (finding 37), a file tree that would cost its decoder too much (finding 38),
// or a value that is not an info dictionary.
var ErrMalformed = errors.New("the metadata is not a well-formed info dictionary")

const (
	// maxDepth bounds nesting; the engine's decoder recurses once per level with no limit of its
	// own. An info nests a file's path a few levels below the top.
	maxDepth = 64
	// maxTreeWork bounds the bytes the engine's file tree decoder re-parses, as a multiple of the
	// tree's length: it decodes every folder again for each folder above it.
	maxTreeWork = 16
)

// CheckBytes parses info bytes the way the engine will and reports whether it may apply them: at
// most l.MetadataBytes, one dictionary the engine's decoder can parse without unbounded cost, and an
// info Check admits. It is the one entry point for raw bytes, so the engine never parses bytes that
// have not passed it.
func CheckBytes(b []byte, l Limits) (*metainfo.Info, error) {
	if len(b) > l.MetadataBytes {
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrInfoTooLarge, len(b), l.MetadataBytes)
	}
	if len(b) == 0 || b[0] != 'd' {
		return nil, fmt.Errorf("%w: the info is not a dictionary", ErrMalformed)
	}
	end, treeLen, work, err := scanValue(b)
	if err == nil && end != len(b) {
		err = fmt.Errorf("%d bytes after the value", len(b)-end)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if work > maxTreeWork*treeLen {
		return nil, fmt.Errorf("%w: the file tree of %d bytes would be decoded as %d", ErrMalformed, treeLen, work)
	}
	var info metainfo.Info
	if err := bencode.Unmarshal(b, &info); err != nil {
		return nil, fmt.Errorf("%w: unmarshal: %v", ErrMalformed, err)
	}
	if err := Check(&info, l); err != nil {
		return nil, err
	}
	return &info, nil
}

// CheckMessage reports whether the engine may decode a peer's extended message: its first value
// must pass the same syntax, depth and integer bounds as an info. Bytes after that value are
// allowed, as a metadata piece carries them.
func CheckMessage(payload []byte) error {
	if _, _, _, err := scanValue(payload); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return nil
}

// maxDigits bounds an integer: the engine's decoder parses a longer one under an unknown key as a
// big integer, in quadratic time. Every integer of an info or a message fits in 19 digits.
const maxDigits = 19

// scanValue walks the bencoded value at the start of b without recursion and checks its syntax,
// depth and integers. It returns where the value ends, the length of the top dictionary's
// "file tree" value, and the bytes the engine's file tree decoder would parse for it: the length of
// every dictionary inside it, the tree included. The tree must be a dictionary: the engine's decoder
// also accepts a one-element list there, which would hide it from this count.
func scanValue(b []byte) (end, treeLen, work int, err error) {
	type frame struct {
		dict, wantKey bool
		start         int
	}
	var stack []frame
	tree := -1 // the stack depth of the file tree's dictionary while inside it
	treeNext := false
	for i := 0; ; {
		if len(stack) > 0 && stack[len(stack)-1].dict && stack[len(stack)-1].wantKey && i < len(b) && (b[i] < '0' || b[i] > '9') && b[i] != 'e' {
			return 0, 0, 0, fmt.Errorf("a dictionary key at byte %d is not a string", i)
		}
		if i == len(b) {
			return 0, 0, 0, errors.New("the bytes end inside a value")
		}
		if treeNext && b[i] != 'd' {
			return 0, 0, 0, fmt.Errorf("the file tree at byte %d is not a dictionary", i)
		}
		isTreeValue := treeNext
		treeNext = false
		switch c := b[i]; {
		case c == 'd' || c == 'l':
			if len(stack) == maxDepth {
				return 0, 0, 0, fmt.Errorf("nested deeper than %d at byte %d", maxDepth, i)
			}
			if isTreeValue {
				tree = len(stack)
			}
			stack = append(stack, frame{dict: c == 'd', wantKey: true, start: i})
			i++
			continue
		case c == 'e':
			if len(stack) == 0 {
				return 0, 0, 0, fmt.Errorf("an end at byte %d with nothing open", i)
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			i++
			if f.dict && tree >= 0 && len(stack) >= tree {
				work += i - f.start
				if len(stack) == tree {
					treeLen, tree = i-f.start, -1
				}
			}
		case c == 'i':
			j := i + 1
			if j < len(b) && b[j] == '-' {
				j++
			}
			digits := j
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			if j == digits || j-digits > maxDigits || j == len(b) || b[j] != 'e' {
				return 0, 0, 0, fmt.Errorf("an integer at byte %d", i)
			}
			i = j + 1
		case c >= '0' && c <= '9':
			n, j := 0, i
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				if n = n*10 + int(b[j]-'0'); n > len(b) {
					return 0, 0, 0, fmt.Errorf("a string at byte %d longer than the bytes", i)
				}
				j++
			}
			if j == len(b) || b[j] != ':' || n > len(b)-j-1 {
				return 0, 0, 0, fmt.Errorf("a string at byte %d", i)
			}
			i = j + 1 + n
			if len(stack) == 1 && stack[0].dict && stack[0].wantKey {
				treeNext = string(b[j+1:i]) == "file tree"
			}
		default:
			return 0, 0, 0, fmt.Errorf("a byte %q at %d that starts no value", c, i)
		}
		if len(stack) == 0 {
			return i, treeLen, work, nil
		}
		top := &stack[len(stack)-1]
		if top.dict {
			top.wantKey = !top.wantKey
		}
	}
}
