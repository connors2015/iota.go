// Package trinary provides functions for validating and converting Trits and Trytes.
package trinary

import (
	"github.com/iotaledger/iota.go/bigint"
	. "github.com/iotaledger/iota.go/consts"
	"github.com/pkg/errors"
	"math"
	"regexp"
	"strings"
	"unsafe"
)

var (
	// TryteToTritsLUT is a Look-up-table for Trytes to Trits conversion.
	TryteToTritsLUT = [][]int8{
		{0, 0, 0}, {1, 0, 0}, {-1, 1, 0}, {0, 1, 0},
		{1, 1, 0}, {-1, -1, 1}, {0, -1, 1}, {1, -1, 1},
		{-1, 0, 1}, {0, 0, 1}, {1, 0, 1}, {-1, 1, 1},
		{0, 1, 1}, {1, 1, 1}, {-1, -1, -1}, {0, -1, -1},
		{1, -1, -1}, {-1, 0, -1}, {0, 0, -1}, {1, 0, -1},
		{-1, 1, -1}, {0, 1, -1}, {1, 1, -1}, {-1, -1, 0},
		{0, -1, 0}, {1, -1, 0}, {-1, 0, 0},
	}
)

// Trits is a slice of int8. You should not use cast, use NewTrits instead to ensure the validity.
type Trits = []int8

// ValidTrit returns true if t is a valid trit.
func ValidTrit(t int8) bool {
	if t == -1 || t == 0 || t == 1 {
		return true
	}
	return false
}

// ValidTrits returns true if t is valid trits.
func ValidTrits(t Trits) error {
	for i, tt := range t {
		if valid := ValidTrit(tt); !valid {
			return errors.Wrapf(ErrInvalidTrit, "at index %d", i)
		}
	}
	return nil
}

// NewTrits casts Trits and checks its validity.
func NewTrits(t []int8) (Trits, error) {
	err := ValidTrits(t)
	return t, err
}

// TritsEqual returns true if t and b are equal Trits
func TritsEqual(a, b Trits) (bool, error) {
	if err := ValidTrits(a); err != nil {
		return false, err
	}
	if err := ValidTrits(b); err != nil {
		return false, err
	}

	if len(a) != len(b) {
		return false, nil
	}

	for i := range a {
		if a[i] != b[i] {
			return false, nil
		}
	}
	return true, nil
}

// IntToTrits converts int64 to trits.
func IntToTrits(value int64) Trits {
	if value == 0 {
		return Trits{0}
	}
	var dest Trits
	if value != 0 {
		dest = make(Trits, int(1+math.Floor(math.Log(2*math.Max(1, math.Abs(float64(value))))/math.Log(3))))
	} else {
		dest = make(Trits, 0)
	}

	var absoluteValue int64
	if value < 0 {
		absoluteValue = -value
	} else {
		absoluteValue = value
	}

	i := 0
	for absoluteValue > 0 {
		remainder := absoluteValue % TrinaryRadix
		absoluteValue = int64(math.Floor(float64(absoluteValue / TrinaryRadix)))

		if remainder > MaxTritValue {
			remainder = MinTritValue
			absoluteValue++
		}

		dest[i] = int8(remainder)
		i++
	}

	if value < 0 {
		for i := 0; i < len(dest); i++ {
			dest[i] = -dest[i]
		}
	}

	return dest
}

// TritsToInt converts a slice of trits into an integer and assumes little-endian notation.
func TritsToInt(t Trits) int64 {
	var val int64
	for i := len(t) - 1; i >= 0; i-- {
		val = val*3 + int64(t[i])
	}
	return val
}

// CanTritsToTrytes returns true if t can be converted to trytes.
func CanTritsToTrytes(trits Trits) bool {
	if len(trits) == 0 {
		return false
	}
	return len(trits)%3 == 0
}

// TrailingZeros returns the number of trailing zeros of the given trits.
func TrailingZeros(t Trits) int64 {
	z := int64(0)
	for i := len(t) - 1; i >= 0 && t[i] == 0; i-- {
		z++
	}
	return z
}

// TritsToTrytes converts a slice of trits into trytes. Returns an error if len(t)%3!=0
func TritsToTrytes(trits Trits) (Trytes, error) {
	if !CanTritsToTrytes(trits) {
		return "", errors.Wrap(ErrInvalidTritsLength, "trits slice size must be a multiple of 3")
	}

	o := make([]byte, len(trits)/3)
	for i := 0; i < len(trits)/3; i++ {
		j := trits[i*3] + trits[i*3+1]*3 + trits[i*3+2]*9
		if j < 0 {
			j += int8(len(TryteAlphabet))
		}
		o[i] = TryteAlphabet[j]
	}
	return Trytes(o), nil
}

