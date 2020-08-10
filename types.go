package mmdbwriter

import (
	"encoding/binary"
	"io"
	"math/big"
	"math/bits"
	"sort"

	"github.com/pkg/errors"
)

type typeNum byte

const (
	typeNumExtended typeNum = iota
	typeNumPointer          // nolint: deadcode, varcheck
	typeNumString
	typeNumFloat64
	typeNumBytes
	typeNumUint16
	typeNumUint32
	typeNumMap
	typeNumInt32
	typeNumUint64
	typeNumUint128
	typeNumSlice
	// We don't use the next two. They are placeholders. See the spec
	// for more details.
	typeNumContainer // nolint: deadcode, varcheck
	typeNumMarker    // nolint: deadcode, varcheck
	typeNumBool
	typeNumFloat32
)

type writer interface {
	io.Writer
	WriteByte(byte) error
	WriteString(string) (int, error)
}

// DataType represents a MaxMind DB data type
type DataType interface {
	size() int
	typeNum() typeNum
	writeTo(writer) (int64, error)
}

// Bool is the MaxMind DB boolean type
type Bool bool

func (t Bool) size() int {
	if t {
		return 1
	}
	return 0
}

func (t Bool) typeNum() typeNum {
	return typeNumBool
}

func (t Bool) writeTo(w writer) (int64, error) {
	return writeCtrlByte(w, t)
}

// Bytes is the MaxMind DB bytes type
type Bytes []byte

func (t Bytes) size() int {
	return len(t)
}

func (t Bytes) typeNum() typeNum {
	return typeNumBytes
}

func (t Bytes) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	written, err := w.Write([]byte(t))
	numBytes += int64(written)
	if err != nil {
		return numBytes, errors.Wrapf(err, `error writing "%s" as bytes`, t)
	}
	return numBytes, nil
}

// Float32 is the MaxMind DB float type
type Float32 float32

func (t Float32) size() int {
	return 4
}

func (t Float32) typeNum() typeNum {
	return typeNumFloat32
}

func (t Float32) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	err = binary.Write(w, binary.BigEndian, t)
	if err != nil {
		return numBytes, errors.Wrapf(err, "error writing %f as float32", t)
	}
	return numBytes + int64(t.size()), nil
}

// Float64 is the MaxMind DB double type
type Float64 float64

func (t Float64) size() int {
	return 8
}

func (t Float64) typeNum() typeNum {
	return typeNumFloat64
}

func (t Float64) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	err = binary.Write(w, binary.BigEndian, t)
	if err != nil {
		return numBytes, errors.Wrapf(err, "error writing %f as float64", t)
	}
	return numBytes + int64(t.size()), nil
}

// Int32 is the MaxMind DB signed 32-bit integer type
type Int32 int32

func (t Int32) size() int {
	return 4 - bits.LeadingZeros32(uint32(t))/8
}

func (t Int32) typeNum() typeNum {
	return typeNumInt32
}

func (t Int32) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	size := t.size()
	// We ignore leading zeros
	for i := size; i > 0; i-- {
		err = w.WriteByte(byte((int32(t) >> (8 * (i - 1))) & 0xFF))
		if err != nil {
			return numBytes + int64(size-i), errors.Wrap(err, "error writing int32")
		}
	}
	return numBytes + int64(size), nil
}

// Map is the MaxMind DB map type
type Map map[String]DataType

func (t Map) size() int {
	return len(t)
}

func (t Map) typeNum() typeNum {
	return typeNumMap
}

func (t Map) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	// We want database builds to be reproducible. As such, we insert
	// the map items in order by key value. In the future, we will
	// likely use a more relevant characteristic here (e.g., putting
	// fields more likely to be accessed first).
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)

	for _, ks := range keys {
		k := String(ks)
		written, err := k.writeTo(w)
		numBytes += written
		if err != nil {
			return numBytes, err
		}
		written, err = t[k].writeTo(w)
		numBytes += written
		if err != nil {
			return numBytes, err
		}
	}
	return numBytes, nil
}

// Slice is the MaxMind DB array type
type Slice []DataType

func (t Slice) size() int {
	return len(t)
}

func (t Slice) typeNum() typeNum {
	return typeNumSlice
}

func (t Slice) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	for _, e := range t {
		written, err := e.writeTo(w)
		numBytes += written
		if err != nil {
			return numBytes, err
		}
	}
	return numBytes, nil
}

// String is the MaxMind DB string type
type String string

func (t String) size() int {
	return len(t)
}

func (t String) typeNum() typeNum {
	return typeNumString
}

func (t String) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	written, err := w.WriteString(string(t))
	numBytes += int64(written)
	if err != nil {
		return numBytes, errors.Wrapf(err, `error writing "%s" as a string`, t)
	}
	return numBytes, nil
}

