# Downstream module graph required a non-transitive replace

Airborne v1.10.10 required `github.com/ai8future/markdown_svc/clients/go v0.0.0` and repaired it locally with a `replace` to the bundled client. Go does not apply dependency replacements in consuming modules, so a fresh downstream `go list -m all` failed on the nonexistent version.

The bundled client now uses its repository-qualified module identity. Airborne v1.10.11 directly requires the published nested-module revision and has no root replacement directives. Release verification creates a clean consumer with no replacements and imports the public generated API before listing, building, and testing the graph.
