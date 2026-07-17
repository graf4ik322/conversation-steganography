package conversationstenography

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"sync"
)

// This dictionary is protocol state shared by both peers. It targets ordinary
// private-chat language; DEFLATE uses it without placing it in the carrier.
var chatDictionary = []byte(`
the of and to a in is it you that for on with this i was are be as have at or
not but we your from can all my me they so if about just what there one like
do don't did didn't i'm i've i'll i'd you're you've you'll you'd we're we've
we'll we'd they're they've they'll they'd it's that's there's what's who's
how's can't couldn't wouldn't shouldn't won't isn't wasn't weren't haven't
hasn't had been really think know want wanted need needed feel felt might maybe
probably actually honestly seriously sorry thanks please okay ok yeah yes no
hey hi hello bye later today tonight tomorrow yesterday morning afternoon
evening day week weekend home work school class dinner lunch breakfast coffee
food pizza movie game going coming got get go come see talk tell said say
something anything everything nothing good great fine bad hard little bit much
too very more less up down out over back around here there now then when where
why how who which because though still already again never always sometimes
someone anyone everyone friend friends guys dude bro love hate miss hope worry
wrong right sure thing stuff time way make made take took give gave let keep
i think i might have i might've i think i've i feel like i don't know
i fucked up i think i fucked up i might have fucked up a bit too hard
i think i might've fucked up a bit too hard i fucked up really hard today
what happened are you okay do you want to talk about it call me when you can
let me know talk to you later see you soon sounds good that makes sense
`)

const maxDecompressedMessage = 16 << 20

const (
	messageRaw          = 0
	messageDeflate      = 1
	messageFragments    = 2
	messageCompactBase  = 3
	messageCompactModes = 8
	messageDynamic      = 11
	messageDenseBase    = 12
	messageDenseModes   = 8
	messagePhraseBase   = 20
	fragmentLiteralBase = 240
	fragmentLiteralMax  = 15
	compactLiteralBase  = 48
	compactLiteralMax   = 15
)

var compactFragments = []string{
	"meet", " me", " after", " lunch", "I ", "I'm", " i ", " you",
	" the", " to", " a", " and", " it", " that", " is", " of",
	" in", " for", " on", " with", " this", " was", " are", " have",
	" not", " but", " we", " your", " my", " so", " just", " about",
	" like", " do", " did", " really", " think", " know", " want", " need",
	" feel", " maybe", " today", " tonight", " tomorrow", " home", " work", " school",
}

var denseFragments = []string{
	"i", "I", " you", " the", " to", " a", " and", " it",
	" that", " is", " of", " in", " for", " on", " with", " me",
}

var commonMessages = []string{
	"yes", "no", "ok", "okay", "thanks", "thank you", "you're welcome", "sorry",
	"sounds good", "that makes sense", "on my way", "be there soon", "i'm here", "i'm home",
	"call me", "call me when you can", "text me", "let me know", "good morning", "good night",
	"see you soon", "see you later", "talk to you later", "i love you", "love you", "miss you",
	"are you okay?", "what happened?", "where are you?", "how are you?", "when are you coming?",
	"what are you doing?", "are you home?", "did you eat?", "want to call?", "can we talk?",
	"meet me after lunch", "meet me after work", "see you after work", "i'm running late",
	"i'll call you later", "i'll text you later", "i made it", "i'm almost there", "have fun",
	"take care", "safe travels", "happy birthday!",
}

var fragmentProtocol = []string{
	"i think", "i don't know", "let me know", "do you want", "what do you", "how are you",
	"talk to you later", "see you soon", "sounds good", "that makes sense", "thank you", "thanks",
	"good morning", "good night", "right now", "a little", "a bit", "going to", "want to", "have to",
	"need to", "got to", "did you", "are you", "can you", "would you", "could you", "i'm", "i've",
	"i'll", "i'd", "you're", "you've", "you'll", "we're", "we've", "we'll", "they're", "it's",
	"that's", "there's", "what's", "don't", "didn't", "can't", "couldn't", "wouldn't", "shouldn't",
	"won't", "isn't", "wasn't", "haven't", "hasn't", "hello", "hey", "yeah", "okay", "please",
	"sorry", "really", "think", "know", "want", "need", "feel", "maybe", "probably", "actually",
	"today", "tonight", "tomorrow", "yesterday", "morning", "afternoon", "evening", "weekend",
	"home", "work", "school", "class", "dinner", "lunch", "breakfast", "coffee", "movie", "game",
	"going", "coming", "meet", "after", "before", "later", "here", "there", "when", "where", "because",
	"about", "with", "this", "that", "from", "your", "have", "just", "what", "like", "good", "great",
	"fine", "hard", "much", "very", "more", "back", "time", "me", "love", "miss", "hope",
}

