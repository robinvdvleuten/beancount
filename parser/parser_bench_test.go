package parser

import (
	"os"
	"testing"
)

func BenchmarkParseKitchensink(b *testing.B) {
	data, err := os.ReadFile("../testdata/kitchensink.beancount")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseBytes(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseExample(b *testing.B) {
	data, err := os.ReadFile("../testdata/example.beancount")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseBytes(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
