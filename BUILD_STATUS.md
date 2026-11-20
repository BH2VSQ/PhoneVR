# Build Status - CI / Release Workflow Update

Base application version: `0.3.8`
Network protocol: `v1`

## Changes

- Fixed the Windows `WM_SIZING` RECT handling so `go vet` no longer reports a possible misuse of `unsafe.Pointer`.
- Replaced separate Android and Windows workflow files with one `.github/workflows/ci.yml` containing parallel Android, Windows Host and release-tooling jobs.
- Added `.github/workflows/release.yml`, triggered only by `workflow_dispatch`.
- Added canonical root `VERSION` file.
- Added `tools/release/bump_version.py` and unit tests. Release bump synchronizes Android `versionName`, increments `versionCode`, updates Android client version, embeds Windows Host version and tags the release.
- Updated the Windows build script to inject the repository version into the GUI and console executables.

## Validation completed in this environment

- `go test -race ./...`: passed (Linux host test suite).
- `go vet ./...`: passed (Linux target).
- `GOOS=windows GOARCH=amd64 go vet ./...`: passed.
- Windows x64 GUI executable cross-build: passed.
- Windows x64 console executable cross-build: passed.
- Release versioning unit tests: 5 passed.
- Both GitHub Actions YAML files parsed successfully.

## Not run here

- Android Gradle build: requires Android SDK 36 and network dependency resolution.
- Windows PowerShell 5.1 / PowerShell 7 execution: no PowerShell runtime in this environment.
- GitHub-hosted Actions and GitHub Release publication: requires pushing this project to a GitHub repository and manually dispatching the workflow.

## Release notes

The manual release workflow publishes an unsigned Android Release APK unless the repository owner adds a signing configuration. Windows release artifacts include the GUI executable, console diagnostics executable and SHA-256 file.