var (
	chatFragments    []string
	fragmentsByFirst [256][]int
	fragmentOnce     sync.Once
)

func initFragments() {
	seen := make(map[string]struct{}, len(fragmentProtocol)*2)
	for _, fragment := range fragmentProtocol {
		for _, candidate := range []string{fragment, " " + fragment} {
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			chatFragments = append(chatFragments, candidate)
		}
	}
	if len(chatFragments) > fragmentLiteralBase {
		panic("chat fragment protocol exceeds one-byte dictionary")
	}
	for index, fragment := range chatFragments {
		fragmentsByFirst[fragment[0]] = append(fragmentsByFirst[fragment[0]], index)
	}
}

func packMessage(plaintext []byte) ([]byte, error) {
	return packMessageWithDictionary(plaintext, nil)
}

func packMessageWithDictionary(plaintext, dynamicDictionary []byte) ([]byte, error) {
	for index, phrase := range commonMessages {
		if bytes.Equal(plaintext, []byte(phrase)) {
			return []byte{byte(messagePhraseBase + index)}, nil
		}
	}
	var compressed bytes.Buffer
	w, err := flate.NewWriterDict(&compressed, flate.BestCompression, chatDictionary)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	best := append([]byte{messageRaw}, plaintext...)
	if compressed.Len()+1 < len(best) {
		best = append([]byte{messageDeflate}, compressed.Bytes()...)
	}
	if len(dynamicDictionary) > 0 {
		var dynamic bytes.Buffer
		w, err := flate.NewWriterDict(&dynamic, flate.BestCompression, dynamicDictionary)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(plaintext); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		if dynamic.Len()+1 < len(best) {
			best = append([]byte{messageDynamic}, dynamic.Bytes()...)
		}
	}
	fragmented := packFragments(plaintext)
	if len(fragmented) < len(best) {
		best = fragmented
	}
	compact := packCompactFragments(plaintext)
	if len(compact) < len(best) {
		best = compact
	}
	dense := packDenseFragments(plaintext)
	if len(dense) < len(best) {
		best = dense
	}
	return best, nil
}

func packMessageDetached(plaintext, dynamicDictionary []byte) (byte, []byte, error) {
	packed, err := packMessageWithDictionary(plaintext, dynamicDictionary)
	if err != nil {
		return 0, nil, err
	}
	return packed[0], append([]byte(nil), packed[1:]...), nil
}

func packingModes() []byte {
	modes := make([]byte, 0, messageCompactModes+4)
	modes = append(modes, messageRaw, messageDeflate, messageFragments)
	for mode := messageCompactBase; mode < messageCompactBase+messageCompactModes; mode++ {
		modes = append(modes, byte(mode))
	}
	modes = append(modes, messageDynamic)
	for mode := messageDenseBase; mode < messageDenseBase+messageDenseModes; mode++ {
		modes = append(modes, byte(mode))
	}
	for index := range commonMessages {
		modes = append(modes, byte(messagePhraseBase+index))
	}
	return modes
}

func unpackMessage(packed []byte) ([]byte, error) {
	return unpackMessageWithDictionary(packed, nil)
}

func unpackMessageWithDictionary(packed, dynamicDictionary []byte) ([]byte, error) {
	if len(packed) == 0 {
		return nil, errors.New("empty packed message")
	}
	if packed[0] == messageRaw {
		return append([]byte(nil), packed[1:]...), nil
	}
	if packed[0] == messageFragments {
		return unpackFragments(packed[1:])
	}
	if packed[0] >= messageCompactBase && packed[0] < messageCompactBase+messageCompactModes {
		return unpackCompactFragments(packed[0], packed[1:])
	}
	if packed[0] == messageDynamic {
		if len(dynamicDictionary) == 0 {
			return nil, errors.New("dynamic chat dictionary is unavailable")
		}
		return inflateMessage(packed[1:], dynamicDictionary)
	}
	if packed[0] >= messageDenseBase && packed[0] < messageDenseBase+messageDenseModes {
		return unpackDenseFragments(packed[0], packed[1:])
	}
	if int(packed[0]) >= messagePhraseBase && int(packed[0]) < messagePhraseBase+len(commonMessages) {
		if len(packed) != 1 {
			return nil, errors.New("authenticated phrase mode contains unexpected body bytes")
		}
		return []byte(commonMessages[int(packed[0])-messagePhraseBase]), nil
	}
	if packed[0] != messageDeflate {
		// Compatibility with records created before message packing existed.
		return append([]byte(nil), packed...), nil
	}
	return inflateMessage(packed[1:], chatDictionary)
}

