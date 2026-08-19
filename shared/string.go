package shared

import "strings"

// Itoa converts an integer to a string without using fmt.
func Itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// URLEncode does minimal form-encoding (space to +, alphanum and -_.~ safe).
func URLEncode(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteString(hexOf(c))
		}
	}
	return b.String()
}

func hexOf(c byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[c>>4], digits[c&0x0F]})
}
