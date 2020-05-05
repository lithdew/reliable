package main

import "math"

var (
	emptyBufferIndexCache [math.MaxUint16]uint32
)

func init() {
	emptyBufferIndexCache[0] = math.MaxUint32
	for i := 1; i < math.MaxUint16; i *= 2 {
		copy(emptyBufferIndexCache[i:], emptyBufferIndexCache[:i])
	}
}

func emptyBufferIndices(indices []uint32) {
	copy(indices[:], emptyBufferIndexCache[:len(indices)])
}
