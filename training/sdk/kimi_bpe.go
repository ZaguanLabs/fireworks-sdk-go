package sdk

import (
	"bytes"
	"container/heap"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// KimiTokenizer is a data-only reader for reviewed Kimi vocabularies.
// It is safe for concurrent encoding and decoding after construction.
type KimiTokenizer struct {
	profile    string
	ranks      map[string]int
	tokens     []string
	special    *regexp.Regexp
	registered *regexp.Regexp
	rawFiles   map[string][]byte
}

func (k *KimiTokenizer) Profile() string     { return k.profile }
func (k *KimiTokenizer) VocabularySize() int { return len(k.tokens) }

func (k *KimiTokenizer) Decode(ids []int) (string, error) {
	var buf bytes.Buffer
	for _, id := range ids {
		if id < 0 || id >= len(k.tokens) {
			return "", fmt.Errorf("invalid Kimi token ID %d", id)
		}
		buf.WriteString(k.tokens[id])
	}
	// Match Python's UTF-8 replacement behavior (one replacement per
	// invalid sequence prefix, including a truncated terminal sequence).
	b := buf.Bytes()
	if utf8.Valid(b) {
		return string(b), nil
	}
	return decodeKimiUTF8(b), nil
}

func decodeKimiUTF8(data []byte) string {
	var out strings.Builder
	for len(data) > 0 {
		r, n := utf8.DecodeRune(data)
		if r == utf8.RuneError && n == 1 && data[0] >= 0xc2 && data[0] <= 0xf4 {
			limit := 2
			switch {
			case data[0] >= 0xf0:
				limit = 4
			case data[0] >= 0xe0:
				limit = 3
			}
			for n < len(data) && n < limit && data[n]&0xc0 == 0x80 {
				if n == 1 && (data[0] == 0xe0 && data[1] < 0xa0 || data[0] == 0xed && data[1] >= 0xa0 || data[0] == 0xf0 && data[1] < 0x90 || data[0] == 0xf4 && data[1] >= 0x90) {
					break
				}
				n++
			}
		}
		out.WriteRune(r)
		data = data[n:]
	}
	return out.String()
}

// Go's RE2 lacks class subtraction. Construct the reviewed Unicode ranges
// once, excluding Han from both letter classes, as the release tokenizer does.
func kimiLetterClass(caseTable *unicode.RangeTable) string {
	var out strings.Builder
	out.WriteByte('[')
	start := -1
	for r := rune(0); r <= unicode.MaxRune+1; r++ {
		ok := r <= unicode.MaxRune && !unicode.Is(unicode.Han, r) && unicode.In(r, caseTable, unicode.Lm, unicode.Lo, unicode.M)
		if caseTable == unicode.Lu && unicode.Is(unicode.Lt, r) {
			ok = true
		}
		if ok && start < 0 {
			start = int(r)
		}
		if !ok && start >= 0 {
			fmt.Fprintf(&out, "\\x{%x}", start)
			if int(r)-1 != start {
				fmt.Fprintf(&out, "-\\x{%x}", r-1)
			}
			start = -1
		}
	}
	out.WriteByte(']')
	return out.String()
}

var kimiPieces = sync.OnceValue(func() *regexp.Regexp {
	upper, lower := kimiLetterClass(unicode.Lu), kimiLetterClass(unicode.Ll)
	prefix := `[^\r\n\pL\pN]?`
	suffix := `(?i:'s|'t|'re|'ve|'m|'ll|'d)?`
	space := `\pZ\x09-\x0D\x85`
	return regexp.MustCompile(`^(?:\p{Han}+|` + prefix + upper + "*" + lower + "+" + suffix + "|" + prefix + upper + "+" + lower + "*" + suffix + `|\pN{1,3}| ?[^` + space + `\pL\pN]+[\r\n]*|[` + space + `]*[\r\n]+|[` + space + `]+)`)
})

func kimiChunkSpace(r rune) bool {
	return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f // Python str.isspace()
}

// Encode applies the pinned chunk-first Kimi tiktoken contract.
func (k *KimiTokenizer) Encode(text string, allowSpecial bool) []int {
	ids := []int{}
	runes := []rune(text)
	for len(runes) > 0 {
		n := min(len(runes), 400000)
		block := runes[:n]
		runes = runes[n:]
		start, run := 0, 0
		space := kimiChunkSpace(block[0])
		for i, r := range block {
			now := kimiChunkSpace(r)
			if now != space {
				run = 0
				space = now
			}
			run++
			if run > 25000 {
				ids = k.encodeChunk(ids, string(block[start:i]), allowSpecial)
				start, run = i, 1
			}
		}
		ids = k.encodeChunk(ids, string(block[start:]), allowSpecial)
	}
	return ids
}

func (k *KimiTokenizer) encodeChunk(ids []int, text string, allowSpecial bool) []int {
	if allowSpecial {
		for {
			p := k.special.FindStringIndex(text)
			if p == nil {
				break
			}
			ids = k.encodeOrdinary(ids, text[:p[0]])
			ids = append(ids, k.ranks[text[p[0]:p[1]]])
			text = text[p[1]:]
		}
	}
	return k.encodeOrdinary(ids, text)
}

func (k *KimiTokenizer) encodeOrdinary(ids []int, text string) []int {
	re := kimiPieces()
	for len(text) > 0 {
		match := re.FindStringIndex(text)
		if match == nil {
			panic("Kimi pretokenizer did not cover input")
		}
		end := match[1]
		// The final whitespace alternatives are greedy except that one space
		// before non-whitespace is available to the next token's prefix.
		if end < len(text) {
			last, size := utf8.DecodeLastRuneInString(text[:end])
			if end > size && unicode.IsSpace(last) && last != '\r' && last != '\n' {
				end -= size
			}
		}
		ids = k.encodePiece(ids, text[:end])
		text = text[end:]
	}
	return ids
}

type kimiPair struct{ left, right, end, rank int }
type kimiPairs []kimiPair

func (p kimiPairs) Len() int { return len(p) }
func (p kimiPairs) Less(i, j int) bool {
	if p[i].rank == p[j].rank {
		return p[i].left < p[j].left
	}
	return p[i].rank < p[j].rank
}
func (p kimiPairs) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p *kimiPairs) Push(v any)   { *p = append(*p, v.(kimiPair)) }
func (p *kimiPairs) Pop() any     { q := *p; v := q[len(q)-1]; *p = q[:len(q)-1]; return v }

