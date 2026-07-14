package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilteredStatements(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	contents := strings.Join([]string{
		"mode: set",
		"example.com/app/a.go:1.1,2.1 2 1",
		"example.com/app/b.go:1.1,2.1 3 0",
		"example.com/app/gen/go/ignored.go:1.1,2.1 9 1",
		"example.com/app/cmd/tool/main.go:1.1,2.1 9 1",
	}, "\n")
	if err := os.WriteFile(profile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	covered, total, err := filteredStatements(profile)
	if err != nil {
		t.Fatal(err)
	}
	if covered != 2 || total != 5 {
		t.Fatalf("filteredStatements() = %d/%d, want 2/5", covered, total)
	}
}

func TestFilteredStatementsRejectsMalformedProfiles(t *testing.T) {
	for _, content := range []string{"", "mode: set\nfile.go:1.1,2.1 bad 1", "mode: set\nfile.go:1.1,2.1 1 bad"} {
		path := filepath.Join(t.TempDir(), "coverage.out")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := filteredStatements(path); err == nil {
			t.Fatalf("filteredStatements(%q) unexpectedly succeeded", content)
		}
	}
}

func TestRun(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(profile, []byte("mode: set\nfile.go:1.1,2.1 2 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-profile", profile, "-minimum", "100"}); err != nil {
		t.Fatalf("run() = %v", err)
	}
	if err := run([]string{"-profile", profile, "-minimum", "101"}); err == nil {
		t.Fatal("run() with excessive minimum unexpectedly succeeded")
	}
	if err := run([]string{"-unknown"}); err == nil {
		t.Fatal("run() with unknown flag unexpectedly succeeded")
	}
}

func TestRunRejectsLowPackageWhenAggregatePasses(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	contents := strings.Join([]string{
		"mode: set",
		"example.com/app/high/high.go:1.1,2.1 99 1",
		"example.com/app/low/low.go:1.1,2.1 1 0",
	}, "\n")
	if err := os.WriteFile(profile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"-profile", profile, "-minimum", "75", "-package-minimum", "60"})
	if err == nil {
		t.Fatal("run() unexpectedly accepted a zero-coverage package behind a 99% aggregate")
	}
	for _, want := range []string{"example.com/app/low", "0.0%", "0/1", "60.0%"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("run() error %q does not contain actionable detail %q", err, want)
		}
	}
}

func TestSummarizeCoverageGroupsFilesByPackage(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	contents := strings.Join([]string{
		"mode: atomic",
		"example.com/app/pkg/a.go:1.1,2.1 3 1",
		"example.com/app/pkg/b.go:1.1,2.1 2 0",
		"C:\\repo\\other\\a.go:1.1,2.1 4 1",
	}, "\n")
	if err := os.WriteFile(profile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := summarizeCoverage(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary.Packages["example.com/app/pkg"]; got != (statementCoverage{Covered: 3, Total: 5}) {
		t.Fatalf("package summary = %+v, want 3/5", got)
	}
	if got := summary.Packages["C:/repo/other"]; got != (statementCoverage{Covered: 4, Total: 4}) {
		t.Fatalf("Windows package summary = %+v, want 4/4", got)
	}
}

func TestSubstantivePackagesExcludePolicyOnlySources(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	contents := strings.Join([]string{
		"mode: set",
		"example.com/app/internal/service/service.go:1.1,2.1 6 1",
		"example.com/app/gen/go/api.pb.go:1.1,2.1 10 0",
		"example.com/app/cmd/server/main.go:1.1,2.1 10 0",
		"example.com/app/tools/coverageaudit/main.go:1.1,2.1 10 0",
		"example.com/app/internal/rag/testutil/helper.go:1.1,2.1 10 0",
	}, "\n")
	if err := os.WriteFile(profile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := summarizeCoverage(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Packages) != 1 {
		t.Fatalf("substantive packages = %#v, want only internal/service", summary.Packages)
	}
	if _, ok := summary.Packages["example.com/app/internal/service"]; !ok {
		t.Fatalf("substantive packages = %#v, missing internal/service", summary.Packages)
	}
	// Aggregate filtering intentionally retains its prior semantics: tools and
	// test support remain in the aggregate denominator while generated code and
	// cmd entrypoints remain excluded.
	if summary.Aggregate != (statementCoverage{Covered: 6, Total: 26}) {
		t.Fatalf("aggregate = %+v, want 6/26", summary.Aggregate)
	}
}

func TestPackageMinimumRejectsProfilesWithoutSubstantiveCode(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	contents := "mode: set\nexample.com/app/tools/helper.go:1.1,2.1 3 1\n"
	if err := os.WriteFile(profile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"-profile", profile, "-minimum", "0", "-package-minimum", "60"})
	if err == nil || !strings.Contains(err.Error(), "no substantive packages") {
		t.Fatalf("run() error = %v, want no substantive packages", err)
	}
}

func TestRunAcceptsCoveredNestedClientAndExcludesGeneratedProtobuf(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "coverage.out")
	contents := strings.Join([]string{
		"mode: atomic",
		"github.com/ai8future/markdown_svc/clients/go/client.go:1.1,2.1 6 1",
		"github.com/ai8future/markdown_svc/clients/go/client.go:3.1,4.1 4 0",
		"github.com/ai8future/markdown_svc/clients/go/markdownsvc/v1/markdown.pb.go:1.1,2.1 100 0",
		"github.com/ai8future/markdown_svc/clients/go/markdownsvc/v1/markdown_grpc.pb.go:1.1,2.1 100 0",
	}, "\n")
	if err := os.WriteFile(profile, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-profile", profile, "-minimum", "60", "-package-minimum", "60"}); err != nil {
		t.Fatalf("run() rejected a 60%% nested client because of generated protobuf: %v", err)
	}
}

func TestExcluded(t *testing.T) {
	for _, tc := range []struct {
		location string
		want     bool
	}{
		{"example.com/app/gen/go/api.pb.go:1.1,2.1", true},
		{"example.com/app/message.pb.go:1.1,2.1", true},
		{"example.com/app/cmd/tool/main.go:1.1,2.1", true},
		{"C:\\repo\\app\\cmd\\tool\\main.go:1.1,2.1", true},
		{"example.com/app/internal/main.go:1.1,2.1", false},
		{"example.com/app/cmd/tool/worker.go:1.1,2.1", false},
	} {
		if got := excluded(tc.location); got != tc.want {
			t.Errorf("excluded(%q) = %v, want %v", tc.location, got, tc.want)
		}
	}
}
