# Private dependency checkout broke release CI

- **Symptom:** Release verification stopped before tests because GitHub Actions could not clone the private `chassis-go-addons` dependency, and dashboard failures lacked durable diagnostics.
- **Root cause:** The workflow used its repository-scoped `GITHUB_TOKEN` for a different private repository and uploaded no dashboard evidence on failure.
- **Fix:** Both jobs now use the read-only `CHASSIS_GO_ADDONS_DEPLOY_KEY` with credential persistence disabled, while dashboard artifacts upload unconditionally.
- **Verification:** YAML and workflow contracts passed local checks, an architect approved the amendment, and an independent reviewer returned CLEAR; GitHub Actions validates the pushed workflow.
