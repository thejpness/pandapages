package gutenberg

import (
	"errors"
	"testing"

	"pandapages/api/internal/sourceprovider"
)

func TestNormalisePlainTextConservativelyExtractsGutenbergBody(t *testing.T) {
	input := "Provider metadata\r\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\r\n\r\nCONTENTS\r\nChapter I  1\r\n\r\nCHAPTER I\r\n‘Punctuation’,  repeated  whitespace.\r\n\r\n[1] A footnote.\r\n\r\nAn introduction inside the body.\r\n\r\n*** END OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\r\nProvider license\r\n"
	got, err := normalisePlainText([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := "CONTENTS\nChapter I  1\n\nCHAPTER I\n‘Punctuation’,  repeated  whitespace.\n\n[1] A footnote.\n\nAn introduction inside the body.\n"
	if got != want {
		t.Fatalf("normalised=%q want=%q", got, want)
	}
}

func TestNormalisePlainTextSupportsBOMAndThisMarker(t *testing.T) {
	input := []byte("\xef\xbb\xbf*** START OF THIS PROJECT GUTENBERG EBOOK EXAMPLE ***\rBody\r*** END OF THIS PROJECT GUTENBERG EBOOK EXAMPLE ***")
	got, err := normalisePlainText(input)
	if err != nil || got != "Body\n" {
		t.Fatalf("normalised/error=%q/%v", got, err)
	}
}

func TestNormalisePlainTextFailsClosedForInvalidBoundaries(t *testing.T) {
	validStart := "*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***"
	validEnd := "*** END OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***"
	for _, test := range []struct {
		name  string
		input []byte
		want  error
	}{
		{"missing start", []byte("Body\n" + validEnd), sourceprovider.ErrNormalisationFailed},
		{"missing end", []byte(validStart + "\nBody"), sourceprovider.ErrNormalisationFailed},
		{"reversed", []byte(validEnd + "\nBody\n" + validStart), sourceprovider.ErrNormalisationFailed},
		{"ambiguous start", []byte(validStart + "\n" + validStart + "\nBody\n" + validEnd), sourceprovider.ErrNormalisationFailed},
		{"empty body", []byte(validStart + "\n \n" + validEnd), sourceprovider.ErrNormalisationFailed},
		{"invalid utf8", []byte{0xff}, sourceprovider.ErrContentInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalisePlainText(test.input); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestNormalisationHashesDistinguishRetrievedBytes(t *testing.T) {
	unix := []byte("metadata\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nBody\n*** END OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nlicense\n")
	windows := []byte("metadata\r\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\r\nBody\r\n*** END OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\r\nlicense\r\n")
	normalisedUnix, err := normalisePlainText(unix)
	if err != nil {
		t.Fatal(err)
	}
	normalisedWindows, err := normalisePlainText(windows)
	if err != nil {
		t.Fatal(err)
	}
	if normalisedUnix != normalisedWindows || sha256Hex(unix) == sha256Hex(windows) || sha256HexString(normalisedUnix) != sha256HexString(normalisedWindows) {
		t.Fatalf("hashes/normalisation=%q/%q", normalisedUnix, normalisedWindows)
	}
}
