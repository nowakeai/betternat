# BetterNAT Release Notes

Release notes in this directory are user-facing runtime/main-repository release
notes. Group files by minor version directory and name each file by the exact
Git tag:

```text
v0.1/v0.1.0-alpha.8.md
```

Rules:

- every published runtime/main-repository tag must have a release note here
  before publication,
- release notes must be grouped by minor version, for example `v0.1/`, `v0.2/`,
  and `v1.0/`,
- the matching GitHub Release body should be copied from the checked-in release
  note,
- provider-only release notes live in the split Terraform provider repository
  under `docs/release-notes/`,
- release notes must state status, changes, validation, known limitations,
  upgrade or compatibility notes, and artifact integrity guidance.

Current releases:

- [v0.1.0-alpha.8](v0.1/v0.1.0-alpha.8.md)
- [v0.1.0-alpha.7](v0.1/v0.1.0-alpha.7.md)
- [v0.1.0-alpha.6](v0.1/v0.1.0-alpha.6.md)
- [v0.1.0-alpha.2](v0.1/v0.1.0-alpha.2.md)
- [v0.1.0-alpha.1](v0.1/v0.1.0-alpha.1.md)