// Uint16 is the MaxMind DB unsigned 16-bit integer type
type Uint16 uint16

func (t Uint16) size() int {
	return 2 - bits.LeadingZeros16(uint16(t))/8
}

func (t Uint16) typeNum() typeNum {
	return typeNumUint16
}

func (t Uint16) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	size := t.size()
	// We ignore leading zeros
	for i := size; i > 0; i-- {
		err = w.WriteByte(byte(t >> (8 * (i - 1)) & 0xFF))
		if err != nil {
			return numBytes + int64(size-i), errors.Wrap(err, "error writing uint16")
		}
	}
	return numBytes + int64(size), nil
}

// Uint32 is the MaxMind DB unsigned 32-bit integer type
type Uint32 uint32

func (t Uint32) size() int {
	return 4 - bits.LeadingZeros32(uint32(t))/8
}

func (t Uint32) typeNum() typeNum {
	return typeNumUint32
}

func (t Uint32) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	size := t.size()
	// We ignore leading zeros
	for i := size; i > 0; i-- {
		err = w.WriteByte(byte(t >> (8 * (i - 1)) & 0xFF))
		if err != nil {
			return numBytes + int64(size-i), errors.Wrap(err, "error writing uint32")
		}
	}
	return numBytes + int64(size), nil
}

// Uint64 is the MaxMind DB unsigned 64-bit integer type
type Uint64 uint64

func (t Uint64) size() int {
	return 8 - bits.LeadingZeros64(uint64(t))/8
}

func (t Uint64) typeNum() typeNum {
	return typeNumUint64
}

func (t Uint64) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	size := t.size()

	// We ignore leading zeros
	for i := size; i > 0; i-- {
		err = w.WriteByte(byte(t >> (8 * (i - 1)) & 0xFF))
		if err != nil {
			return numBytes + int64(size-i), errors.Wrap(err, "error writing uint64")
		}
	}
	return numBytes + int64(size), nil
}

// Uint128 is the MaxMind DB unsigned 128-bit integer type
type Uint128 big.Int

func (t *Uint128) size() int {
	// We add 7 here as we want the ceiling of the division operation rather
	// than the floor.
	return ((*big.Int)(t).BitLen() + 7) / 8
}

func (t *Uint128) typeNum() typeNum {
	return typeNumUint128
}

func (t *Uint128) writeTo(w writer) (int64, error) {
	numBytes, err := writeCtrlByte(w, t)
	if err != nil {
		return numBytes, err
	}

	written, err := w.Write((*big.Int)(t).Bytes())
	numBytes += int64(written)
	if err != nil {
		return numBytes, errors.Wrap(err, "error writing uint128")
	}
	return numBytes, nil
}

const (
	firstSize  = 29
	secondSize = firstSize + 256
	thirdSize  = secondSize + (1 << 16)
	maxSize    = thirdSize + (1 << 24)
)

func writeCtrlByte(w writer, t DataType) (int64, error) {
	size := t.size()

	typeNum := t.typeNum()

	var firstByte byte
	var secondByte byte

	if typeNum < 8 {
		firstByte = byte(typeNum << 5)
	} else {
		firstByte = byte(typeNumExtended << 5)
		secondByte = byte(typeNum - 7)
	}

	leftOver := 0
	leftOverSize := 0
	switch {
	case size < firstSize:
		firstByte |= byte(size)
	case size <= secondSize:
		firstByte |= 29
		leftOver = size - firstSize
		leftOverSize = 1
	case size <= thirdSize:
		firstByte |= 30
		leftOver = size - secondSize
		leftOverSize = 2
	case size <= maxSize:
		firstByte |= 31
		leftOver = size - thirdSize
		leftOverSize = 3
	default:
		return 0, errors.Errorf(
			"cannot store %d bytes; max size is %d",
			size,
			maxSize,
		)
	}

	err := w.WriteByte(firstByte)
	if err != nil {
		return 0, errors.Wrapf(
			err,
			"error writing first ctrl byte (type: %d, size: %d)",
			typeNum,
			size,
		)
	}
	numBytes := int64(1)

	if secondByte != 0 {
		err = w.WriteByte(secondByte)
		if err != nil {
			return numBytes, errors.Wrapf(
				err,
				"error writing second ctrl byte (type: %d, size: %d)",
				typeNum,
				size,
			)
		}
		numBytes++
	}

	for i := leftOverSize - 1; i >= 0; i-- {
		v := byte((leftOver >> (8 * i)) & 0xFF)
		err = w.WriteByte(v)
		if err != nil {
			return numBytes, errors.Wrapf(
				err,
				"error writing remaining ctrl bytes (type: %d, size: %d, value: %d)",
				typeNum,
				size,
				v,
			)
		}
		numBytes++
	}
	return numBytes, nil
}