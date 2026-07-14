package decalgo

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestChatMessagePackingRoundTrip(t *testing.T) {
	for _, original := range [][]byte{
		{},
		[]byte("i think i might've fucked up a bit too hard"),
		{0, 1, 2, 3, 254, 255},
	} {
		packed, err := packMessage(original)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := unpackMessage(packed)
		if err != nil || !bytes.Equal(decoded, original) {
			t.Fatalf("round trip %q: %q, %v", original, decoded, err)
		}
	}
}

func TestCommonChatMessageCompresses(t *testing.T) {
	original := []byte("i think i might've fucked up a bit too hard")
	packed, err := packMessage(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(packed) >= len(original)/2 {
		t.Fatalf("packed message is still too large: %d bytes from %d", len(packed), len(original))
	}
}

func TestCommonExactMessageUsesPhraseProtocol(t *testing.T) {
	original := []byte("meet me after lunch")
	packed, err := packMessage(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(packed) != 1 || int(packed[0]) < messagePhraseBase {
		t.Fatalf("got %d-byte mode-%d representation %x; want one phrase-mode byte", len(packed), packed[0], packed)
	}
	decoded, err := unpackMessage(packed)
	if err != nil || !bytes.Equal(decoded, original) {
		t.Fatalf("got %q, %v", decoded, err)
	}
}

func TestShortChatUsesCompactFragmentProtocol(t *testing.T) {
	original := []byte("meet me after lunch today")
	packed, err := packMessage(original)
	if err != nil {
		t.Fatal(err)
	}
	if packed[0] < messageCompactBase || packed[0] >= messageCompactBase+messageCompactModes {
		t.Fatalf("got mode %d; want compact fragments", packed[0])
	}
	decoded, err := unpackMessage(packed)
	if err != nil || !bytes.Equal(decoded, original) {
		t.Fatalf("got %q, %v", decoded, err)
	}
}

func TestMessagePackingRandomBinaryRoundTrips(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for size := 0; size <= 4096; size += 17 {
		original := make([]byte, size)
		if _, err := rng.Read(original); err != nil {
			t.Fatal(err)
		}
		packed, err := packMessage(original)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		decoded, err := unpackMessage(packed)
		if err != nil || !bytes.Equal(decoded, original) {
			t.Fatalf("size %d round trip failed: %v", size, err)
		}
	}
}

func TestFragmentDecoderRejectsMalformedInput(t *testing.T) {
	for _, packed := range [][]byte{
		{messageFragments, 255},
		{messageFragments, fragmentLiteralBase},
		{messageCompactBase + 2, 0xfc},
		{messageCompactBase + 2, 0xc0},
		{messageCompactBase + 1, 0x01},
		{messageDenseBase + 3, 0xf8},
		{messageDenseBase + 3, 0x80},
		{messageDenseBase + 1, 0x01},
	} {
		if _, err := unpackMessage(packed); err == nil {
			t.Fatalf("malformed fragment stream accepted: %x", packed)
		}
	}
}

func TestDenseFragmentsWinForCommonConnectiveText(t *testing.T) {
	original := []byte("i that it is in the and it is for me")
	packed, err := packMessage(original)
	if err != nil {
		t.Fatal(err)
	}
	if packed[0] < messageDenseBase || packed[0] >= messageDenseBase+messageDenseModes {
		t.Fatalf("dense mode did not win: mode=%d size=%d", packed[0], len(packed))
	}
	t.Logf("common connective text: %d bytes raw, %d bytes dense", len(original), len(packed))
	decoded, err := unpackMessage(packed)
	if err != nil || !bytes.Equal(decoded, original) {
		t.Fatalf("got %q, %v", decoded, err)
	}
}

func TestDenseFragmentsRandomBinaryRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for size := 0; size <= 256; size++ {
		original := make([]byte, size)
		if _, err := rng.Read(original); err != nil {
			t.Fatal(err)
		}
		packed := packDenseFragments(original)
		decoded, err := unpackMessage(packed)
		if err != nil || !bytes.Equal(decoded, original) {
			t.Fatalf("size %d round trip failed: %v", size, err)
		}
	}
}

func TestCompactFragmentsRandomBinaryRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for size := 0; size <= 256; size++ {
		original := make([]byte, size)
		if _, err := rng.Read(original); err != nil {
			t.Fatal(err)
		}
		packed := packCompactFragments(original)
		decoded, err := unpackMessage(packed)
		if err != nil || !bytes.Equal(decoded, original) {
			t.Fatalf("size %d round trip failed: %v", size, err)
		}
	}
}

func TestDynamicConversationDictionary(t *testing.T) {
	dictionary := []byte("The blue ceramic bicycle is waiting beside the northern greenhouse at Riverside Botanical Garden.")
	original := []byte("the northern greenhouse at Riverside Botanical Garden")
	packed, err := packMessageWithDictionary(original, dictionary)
	if err != nil {
		t.Fatal(err)
	}
	if packed[0] != messageDynamic || len(packed) >= len(original)/2 {
		t.Fatalf("dynamic dictionary was not effective: mode=%d size=%d original=%d", packed[0], len(packed), len(original))
	}
	t.Logf("repeated-topic text: %d bytes raw, %d bytes with synchronized context", len(original), len(packed))
	decoded, err := unpackMessageWithDictionary(packed, dictionary)
	if err != nil || !bytes.Equal(decoded, original) {
		t.Fatalf("got %q, %v", decoded, err)
	}
	if _, err := unpackMessage(packed); err == nil {
		t.Fatal("dynamic stream decoded without its synchronized dictionary")
	}
	if _, err := unpackMessageWithDictionary(packed, []byte("wrong conversation")); err == nil {
		t.Fatal("dynamic stream decoded with the wrong conversation dictionary")
	}
}

func TestEveryAuthenticatedPhraseRoundTripsWithoutBody(t *testing.T) {
	seen := make(map[string]struct{}, len(commonMessages))
	for index, phrase := range commonMessages {
		if _, duplicate := seen[phrase]; duplicate {
			t.Fatalf("duplicate phrasebook entry %q", phrase)
		}
		seen[phrase] = struct{}{}
		packed, err := packMessage([]byte(phrase))
		if err != nil {
			t.Fatal(err)
		}
		wantMode := byte(messagePhraseBase + index)
		if len(packed) != 1 || packed[0] != wantMode {
			t.Fatalf("phrase %q packed as %x; want mode %d only", phrase, packed, wantMode)
		}
		decoded, err := unpackMessage(packed)
		if err != nil || string(decoded) != phrase {
			t.Fatalf("phrase %q decoded as %q, %v", phrase, decoded, err)
		}
	}
	if messagePhraseBase+len(commonMessages) > 256 {
		t.Fatal("phrasebook exceeds one-byte authenticated mode space")
	}
}

func TestPhraseModeRejectsUnexpectedBody(t *testing.T) {
	if _, err := unpackMessage([]byte{messagePhraseBase, 0}); err == nil {
		t.Fatal("phrase mode accepted unauthenticated body data")
	}
}
