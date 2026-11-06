package uistream

import (
	"errors"
	"strconv"
)

// encoding/json replaces lone UTF-16 surrogate escapes with U+FFFD. Reject them
// in source strings before decoding, rather than silently changing opaque keys,
// callback IDs or native text. Escaped backslashes are ordinary string data.
func checkUnicodeEscapes(raw []byte) error {
	inString := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return errors.New("unfinished string escape")
		}
		if raw[i] != 'u' {
			continue
		}
		value, err := unicodeEscape(raw, i)
		if err != nil {
			return err
		}
		switch {
		case value >= 0xdc00 && value <= 0xdfff:
			return errors.New("unpaired Unicode surrogate")
		case value >= 0xd800 && value <= 0xdbff:
			if i+10 >= len(raw) || raw[i+5] != '\\' || raw[i+6] != 'u' {
				return errors.New("unpaired Unicode surrogate")
			}
			low, err := unicodeEscape(raw, i+6)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return errors.New("unpaired Unicode surrogate")
			}
			i += 10
		default:
			i += 4
		}
	}
	return nil
}
func unicodeEscape(raw []byte, start int) (uint64, error) {
	if start+4 >= len(raw) {
		return 0, errors.New("unfinished Unicode escape")
	}
	return strconv.ParseUint(string(raw[start+1:start+5]), 16, 16)
}
