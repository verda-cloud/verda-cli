# Changelog


## [v1.6.1] - 2026-04-10
- [`fac64c7`] Fix/skills install claude code dir structure

## [v1.6.0] - 2026-04-10
- [`820fa11`] Feat: wizard Ctrl+C exit with confirmation

## [v1.5.1] - 2026-04-10
- [`08eae30`] Fix: remove RELEASE_NOTES.md from repo and add to .gitignore
- [`fef8653`] Feat: template improvements — image names, description field, wizard steps
- [`604bc9e`] Fix/template chagne instance type

## [v1.5.0] - 2026-04-09
- [`d6fe448`] Feat/vm batch operation support
- [`79cf7b7`] Feat/verda status dashboard
- [`d2e2338`] Require --all for filter flags (--status, --hostname) in batch operations
- [`72ccba7`] Update license
- [`be03cc3`] Feat/verda template
- [`dfa9753`] Refactor: architecture hardening + template edit command
- [`2b00794`] Feat/ai skills integration
- [`897ef0e`] Fix: commit changelog before tagging to fix goreleaser dirty-state error

## [v1.4.2] - 2026-04-08
- [`0095dee`] Fix: correct homebrew and scoop package name in README
- [`6852d07`] Add auth resolve order explain
- [`2fbf4a0`] Fix/refactor version sub command to --version/-v pararms
- [`e4a5df9`] Image list show image type remove id
- [`2f2d9b7`] Ssh add pipe support
- [`c5f9cdd`] Add more test for auth command

## [v1.4.1] - 2026-04-07
- [`d0a3b8e`] Fix: update binary migration to ~/.verda/bin
- [`bd28c3e`] Fix/vm create related bugs
- [`5e19808`] Fix: use official gitleaks-action to fix 404 on version fetch

## [v1.4.0] - 2026-04-07
- [`a5c57bc`] Feat/agent mode and mcp
- [`15ee7ff`] Fix/update command issue

## [v1.3.2] - 2026-04-05
- [`cfee0cb`] Fix: resolve verify checksum matching and changelog generation issues

## [v1.3.1] - 2026-04-05
- [`2b0f0c0`] Fix/verify version prefix

## [v1.3.0] - 2026-04-05
- [`a533db7`] Fix/update permission
- [`e2960a7`] Feat/add deatils version command

## [v1.2.0] - 2026-04-03
- [`c0b869a`] Feat: add auto-maintained per-subcommand knowledge docs
- [`4c2d084`] Feat/cli improvements

## [v1.1.1] - 2026-04-02
- [`7808962`] Docs: add VHS demo gif and skip CI for doc-only changes
- [`f6217c8`] Docs: reformat README tables and restore demo gif embed
- [`d10e094`] Feat: upgrade verdagostack to v1.1.2, add theme-aware hints and interactive theme selector

## [v1.1.0] - 2026-04-02
- [`d8dbeb4`] Feat: add self-update command, update README with install/update docs
- [`3d3b2c3`] Fix(ci): add gitleaks config to allowlist VHS demo tape files
- [`4dfadfa`] Fix: remove vhs/ from repo, add to gitignore

