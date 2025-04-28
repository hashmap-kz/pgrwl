package conv

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseUint32(t *testing.T) {
	t.Run("valid uint32", func(t *testing.T) {
		val, err := ParseUint32("4294967295") // max uint32
		assert.NoError(t, err)
		assert.Equal(t, uint32(4294967295), val)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := ParseUint32("abc")
		assert.Error(t, err)
	})

	t.Run("overflow uint32", func(t *testing.T) {
		_, err := ParseUint32("4294967296") // 1 more than max uint32
		assert.Error(t, err)
	})
}

func TestToUint64(t *testing.T) {
	t.Run("positive int64", func(t *testing.T) {
		val := ToUint64(12345)
		assert.Equal(t, uint64(12345), val)
	})

	t.Run("zero int64", func(t *testing.T) {
		val := ToUint64(0)
		assert.Equal(t, uint64(0), val)
	})

	t.Run("negative int64", func(t *testing.T) {
		val := ToUint64(-12345)
		assert.Equal(t, uint64(0), val)
	})
}

func TestUint64ToInt64(t *testing.T) {
	t.Run("valid uint64", func(t *testing.T) {
		val, err := Uint64ToInt64(uint64(math.MaxInt64))
		assert.NoError(t, err)
		assert.Equal(t, int64(math.MaxInt64), val)
	})

	t.Run("overflow uint64", func(t *testing.T) {
		_, err := Uint64ToInt64(uint64(math.MaxInt64) + 1)
		assert.Error(t, err)
	})
}

func TestUint32ToInt32(t *testing.T) {
	t.Run("valid uint32", func(t *testing.T) {
		val, err := Uint32ToInt32(uint32(math.MaxInt32))
		assert.NoError(t, err)
		assert.Equal(t, int32(math.MaxInt32), val)
	})

	t.Run("overflow uint32", func(t *testing.T) {
		_, err := Uint32ToInt32(uint32(math.MaxInt32) + 1)
		assert.Error(t, err)
	})
}