func inflateMessage(compressed, dictionary []byte) ([]byte, error) {
	r := flate.NewReaderDict(bytes.NewReader(compressed), dictionary)
	defer r.Close()
	decoded, err := io.ReadAll(io.LimitReader(r, maxDecompressedMessage+1))
	if err != nil {
		return nil, fmt.Errorf("decompress message: %w", err)
	}
	if len(decoded) > maxDecompressedMessage {
		return nil, errors.New("decompressed message exceeds size limit")
	}
	return decoded, nil
}

func longestCompactFragment(src []byte) (index, length int) {
	index = -1
	for candidate, fragment := range compactFragments {
		if len(fragment) > length && len(fragment) <= len(src) && bytes.Equal(src[:len(fragment)], []byte(fragment)) {
			index, length = candidate, len(fragment)
		}
	}
	return index, length
}

func appendPackedBits(dst []byte, offset, count, value int) ([]byte, int) {
	for bit := count - 1; bit >= 0; bit-- {
		dst = appendBit(dst, offset, (value>>uint(bit))&1)
		offset++
	}
	return dst, offset
}

func packCompactFragments(plaintext []byte) []byte {
	bits := make([]byte, 0, len(plaintext))
	bitOffset := 0
	for offset := 0; offset < len(plaintext); {
		if index, length := longestCompactFragment(plaintext[offset:]); index >= 0 {
			bits, bitOffset = appendPackedBits(bits, bitOffset, 6, index)
			offset += length
			continue
		}
		start := offset
		for offset < len(plaintext) && offset-start < compactLiteralMax {
			offset++
			if index, _ := longestCompactFragment(plaintext[offset:]); index >= 0 {
				break
			}
		}
		length := offset - start
		bits, bitOffset = appendPackedBits(bits, bitOffset, 6, compactLiteralBase+length-1)
		for _, literal := range plaintext[start:offset] {
			bits, bitOffset = appendPackedBits(bits, bitOffset, 8, int(literal))
		}
	}
	padding := (8 - bitOffset%8) % 8
	return append([]byte{byte(messageCompactBase + padding)}, bits...)
}

func unpackCompactFragments(mode byte, encoded []byte) ([]byte, error) {
	padding := int(mode) - messageCompactBase
	if padding < 0 || padding >= messageCompactModes || padding > len(encoded)*8 {
		return nil, errors.New("invalid compact fragment padding")
	}
	bitLimit := len(encoded)*8 - padding
	for bit := bitLimit; bit < len(encoded)*8; bit++ {
		if readBits(encoded, bit, 1) != 0 {
			return nil, errors.New("non-zero compact fragment padding")
		}
	}
	out := make([]byte, 0, len(encoded)*2)
	for bitOffset := 0; bitOffset < bitLimit; {
		if bitLimit-bitOffset < 6 {
			return nil, errors.New("truncated compact fragment symbol")
		}
		code := readBits(encoded, bitOffset, 6)
		bitOffset += 6
		if code < len(compactFragments) {
			fragment := compactFragments[code]
			if len(out)+len(fragment) > maxDecompressedMessage {
				return nil, errors.New("compact fragment-decoded message exceeds size limit")
			}
			out = append(out, fragment...)
			continue
		}
		if code < compactLiteralBase || code >= compactLiteralBase+compactLiteralMax {
			return nil, errors.New("invalid compact fragment code")
		}
		length := code - compactLiteralBase + 1
		if bitLimit-bitOffset < length*8 {
			return nil, errors.New("truncated compact fragment literal")
		}
		if len(out)+length > maxDecompressedMessage {
			return nil, errors.New("compact fragment-decoded message exceeds size limit")
		}
		for range length {
			out = append(out, byte(readBits(encoded, bitOffset, 8)))
			bitOffset += 8
		}
	}
	return out, nil
}

func longestDenseFragment(src []byte) (index, length int) {
	index = -1
	for candidate, fragment := range denseFragments {
		if len(fragment) > length && len(fragment) <= len(src) && bytes.Equal(src[:len(fragment)], []byte(fragment)) {
			index, length = candidate, len(fragment)
		}
	}
	return index, length
}

