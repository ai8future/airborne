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

func main() {
	profile := flag.String("profile", "", "coverage profile to evaluate")
	output := flag.String("inventory", "", "write package evidence inventory to this path")
	minimum := flag.Float64("minimum", 75, "minimum filtered statement coverage percentage")
	flag.Parse()

	if *output != "" {
		inv, err := collectInventory()
		if err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			fatal(err)
		}
		data, err := json.MarshalIndent(inv, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*output, append(data, '\n'), 0o644); err != nil {
			fatal(err)
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
		return
	}
	covered, total, err := filteredStatements(*profile)
	if err != nil {
		fatal(err)
	}
	if total == 0 {
		fatal(fmt.Errorf("coverage profile has no included statements"))
	}
	percent := float64(covered) * 100 / float64(total)
	fmt.Printf("filtered statement coverage: %.1f%% (%d/%d)\n", percent, covered, total)
	if percent < *minimum {
		fatal(fmt.Errorf("filtered statement coverage %.1f%% is below required %.1f%%", percent, *minimum))
	}
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
	file, err := os.Open(profile)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "mode:") {
		return 0, 0, fmt.Errorf("invalid coverage profile %q", profile)
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || excluded(fields[0]) {
			continue
		}
		statements, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil {
			return 0, 0, fmt.Errorf("parse statements: %w", parseErr)
		}
		count, parseErr := strconv.Atoi(fields[2])
		if parseErr != nil {
			return 0, 0, fmt.Errorf("parse count: %w", parseErr)
		}
		total += statements
		if count > 0 {
			covered += statements
		}
	}
	return covered, total, scanner.Err()
}

func excluded(location string) bool {
	path := strings.ReplaceAll(strings.SplitN(location, ":", 2)[0], "\\", "/")
	return strings.Contains(path, "/gen/go/") || strings.HasSuffix(path, ".pb.go") || strings.Contains(path, "/cmd/") && strings.HasSuffix(path, "/main.go")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "coverageaudit:", err)
	os.Exit(1)
}
