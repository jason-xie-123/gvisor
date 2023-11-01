package buffer

import (
	"encoding/base64"
	"fmt"
	"math/bits"
	"net"
	"unsafe"

	"gvisor.dev/gvisor/pkg/sync"
)

const fastSmalls = true // enable fast path for small integers

func NewViewWithTag(tag string, cap int) *View {
	c := newChunk(cap)
	v := viewPool.Get().(*View)
	*v = View{chunk: c}

	v.tag = tag
	addViewTag(tag)

	return v
}

func NewViewSizeWithTag(tag string, size int) *View {
	v := NewView(size)
	v.Grow(size)

	v.tag = tag
	addViewTag(tag)

	return v
}

func NewViewWithDataWithTag(tag string, data []byte) *View {
	c := newChunk(len(data))
	v := viewPool.Get().(*View)
	*v = View{chunk: c}
	v.Write(data)

	v.tag = tag
	addViewTag(tag)

	return v
}

func NewViewByBase64EncodeWithTag(tag string, src []byte) *View {
	buf := NewViewSize(base64.StdEncoding.EncodedLen(len(src)))

	buf.tag = tag
	addViewTag(tag)

	base64.StdEncoding.Encode(buf.AsSlice(), src)

	return buf
}

func NewViewByBase64DecodeWithTag(tag string, src []byte) (*View, error) {
	buf := NewViewSize(base64.StdEncoding.DecodedLen(len(src)))

	buf.tag = tag
	addViewTag(tag)

	n, err := base64.StdEncoding.Decode(buf.AsSlice(), src)

	if err != nil {
		buf.Release()
		return nil, err
	}

	buf.read = 0
	buf.write = n

	return buf, nil
}

func (v *View) DecodeByBase64WithTag(tag string) (*View, error) {
	buf := NewViewSize(base64.StdEncoding.DecodedLen(len(v.AsSlice())))

	buf.tag = tag
	addViewTag(tag)

	n, err := base64.StdEncoding.Decode(buf.AsSlice(), v.AsSlice())

	if err != nil {
		buf.Release()
		return nil, err
	}

	buf.read = 0
	buf.write = n

	return buf, nil
}

func (v *View) EncodeToBase64WithTag(tag string) *View {
	buf := NewViewSize(base64.StdEncoding.EncodedLen(len(v.AsSlice())))

	buf.tag = tag
	addViewTag(tag)

	base64.StdEncoding.Encode(buf.AsSlice(), v.AsSlice())

	return buf
}

// FormatUint returns the string representation of i in the given base,
// for 2 <= base <= 36. The result uses the lower-case letters 'a' to 'z'
// for digit values >= 10.
func (v *View) WriteUintAsString(i uint64, base int) {
	if fastSmalls && i < nSmalls && base == 10 {
		small(v, int(i))
	}
	formatBits(v, i, base, false)
}

// FormatInt returns the string representation of i in the given base,
// for 2 <= base <= 36. The result uses the lower-case letters 'a' to 'z'
// for digit values >= 10.
func (v *View) WriteIntAsString(i int64, base int) {
	if fastSmalls && 0 <= i && i < nSmalls && base == 10 {
		small(v, int(i))
	}
	formatBits(v, uint64(i), base, i < 0)
}

// small returns the string for an i with 0 <= i < nSmalls.
func small(v *View, i int) {
	if i < 10 {
		v.Write(StringToBytes(digits[i : i+1]))
		return
	}
	v.Write(StringToBytes(smallsString[i*2 : i*2+2]))
}

const nSmalls = 100

const smallsString = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

const host32bit = ^uint(0)>>32 == 0

const digits = "0123456789abcdefghijklmnopqrstuvwxyz"