func packDenseFragments(plaintext []byte) []byte {
	bits := make([]byte, 0, len(plaintext))
	bitOffset := 0
	for offset := 0; offset < len(plaintext); {
		if index, length := longestDenseFragment(plaintext[offset:]); index >= 0 {
			bits, bitOffset = appendPackedBits(bits, bitOffset, 5, index)
			offset += length
			continue
		}
		start := offset
		for offset < len(plaintext) && offset-start < compactLiteralMax {
			offset++
			if index, _ := longestDenseFragment(plaintext[offset:]); index >= 0 {
				break
			}
		}
		length := offset - start
		bits, bitOffset = appendPackedBits(bits, bitOffset, 5, 16+length-1)
		for _, literal := range plaintext[start:offset] {
			bits, bitOffset = appendPackedBits(bits, bitOffset, 8, int(literal))
		}
	}
	padding := (8 - bitOffset%8) % 8
	return append([]byte{byte(messageDenseBase + padding)}, bits...)
}

func unpackDenseFragments(mode byte, encoded []byte) ([]byte, error) {
	padding := int(mode) - messageDenseBase
	if padding < 0 || padding >= messageDenseModes || padding > len(encoded)*8 {
		return nil, errors.New("invalid dense fragment padding")
	}
	bitLimit := len(encoded)*8 - padding
	for bit := bitLimit; bit < len(encoded)*8; bit++ {
		if readBits(encoded, bit, 1) != 0 {
			return nil, errors.New("non-zero dense fragment padding")
		}
	}
	out := make([]byte, 0, len(encoded)*2)
	for bitOffset := 0; bitOffset < bitLimit; {
		if bitLimit-bitOffset < 5 {
			return nil, errors.New("truncated dense fragment symbol")
		}
		code := readBits(encoded, bitOffset, 5)
		bitOffset += 5
		if code < len(denseFragments) {
			fragment := denseFragments[code]
			if len(out)+len(fragment) > maxDecompressedMessage {
				return nil, errors.New("dense fragment-decoded message exceeds size limit")
			}
			out = append(out, fragment...)
			continue
		}
		if code < 16 || code >= 16+compactLiteralMax {
			return nil, errors.New("invalid dense fragment code")
		}
		length := code - 16 + 1
		if bitLimit-bitOffset < length*8 {
			return nil, errors.New("truncated dense fragment literal")
		}
		if len(out)+length > maxDecompressedMessage {
			return nil, errors.New("dense fragment-decoded message exceeds size limit")
		}
		for range length {
			out = append(out, byte(readBits(encoded, bitOffset, 8)))
			bitOffset += 8
		}
	}
	return out, nil
}

func longestFragment(src []byte) (index, length int) {
	fragmentOnce.Do(initFragments)
	if len(src) == 0 {
		return -1, 0
	}
	index = -1
	for _, candidate := range fragmentsByFirst[src[0]] {
		fragment := chatFragments[candidate]
		if len(fragment) > length && len(fragment) <= len(src) && bytes.Equal(src[:len(fragment)], []byte(fragment)) {
			index, length = candidate, len(fragment)
		}
	}
	return index, length
}

func packFragments(plaintext []byte) []byte {
	out := make([]byte, 1, len(plaintext)+1)
	out[0] = messageFragments
	for offset := 0; offset < len(plaintext); {
		if index, length := longestFragment(plaintext[offset:]); index >= 0 {
			out = append(out, byte(index))
			offset += length
			continue
		}
		start := offset
		for offset < len(plaintext) && offset-start < fragmentLiteralMax {
			offset++
			if index, _ := longestFragment(plaintext[offset:]); index >= 0 {
				break
			}
		}
		length := offset - start
		out = append(out, byte(fragmentLiteralBase+length-1))
		out = append(out, plaintext[start:offset]...)
	}
	return out
}

func unpackFragments(encoded []byte) ([]byte, error) {
	fragmentOnce.Do(initFragments)
	out := make([]byte, 0, len(encoded)*2)
	for offset := 0; offset < len(encoded); {
		code := int(encoded[offset])
		offset++
		if code < len(chatFragments) {
			fragment := chatFragments[code]
			if len(out)+len(fragment) > maxDecompressedMessage {
				return nil, errors.New("fragment-decoded message exceeds size limit")
			}
			out = append(out, fragment...)
			continue
		}
		if code < fragmentLiteralBase || code >= fragmentLiteralBase+fragmentLiteralMax {
			return nil, errors.New("invalid chat fragment code")
		}
		length := code - fragmentLiteralBase + 1
		if length > len(encoded)-offset {
			return nil, errors.New("truncated chat fragment literal")
		}
		if len(out)+length > maxDecompressedMessage {
			return nil, errors.New("fragment-decoded message exceeds size limit")
		}
		out = append(out, encoded[offset:offset+length]...)
		offset += length
	}
	return out, nil
}
