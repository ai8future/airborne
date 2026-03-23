package imagegen

import (
	"os"
	"testing"

	chassis "github.com/ai8future/chassis-go/v10"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(10)
	os.Exit(m.Run())
}