// MustTritsToTrytes converts a slice of trits into trytes. Panics if len(t)%3!=0
func MustTritsToTrytes(trits Trits) Trytes {
	trytes, err := TritsToTrytes(trits)
	if err != nil {
		panic(err)
	}
	return trytes
}

// 12 * 32 bit
// hex representation of (3^242)/2
var halfThree = []uint32{
	0xa5ce8964,
	0x9f007669,
	0x1484504f,
	0x3ade00d9,
	0x0c24486e,
	0x50979d57,
	0x79a4c702,
	0x48bbae36,
	0xa9f6808b,
	0xaa06a805,
	0xa87fabdf,
	0x5e69ebef,
}

// CanBeHash returns the validity of the trit length
func CanBeHash(trits Trits) bool {
	return len(trits) == HashTrinarySize
}

// TritsToBytes is only defined for hashes, i.e. slices of trits of length 243. It returns 48 bytes.
func TritsToBytes(trits Trits) ([]byte, error) {
	if !CanBeHash(trits) {
		return nil, errors.Wrapf(ErrInvalidTritsLength, "must be %d in size", HashTrinarySize)
	}

	allNeg := true
	// last position should be always zero.
	for _, e := range trits[0 : HashTrinarySize-1] {
		if e != -1 {
			allNeg = false
			break
		}
	}

	// trit to BigInt
	b := make([]byte, 48) // 48 bytes/384 bits

	// 12 * 32 bits = 384 bits
	base := (*(*[]uint32)(unsafe.Pointer(&b)))[0:IntLength]

	if allNeg {
		// if all trits are -1 then we're half way through all the numbers,
		// since they're in two's complement notation.
		copy(base, halfThree)

		// compensate for setting the last position to zero.
		bigint.Not(base)
		bigint.AddSmall(base, 1)

		return bigint.Reverse(b), nil
	}

	revT := make([]int8, len(trits))
	copy(revT, trits)
	size := 1

	for _, e := range ReverseTrits(revT[0 : HashTrinarySize-1]) {
		sz := size
		var carry uint32
		for j := 0; j < sz; j++ {
			v := uint64(base[j])*uint64(TrinaryRadix) + uint64(carry)
			carry = uint32(v >> 32)
			base[j] = uint32(v)
		}

		if carry > 0 {
			base[sz] = carry
			size = size + 1
		}

		trit := uint32(e + 1)

		ns := bigint.AddSmall(base, trit)
		if ns > size {
			size = ns
		}
	}

	if !bigint.IsNull(base) {
		if bigint.MustCmp(halfThree, base) <= 0 {
			// base >= HALF_3
			// just do base - HALF_3
			bigint.MustSub(base, halfThree)
		} else {
			// we don'trits have a wrapping sub.
			// so let's use some bit magic to achieve it
			tmp := make([]uint32, IntLength)
			copy(tmp, halfThree)
			bigint.MustSub(tmp, base)
			bigint.Not(tmp)
			bigint.AddSmall(tmp, 1)
			copy(base, tmp)
		}
	}
	return bigint.Reverse(b), nil
}

// BytesToTrits converts binary to trinary
func BytesToTrits(b []byte) (Trits, error) {
	if len(b) != HashBytesSize {
		return nil, errors.Wrapf(ErrInvalidBytesLength, "must be %d in size", HashBytesSize)
	}

	rb := make([]byte, len(b))
	copy(rb, b)
	bigint.Reverse(rb)

	t := Trits(make([]int8, HashTrinarySize))
	t[HashTrinarySize-1] = 0

	base := (*(*[]uint32)(unsafe.Pointer(&rb)))[0:IntLength] // 12 * 32 bits = 384 bits

	if bigint.IsNull(base) {
		return t, nil
	}

	var flipTrits bool

	// Check if the MSB is 0, i.e. we have a positive number
	msbM := (unsafe.Sizeof(base[IntLength-1]) * 8) - 1

	switch {
	case base[IntLength-1]>>msbM == 0:
		bigint.MustAdd(base, halfThree)
	default:
		bigint.Not(base)
		if bigint.MustCmp(base, halfThree) == 1 {
			bigint.MustSub(base, halfThree)
			flipTrits = true
		} else {
			bigint.AddSmall(base, 1)
			tmp := make([]uint32, IntLength)
			copy(tmp, halfThree)
			bigint.MustSub(tmp, base)
			copy(base, tmp)
		}
	}

	var rem uint64
	for i := range t[0 : HashTrinarySize-1] {
		rem = 0
		for j := IntLength - 1; j >= 0; j-- {
			lhs := (rem << 32) | uint64(base[j])
			rhs := uint64(TrinaryRadix)
			q := uint32(lhs / rhs)
			r := uint32(lhs % rhs)
			base[j] = q
			rem = uint64(r)
		}
		t[i] = int8(rem) - 1
	}

	if flipTrits {
		for i := range t {
			t[i] = -t[i]
		}
	}

	return t, nil
}

