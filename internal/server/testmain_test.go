package server

import (
	"os"
	"testing"

	chassis "github.com/ai8future/chassis-go/v6"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(6)
	os.Exit(m.Run())
}
