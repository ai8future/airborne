package vectorstore

import (
	"os"
	"testing"

	chassis "github.com/ai8future/chassis-go"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(4)
	os.Exit(m.Run())
}
