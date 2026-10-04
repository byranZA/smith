# Release notes

One pair of files per release, written by the `/release` skill before the tag is cut:

- `<version>.md` — the release notes: what changed for someone upgrading from the previous stable release, and what they must do. They head the GitHub release body, above the generated commit changelog.
- `<version>-audit.md` — the docs audit: every PR since the previous stable release, traced to the doc section that describes it, or marked internal.

Release candidates share their target version's files.
