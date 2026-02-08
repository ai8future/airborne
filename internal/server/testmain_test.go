package server

import (
	"os"
	"testing"

	chassis "github.com/ai8future/chassis-go/v5"
)

func TestMain(m *testing.M) {
	chassis.RequireMajor(5)
	os.Exit(m.Run())
}
