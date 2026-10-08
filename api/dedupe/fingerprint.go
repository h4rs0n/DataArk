// Package dedupe implements versioned, deterministic lexical fingerprints.
// Similarity retrieval is a candidate generator, never proof of semantic equality.
package dedupe

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
	"math"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const Algorithm = "minhash-rune5-128-b16r8-v1"
const Dimensions = 128
const Bands = 16
const MinimumCharacters = 200
const SimilarityFloor = 0.90

func Normalize(body string) string { return norm.NFC.String(strings.Join(strings.Fields(body), " ")) }
func Length(body string) int       { return len([]rune(strings.ReplaceAll(Normalize(body), " ", ""))) }
func Shingles(body string) map[string]struct{} {
	runes := []rune(Normalize(body))
	result := make(map[string]struct{})
	for i := 0; i+5 <= len(runes); i++ {
		result[string(runes[i:i+5])] = struct{}{}
	}
	return result
}

// Fixed SplitMix64 seeds and little-endian encoding are part of Algorithm.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
func Signature(body string) []byte {
	minima := [Dimensions]uint64{}
	for i := range minima {
		minima[i] = math.MaxUint64
	}
	for shingle := range Shingles(body) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(shingle))
		base := h.Sum64()
		for i := range minima {
			value := mix(base ^ mix(uint64(i)))
			if value < minima[i] {
				minima[i] = value
			}
		}
	}
	result := make([]byte, Dimensions*8)
	for i, value := range minima {
		binary.LittleEndian.PutUint64(result[i*8:], value)
	}
	return result
}
func Buckets(signature []byte) []string {
	if len(signature) != Dimensions*8 {
		return nil
	}
	result := make([]string, Bands)
	for i := range result {
		sum := sha256.Sum256(signature[i*64 : (i+1)*64])
		result[i] = hex.EncodeToString(sum[:16])
	}
	return result
}
func Similarity(a, b string) float64 {
	first, second := Shingles(a), Shingles(b)
	if len(first) == 0 || len(second) == 0 {
		return 0
	}
	intersection := 0
	for shingle := range first {
		if _, ok := second[shingle]; ok {
			intersection++
		}
	}
	return float64(intersection) / float64(len(first)+len(second)-intersection)
}

var numericToken = regexp.MustCompile(`[-+]?\p{Nd}+(?:[.,]\p{Nd}+)*(?:[eE][-+]?\p{Nd}+)?`)

func numbers(body string) string {
	return strings.Join(numericToken.FindAllString(Normalize(body), -1), "|")
}

func Verify(a, b string) (float64, bool) {
	a, b = Normalize(a), Normalize(b)
	if a == b && a != "" {
		return 1, true
	}
	la, lb := Length(a), Length(b)
	if la < MinimumCharacters || lb < MinimumCharacters {
		return 0, false
	}
	if la > lb {
		la, lb = lb, la
	}
	if float64(la)/float64(lb) < SimilarityFloor || numbers(a) != numbers(b) {
		return 0, false
	}
	score := Similarity(a, b)
	return score, score >= SimilarityFloor
}

func NumbersHash(body string) string {
	sum := sha256.Sum256([]byte(numbers(body)))
	return hex.EncodeToString(sum[:])
}
