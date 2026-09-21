package similarity

import (
	"encoding/binary"
	"math"
	"strconv"

	"github.com/ccsrvs/codetwin/internal/paircache"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// pairScoreCacheSchema must change whenever exactPairScore semantics or the
// document-key inputs change. Candidate retrieval changes do not require a
// bump: missing current candidates are recomputed and stale ones are ignored.
const pairScoreCacheSchema = "pair-score-v1"

type scoreDigest = paircache.DocumentKey

func pairScoreCacheContext(minConfLines int) string {
	return pairScoreCacheSchema + ";min-confidence=" + strconv.Itoa(minConfLines)
}

func snippetScoreDigest(snippet scan.Snippet, vector NormalizedVector) scoreDigest {
	builder := newScoreDigestBuilder()
	builder.addString(snippet.Path)
	builder.addString(snippet.CacheKey)
	builder.addUint64(uint64(snippet.StartLine))
	builder.addUint64(uint64(snippet.EndLine))
	builder.addUint64(uint64(snippet.NonBlankLn))
	builder.addString(string(snippet.Lang))
	builder.addString(string(snippet.Kind))
	builder.addUint64(uint64(snippet.Fps.K))
	var fingerprintSum, fingerprintXOR uint64
	for value := range snippet.Fps.Set {
		mixed := uint64(value) * 0x9e3779b185ebca87
		fingerprintSum += mixed
		fingerprintXOR ^= mixed + 0x517cc1b727220a95
	}
	builder.addUint64(uint64(len(snippet.Fps.Set)))
	builder.addUint64(fingerprintSum)
	builder.addUint64(fingerprintXOR)
	builder.addUint64(uint64(len(vector.Keys)))
	for _, term := range vector.Keys {
		if snippet.CacheKey == "" {
			builder.addString(term)
		}
		builder.addUint64(math.Float64bits(vector.V[term]))
	}
	builder.addUint64(uint64(len(snippet.LexTerms)))
	for _, term := range snippet.LexTerms {
		builder.addString(term)
	}
	return builder.digest()
}

// scoreDigestBuilder is a deterministic allocation-free 128-bit mixer. The
// final pair key is still SHA-256; two independent lanes make accidental
// endpoint-digest collisions vanishingly unlikely without allocating a full
// serialized vector on every warm run.
type scoreDigestBuilder struct {
	lanes [2]uint64
}

func newScoreDigestBuilder() scoreDigestBuilder {
	return scoreDigestBuilder{lanes: [2]uint64{
		0xcbf29ce484222325,
		0x9e3779b97f4a7c15,
	}}
}

func (builder *scoreDigestBuilder) addByte(value byte) {
	primes := [2]uint64{0x100000001b3, 0x100000001e7}
	for i := range builder.lanes {
		builder.lanes[i] ^= uint64(value) + uint64(i)*0x9e
		builder.lanes[i] *= primes[i]
	}
}

func (builder *scoreDigestBuilder) addUint64(value uint64) {
	for i := range builder.lanes {
		builder.lanes[i] ^= value + uint64(i)*0x9e3779b97f4a7c15
		builder.lanes[i] = (builder.lanes[i] << 27) | (builder.lanes[i] >> 37)
		builder.lanes[i] *= 0x94d049bb133111eb
	}
}

func (builder *scoreDigestBuilder) addString(value string) {
	builder.addUint64(uint64(len(value)))
	for i := 0; i < len(value); i++ {
		builder.addByte(value[i])
	}
}

func (builder *scoreDigestBuilder) digest() scoreDigest {
	var digest scoreDigest
	for i, lane := range builder.lanes {
		binary.LittleEndian.PutUint64(digest[i*8:(i+1)*8], lane)
	}
	return digest
}
