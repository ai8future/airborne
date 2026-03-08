package extractor

import (
	"os"
	"testing"

	chassis "github.com/ai8future/chassis-go/v8"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(8)
	os.Exit(m.Run())
}