// formatBits computes the string representation of u in the given base.
// If neg is set, u is treated as negative int64 value. If append_ is
// set, the string is appended to dst and the resulting byte slice is
// returned as the first result value; otherwise the string is returned
// as the second result value.
func formatBits(v *View, u uint64, base int, neg bool) {
	if base < 2 || base > len(digits) {
		panic("strconv: illegal AppendInt/FormatInt base")
	}
	// 2 <= base && base <= len(digits)

	var a = NewViewSizeWithTag("tag-it", 64+1) // +1 for sign of 64bit value in base 2
	i := a.Size()

	if neg {
		u = -u
	}

	// convert bits
	// We use uint values where we can because those will
	// fit into a single register even on a 32bit machine.
	if base == 10 {
		// common case: use constants for / because
		// the compiler can optimize it into a multiply+shift

		if host32bit {
			// convert the lower digits using 32bit operations
			for u >= 1e9 {
				// Avoid using r = a%b in addition to q = a/b
				// since 64bit division and modulo operations
				// are calculated by runtime functions on 32bit machines.
				q := u / 1e9
				us := uint(u - q*1e9) // u % 1e9 fits into a uint
				for j := 4; j > 0; j-- {
					is := us % 100 * 2
					us /= 100
					i -= 2
					a.AsSlice()[i+1] = smallsString[is+1]
					a.AsSlice()[i+0] = smallsString[is+0]
				}

				// us < 10, since it contains the last digit
				// from the initial 9-digit us.
				i--
				a.AsSlice()[i] = smallsString[us*2+1]

				u = q
			}
			// u < 1e9
		}

		// u guaranteed to fit into a uint
		us := uint(u)
		for us >= 100 {
			is := us % 100 * 2
			us /= 100
			i -= 2
			a.AsSlice()[i+1] = smallsString[is+1]
			a.AsSlice()[i+0] = smallsString[is+0]
		}

		// us < 100
		is := us * 2
		i--
		a.AsSlice()[i] = smallsString[is+1]
		if us >= 10 {
			i--
			a.AsSlice()[i] = smallsString[is]
		}

	} else if isPowerOfTwo(base) {
		// Use shifts and masks instead of / and %.
		// Base is a power of 2 and 2 <= base <= len(digits) where len(digits) is 36.
		// The largest power of 2 below or equal to 36 is 32, which is 1 << 5;
		// i.e., the largest possible shift count is 5. By &-ind that value with
		// the constant 7 we tell the compiler that the shift count is always
		// less than 8 which is smaller than any register width. This allows
		// the compiler to generate better code for the shift operation.
		shift := uint(bits.TrailingZeros(uint(base))) & 7
		b := uint64(base)
		m := uint(base) - 1 // == 1<<shift - 1
		for u >= b {
			i--
			a.AsSlice()[i] = digits[uint(u)&m]
			u >>= shift
		}
		// u < base
		i--
		a.AsSlice()[i] = digits[uint(u)]
	} else {
		// general case
		b := uint64(base)
		for u >= b {
			i--
			// Avoid using r = a%b in addition to q = a/b
			// since 64bit division and modulo operations
			// are calculated by runtime functions on 32bit machines.
			q := u / b
			a.AsSlice()[i] = digits[uint(u-q*b)]
			u = q
		}
		// u < base
		i--
		a.AsSlice()[i] = digits[uint(u)]
	}

	// add sign, if any
	if neg {
		i--
		a.AsSlice()[i] = '-'
	}

	v.Write(a.AsSlice()[i:])
	a.Release()
}

func isPowerOfTwo(x int) bool {
	return x&(x-1) == 0
}

const ipV4Format = "%d.%d.%d.%d"

// ubtoa encodes the string form of the integer v to dst[start:] and
// returns the number of bytes written to dst. The caller must ensure
// that dst has sufficient length.
func ubtoa(dst *View, start int, v byte) int {
	if v < 10 {
		dst.WriteByte(v + '0')
		return 1
	} else if v < 100 {
		dst.WriteByte(v/10 + '0')
		dst.WriteByte(v%10 + '0')
		return 2
	}
	dst.WriteByte(v/100 + '0')
	dst.WriteByte((v/10)%10 + '0')
	dst.WriteByte(v%10 + '0')

	return 3
}