// The leftmost pair wins ties. Stale pairs no longer span two live tokens.
func (k *KimiTokenizer) encodePiece(ids []int, piece string) []int {
	if rank, ok := k.ranks[piece]; ok && rank < kimiBaseVocabSize {
		return append(ids, rank)
	}
	n := len(piece)
	next, prev := make([]int, n+1), make([]int, n+1)
	for i := 0; i <= n; i++ {
		next[i] = i + 1
		prev[i] = i - 1
	}
	var pairs kimiPairs
	schedule := func(i int) {
		if i < 0 || i >= n || next[i] >= n {
			return
		}
		j := next[i]
		end := next[j]
		if rank, ok := k.ranks[piece[i:end]]; ok && rank < kimiBaseVocabSize {
			heap.Push(&pairs, kimiPair{i, j, end, rank})
		}
	}
	for i := 0; i < n-1; i++ {
		schedule(i)
	}
	for len(pairs) > 0 {
		p := heap.Pop(&pairs).(kimiPair)
		if next[p.left] != p.right || next[p.right] != p.end {
			continue
		}
		next[p.left], prev[p.end], next[p.right] = p.end, p.left, -1
		schedule(prev[p.left])
		schedule(p.left)
	}
	for i := 0; i < n; i = next[i] {
		ids = append(ids, k.ranks[piece[i:next[i]]])
	}
	return ids
}

func kimiSpecialPattern(names []string) *regexp.Regexp {
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for i, name := range names {
		names[i] = regexp.QuoteMeta(name)
	}
	return regexp.MustCompile(strings.Join(names, "|"))
}
