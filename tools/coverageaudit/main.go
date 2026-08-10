// Command coverageaudit turns a Go coverage profile into reviewable evidence.
// It intentionally reports packages with no direct tests instead of treating
// aggregate coverage as proof that every production package is exercised.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type packageInfo struct {
	ImportPath string   `json:"import_path"`
	Directory  string   `json:"directory"`
	Evidence   string   `json:"evidence"`
	TestFiles  []string `json:"test_files"`
}

type inventory struct {
	Packages []packageInfo `json:"packages"`
}

type statementCoverage struct {
	Covered int
	Total   int
}

type coverageSummary struct {
	Aggregate statementCoverage
	Packages  map[string]statementCoverage
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fatal(err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("coverageaudit", flag.ContinueOnError)
	profile := flags.String("profile", "", "coverage profile to evaluate")
	output := flags.String("inventory", "", "write package evidence inventory to this path")
	minimum := flags.Float64("minimum", 75, "minimum filtered statement coverage percentage")
	packageMinimum := flags.Float64("package-minimum", 0, "minimum statement coverage for each substantive package (0 disables)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *output != "" {
		inv, err := collectInventory()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			return err
		}
		data, err := json.MarshalIndent(inv, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*output, append(data, '\n'), 0o644); err != nil {
			return err
		}
		missing := 0
		for _, pkg := range inv.Packages {
			if pkg.Evidence == "missing" {
				missing++
			}
		}
		fmt.Printf("package evidence: %d packages, %d without direct tests\n", len(inv.Packages), missing)
	}

	if *profile == "" {
		return nil
	}
	summary, err := summarizeCoverage(*profile)
	if err != nil {
		return err
	}
	if summary.Aggregate.Total == 0 {
		return fmt.Errorf("coverage profile has no included statements")
	}
	percent := summary.Aggregate.percent()
	fmt.Printf("filtered statement coverage: %.1f%% (%d/%d)\n", percent, summary.Aggregate.Covered, summary.Aggregate.Total)

	var failures []string
	if percent < *minimum {
		failures = append(failures, fmt.Sprintf("filtered statement coverage %.1f%% is below required %.1f%%", percent, *minimum))
	}
	if *packageMinimum > 0 {
		packageFailures, err := packageCoverageFailures(summary.Packages, *packageMinimum)
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			fmt.Printf("substantive package coverage: %d packages checked at %.1f%% minimum\n", len(summary.Packages), *packageMinimum)
			failures = append(failures, packageFailures...)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("coverage requirements failed:\n  - %s", strings.Join(failures, "\n  - "))
	}
	return nil
}

func (coverage statementCoverage) percent() float64 {
	if coverage.Total == 0 {
		return 0
	}
	return float64(coverage.Covered) * 100 / float64(coverage.Total)
}

func packageCoverageFailures(packages map[string]statementCoverage, minimum float64) ([]string, error) {
	if len(packages) == 0 {
		return nil, fmt.Errorf("coverage profile has no substantive packages")
	}
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)

	var failures []string
	for _, name := range names {
		coverage := packages[name]
		if coverage.percent() < minimum {
			failures = append(failures, fmt.Sprintf(
				"package %s coverage %.1f%% (%d/%d) is below required %.1f%%",
				name, coverage.percent(), coverage.Covered, coverage.Total, minimum,
			))
		}
	}
	return failures, nil
}

func collectInventory() (inventory, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	output, err := cmd.Output()
	if err != nil {
		return inventory{}, fmt.Errorf("go list: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var packages []packageInfo
	for decoder.More() {
		var pkg struct {
			ImportPath   string
			Dir          string
			GoFiles      []string
			TestGoFiles  []string
			XTestGoFiles []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			return inventory{}, fmt.Errorf("decode go list: %w", err)
		}
		if len(pkg.GoFiles) == 0 || strings.Contains(pkg.ImportPath, "/vendor/") {
			continue
		}
		tests := append(append([]string{}, pkg.TestGoFiles...), pkg.XTestGoFiles...)
		sort.Strings(tests)
		evidence := "missing"
		if len(tests) > 0 {
			evidence = "direct"
		}
		packages = append(packages, packageInfo{ImportPath: pkg.ImportPath, Directory: filepath.ToSlash(pkg.Dir), Evidence: evidence, TestFiles: tests})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	return inventory{Packages: packages}, nil
}

func filteredStatements(profile string) (covered, total int, err error) {
	summary, err := summarizeCoverage(profile)
	if err != nil {
		return 0, 0, err
	}
	return summary.Aggregate.Covered, summary.Aggregate.Total, nil
}

func summarizeCoverage(profile string) (coverageSummary, error) {
	file, err := os.Open(profile)
	if err != nil {
		return coverageSummary{}, err
	}
	defer file.Close()

	summary := coverageSummary{Packages: make(map[string]statementCoverage)}
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "mode:") {
		return coverageSummary{}, fmt.Errorf("invalid coverage profile %q", profile)
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			continue
		}
		statements, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil {
			return coverageSummary{}, fmt.Errorf("parse statements: %w", parseErr)
		}
		count, parseErr := strconv.Atoi(fields[2])
		if parseErr != nil {
			return coverageSummary{}, fmt.Errorf("parse count: %w", parseErr)
		}
		if !excluded(fields[0]) {
			summary.Aggregate.Total += statements
			if count > 0 {
				summary.Aggregate.Covered += statements
			}
		}
		if packageName, ok := substantivePackage(fields[0]); ok {
			coverage := summary.Packages[packageName]
			coverage.Total += statements
			if count > 0 {
				coverage.Covered += statements
			}
			summary.Packages[packageName] = coverage
		}
	}
	if err := scanner.Err(); err != nil {
		return coverageSummary{}, err
	}
	return summary, nil
}

func excluded(location string) bool {
	path := coverageSourcePath(location)
	return strings.Contains(path, "/gen/go/") || strings.HasSuffix(path, ".pb.go") || strings.Contains(path, "/cmd/") && strings.HasSuffix(path, "/main.go")
}

// substantivePackage applies the narrower per-package policy without changing
// the historical aggregate denominator. Tooling and test helpers remain
// visible in aggregate coverage, but they cannot create release-blocking
// package floors of their own.
func substantivePackage(location string) (string, bool) {
	if excluded(location) {
		return "", false
	}
	path := coverageSourcePath(location)
	if hasPathSegment(path, "tools") || hasPathSegment(path, "testutil") {
		return "", false
	}
	return pathpkg.Dir(path), true
}

func coverageSourcePath(location string) string {
	if separator := strings.LastIndex(location, ":"); separator >= 0 {
		location = location[:separator]
	}
	return strings.ReplaceAll(location, "\\", "/")
}

func hasPathSegment(path, segment string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "coverageaudit:", err)
	os.Exit(1)
}
