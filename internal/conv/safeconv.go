package conv

import (
	"fmt"
	"math"
	"strconv"
)

func ParseUint32(s string) (uint32, error) {
	u64, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(u64), nil
}

func ToUint64(i int64) uint64 {
	if i < 0 {
		return 0
	}
	return uint64(i)
}

func ToUint32(i int32) uint32 {
	if i < 0 {
		return 0
	}
	return uint32(i)
}

func Uint64ToInt64(u uint64) (int64, error) {
	if u > math.MaxInt64 {
		return 0, fmt.Errorf("cannot convert uint64(%d) to int64: overflows", u)
	}
	return int64(u), nil
}

func Uint64ToUint32(u uint64) (uint32, error) {
	if u > math.MaxUint32 {
		return 0, fmt.Errorf("cannot convert uint64(%d) to uint32: overflows", u)
	}
	return uint32(u), nil
}

func Uint32ToInt32(u uint32) (int32, error) {
	if u > math.MaxInt32 {
		return 0, fmt.Errorf("cannot convert uint32(%d) to int32: overflows", u)
	}
	return int32(u), nil
}
