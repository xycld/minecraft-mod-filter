package inspect

// CurseForgeFingerprint computes CurseForge's file fingerprint: 32-bit
// MurmurHash2 (seed 1) over the file with tab, LF, CR and space bytes removed.
func CurseForgeFingerprint(data []byte) uint32 {
	const m = 0x5bd1e995
	n := 0
	for _, b := range data {
		if !isFingerprintWhitespace(b) {
			n++
		}
	}
	h := uint32(1) ^ uint32(n)
	var k uint32
	shift := 0
	for _, b := range data {
		if isFingerprintWhitespace(b) {
			continue
		}
		k |= uint32(b) << shift
		shift += 8
		if shift == 32 {
			k *= m
			k ^= k >> 24
			k *= m
			h = h*m ^ k
			k, shift = 0, 0
		}
	}
	if shift > 0 {
		h ^= k
		h *= m
	}
	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return h
}

func isFingerprintWhitespace(b byte) bool { return b == 9 || b == 10 || b == 13 || b == 32 }