func (v *View) WriteUint16AsString(number uint16) {
	if number < 10 {
		v.WriteByte(byte(number + 48))
	} else if number < 100 {
		left := number
		tmp := left / 10
		v.WriteByte(byte(tmp + 48))
		v.WriteByte(byte(left - tmp*10 + 48))
	} else if number < 1000 {
		left := number
		tmp := left / 100
		v.WriteByte(byte(tmp + 48))
		left -= tmp * 100
		tmp = left / 10
		v.WriteByte(byte(tmp + 48))
		v.WriteByte(byte(left - tmp*10 + 48))
	} else if number < 10000 {
		left := number
		tmp := left / 1000
		v.WriteByte(byte(tmp + 48))
		left -= tmp * 1000
		tmp = left / 100
		v.WriteByte(byte(tmp + 48))
		left -= tmp * 100
		tmp = left / 10
		v.WriteByte(byte(tmp + 48))
		v.WriteByte(byte(left - tmp*10 + 48))
	} else if number < 65535 {
		left := number
		tmp := left / 10000
		v.WriteByte(byte(tmp + 48))
		left -= tmp * 10000
		tmp = left / 1000
		v.WriteByte(byte(tmp + 48))
		left -= tmp * 1000
		tmp = left / 100
		v.WriteByte(byte(tmp + 48))
		left -= tmp * 100
		tmp = left / 10
		v.WriteByte(byte(tmp + 48))
		v.WriteByte(byte(left - tmp*10 + 48))
	}
}

// String implements the fmt.Stringer interface.
func (bufView *View) WriteIPIntoStringBuf(ip net.IP) {
	switch l := len(ip); l {
	case 4:
		n := ubtoa(bufView, 0, ip[0])
		bufView.WriteByte('.')
		n++

		n += ubtoa(bufView, n, ip[1])
		bufView.WriteByte('.')
		n++

		n += ubtoa(bufView, n, ip[2])
		bufView.WriteByte('.')
		n++

		n += ubtoa(bufView, n, ip[3])
	case 16:
		// Find the longest subsequence of hexadecimal zeros.
		start, end := -1, -1
		for i := 0; i < len(ip); i += 2 {
			j := i
			for j < len(ip) && ip[j] == 0 && ip[j+1] == 0 {
				j += 2
			}
			if j > i+2 && j-i > end-start {
				start, end = i, j
			}
		}

		for i := 0; i < len(ip); i += 2 {
			if i == start {
				bufView.WriteByte(':')
				bufView.WriteByte(':')

				i = end
				if end >= len(ip) {
					break
				}
			} else if i > 0 {
				bufView.WriteByte(':')
			}
			v := uint16(ip[i+0])<<8 | uint16(ip[i+1])
			if v == 0 {
				bufView.WriteByte('0')
			} else {
				const digits = "0123456789abcdef"
				for i := uint(3); i < 4; i-- {
					if v := v >> (i * 4); v != 0 {
						bufView.WriteByte(digits[v&0xf])
					}
				}
			}
		}
	default:
		fmt.Fprintf(bufView, "%x", ip[:l])
	}
}

func StringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func BytesToString(b []byte) string {
	return unsafe.String(&b[0], len(b))
}

func BytesToStringWithLen(b []byte, len int) string {
	return unsafe.String(&b[0], len)
}

func clearString(str string) {
	slice := StringToBytes(str)
	for i := range slice {
		if i >= 1 {
			slice[i] = byte(i % 254)
		}
	}
}

func clearBytes(bits []byte) {
	for i := range bits {
		if i >= 1 {
			bits[i] = byte(i % 254)
		}
	}
}

var (
	debugViewMap   map[string]int = make(map[string]int)
	debugViewMutex sync.Mutex
)

func addViewTag(tag string) {
	if debugViewSupport {
		if len(tag) > 0 {
			debugViewMutex.Lock()

			if val, ok := debugViewMap[tag]; ok {
				debugViewMap[tag] = val + 1
			} else {
				debugViewMap[tag] = 1
			}

			debugViewMutex.Unlock()
		}
	}
}

func removeViewTag(tag string) {
	if debugViewSupport {
		if len(tag) > 0 {
			debugViewMutex.Lock()

			if val, ok := debugViewMap[tag]; ok {
				debugViewMap[tag] = val - 1
			} else {
				debugViewMap[tag] = -1
			}

			debugViewMutex.Unlock()
		}
	}
}
