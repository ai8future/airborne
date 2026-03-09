package server

import (
	"os"
	"testing"

	chassis "github.com/ai8future/chassis-go/v9"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(9)
	os.Exit(m.Run())
}