// ReverseTrits reverses the given trits.
func ReverseTrits(trits Trits) Trits {
	for left, right := 0, len(trits)-1; left < right; left, right = left+1, right-1 {
		trits[left], trits[right] = trits[right], trits[left]
	}

	return trits
}

// Trytes is a string of trytes. Use NewTrytes() instead of typecasting.
type Trytes = string

// Hash represents a trinary hash
type Hash = Trytes

// Hashes is a slice of Hash.
type Hashes = []Hash

var trytesRegex = regexp.MustCompile("^[9A-Z]+$")

// ValidTrytes returns true if t is made of valid trytes.
func ValidTrytes(trytes Trytes) error {
	if !trytesRegex.MatchString(string(trytes)) {
		return ErrInvalidTrytes
	}
	return nil

}

// ValidTryte returns the validity of a tryte (must be rune A-Z or 9)
func ValidTryte(t rune) error {
	return ValidTrytes(string(t))
}

// NewTrytes casts to Trytes and checks its validity.
func NewTrytes(s string) (Trytes, error) {
	err := ValidTrytes(s)
	return s, err
}

// TrytesToTrits converts a slice of trytes into trits.
func TrytesToTrits(trytes Trytes) (Trits, error) {
	if err := ValidTrytes(trytes); err != nil {
		return nil, err
	}
	trits := make(Trits, len(trytes)*3)
	for i := range trytes {
		idx := strings.Index(TryteAlphabet, string(trytes[i:i+1]))
		copy(trits[i*3:i*3+3], TryteToTritsLUT[idx])
	}
	return trits, nil
}

// MustTrytesToTrits converts a slice of trytes into trits.
func MustTrytesToTrits(trytes Trytes) Trits {
	trits, err := TrytesToTrits(trytes)
	if err != nil {
		panic(err)
	}
	return trits
}

// Pad pads the given trytes with 9s up to the given size.
func Pad(trytes Trytes, size int) Trytes {
	if len(trytes) >= size {
		return trytes
	}
	out := make([]byte, size)
	copy(out, []byte(trytes))

	for i := len(trytes); i < size; i++ {
		out[i] = '9'
	}
	return Trytes(out)
}

// PadTrits pads the given trits with 0 up to the given size.
func PadTrits(trits Trits, size int) Trits {
	if len(trits) >= size {
		return trits
	}
	sized := make(Trits, size)
	for i := 0; i < size; i++ {
		if len(trits) > i {
			sized[i] = trits[i]
			continue
		}
		sized[i] = 0
	}
	return sized
}

func sum(a int8, b int8) int8 {
	s := a + b

	switch s {
	case 2:
		return -1
	case -2:
		return 1
	default:
		return s
	}
}

func cons(a int8, b int8) int8 {
	if a == b {
		return a
	}

	return 0
}

func any(a int8, b int8) int8 {
	s := a + b

	if s > 0 {
		return 1
	}

	if s < 0 {
		return -1
	}

	return 0
}

func fullAdd(a int8, b int8, c int8) [2]int8 {
	sA := sum(a, b)
	cA := cons(a, b)
	cB := cons(sA, c)
	cOut := any(cA, cB)
	sOut := sum(sA, c)
	return [2]int8{sOut, cOut}
}

// AddTrits adds a to b.
func AddTrits(a Trits, b Trits) Trits {
	maxLen := int64(math.Max(float64(len(a)), float64(len(b))))
	if maxLen == 0 {
		return Trits{0}
	}
	out := make(Trits, maxLen)
	var aI, bI, carry int8

	for i := 0; i < len(out); i++ {
		if i < len(a) {
			aI = a[i]
		} else {
			aI = 0
		}
		if i < len(b) {
			bI = b[i]
		} else {
			bI = 0
		}

		fA := fullAdd(aI, bI, carry)
		out[i] = fA[0]
		carry = fA[1]
	}
	return out
}