## [v1.0.0] - 2026-04-02
- [`3ee032a`] Feat(vm): add interactive wizard flow for vm create
- [`99b1ccd`] Feat(auth): add interactive wizard flow for auth configure
- [`87b337c`] Fix(vm): lazily resolve API client in wizard flow
- [`6a01f97`] Fix(auth): improve error message when credentials are missing
- [`91a9855`] Feat(auth): replace auth configure with auth login, add base_url
- [`7bc108d`] Refactor: rename --server flag to --base-url
- [`f36583c`] Refactor(auth): add verda_ prefix to credential file keys
- [`0e928c1`] Fix(vm): check credentials before starting wizard
- [`777256d`] Fix: hide auth and log flags from help output
- [`9cb8382`] Fix: remove global flags section from subcommand help
- [`3f6f78b`] Fix(auth): show base_url in auth show output
- [`7931cff`] Feat(vm): pre-filter instance types by availability, show locations
- [`516e301`] Refactor: adapt to verdagostack wizard engine v2
- [`9090584`] Feat(vm): add spinners to API-loading steps and final submit
- [`5ed5b3a`] Feat: use percentage progress bar in wizard flows
- [`549324e`] Feat(vm): replace JSON output with live instance status view
- [`6d6895b`] Feat(vm): add animated spinner and elapsed timer to status polling
- [`7ee0a01`] Feat(vm): Claude-style animated status line while polling
- [`e774c89`] Feat(vm): add inline SSH key creation sub-flow in wizard
- [`2e657b2`] Feat(vm): add inline startup script creation sub-flow in wizard
- [`5f39096`] Refactor(vm): inline "Add new" option in SSH keys and startup script lists
- [`0ed66e7`] Feat(vm): add "load from file" option for startup script creation
- [`f4b4a16`] Fix(vm): clarify startup script "None" label to "None (skip)"
- [`7c59c57`] Feat(vm): replace simple storage step with full volume sub-flow
- [`5d3d484`] Fix(vm): add back option and navigation hints to storage sub-flows
- [`e78458c`] Fix(vm): handle API errors gracefully in SSH key and startup script creation
- [`32b44fb`] Fix(vm): never crash wizard on sub-flow errors
- [`a1ae326`] Fix(vm): fix SSH keys/storage/scripts lost after Loader, add --debug flag
- [`2511532`] Feat(vm): add vm list command with interactive detail view
- [`da4eb7a`] Fix(vm): simplify vm list to single interactive select
- [`3144970`] Feat(vm): add vm delete command with interactive selection
- [`e6b5db0`] Feat(vm): show storage volumes in instance detail card
- [`a2332ac`] Fix(vm): deduplicate OS volume in storage list, add volume IDs
- [`21a8097`] Fix(vm): render storage as table with header row in detail card
- [`e7d96a0`] Fix(vm): show storage volumes as key-value list matching instance style
- [`f2929e5`] Feat: make --debug a global flag available to all subcommands
- [`eda167a`] Feat(vm): replace vm delete with vm action supporting all operations
- [`da49a2a`] Fix(vm): remove action aliases to keep CLI surface simple
- [`360b246`] Feat(vm): filter actions by instance status, add confirmation messages
- [`513b09d`] Feat(vm): poll instance status after action until expected state
- [`8ca148c`] Fix(vm): poll until expected status after action, not just any terminal
- [`0ff708d`] Fix(vm): add 5-minute timeout to status polling
- [`53705d7`] Feat(vm): add volume selection sub-flow to delete action
- [`0d6425f`] Fix(vm): add red bold warnings to shutdown and force shutdown actions
- [`7eb2e19`] Feat(vm): add deployment summary with cost breakdown before submit
- [`4dde90c`] Fix(vm): fix pricing in deployment summary and instance type list
- [`ab2c493`] Fix(vm): show price per hour in instance type selection list
- [`d1821e1`] Fix(vm): multiply unit price by GPU/CPU count for total hourly price
- [`539c9be`] Fix(vm): always show price for OS and storage volumes in summary
- [`c0d560c`] Feat(vm): show unit price alongside total for volumes in summary
- [`7fde23d`] Feat(vm): show volume prices in type selection and storage step
- [`9d55c6a`] Feat(vm): show unit price breakdown for multi-GPU/CPU instances
- [`26d3b11`] Fix(vm): API price_per_hour is total, not per-GPU — remove double multiply
- [`719936e`] Feat: add ssh-key, startup-script, and volume resource commands
- [`6471851`] Fix: remove key and script aliases for ssh-key and startup-script
- [`f2e4977`] Feat(volume): replace delete with action command supporting all operations
- [`add7f40`] Feat(volume): add volume create command
- [`27b5c64`] Feat(volume): add pricing summary with confirm before creating volume
- [`1f447d3`] Fix(volume): collect user input before spinner in rename/resize/clone
- [`cbd7c06`] Feat(volume): add volume trash command to list deleted volumes
- [`c07e06d`] Fix(volume): improve trash display with type, contract, and pricing
- [`929f248`] Feat(volume): upgrade SDK to v1.4.1, use proper trash API
- [`1d2f7d0`] Feat: upgrade to bubbletea v2, add settings/theme, CI/CD, cross-platform release
- [`8a54571`] Feat: add install script, update README, preload editor template
- [`d992cfd`] Fix: resolve gosec, errcheck, staticcheck, and trivy CI failures
- [`bd4d5fd`] Fix(ci): pin trivy-action to v0.32.0 to avoid Node.js 20 deprecation warning
- [`0517ce9`] Fix(ci): suppress gosec G304 in test file
- [`c7c1955`] Fix(ci): use trivy-action v0.31.0 (v0.32.0 does not exist)
- [`c14b115`] Fix(ci): revert trivy-action to @master (tagged versions not available)
- [`e8ea655`] Fix: resolve all golangci-lint issues and formatting
- [`9eccacf`] Fix: add -short flag to pre-commit unit tests to avoid slow API timeout tests
- [`d4c0b43`] Fix: resolve remaining lint and gosec issues
