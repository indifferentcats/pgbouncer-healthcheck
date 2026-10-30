# Contributing

## Workflow

- Branch from `master` and open a pull request. `master` is protected:
  merging needs the code owner's approval and passing CI (build and
  integration tests on amd64 and arm64, unit tests, CodeQL, security scans).
- Use [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `ci:`, `docs:`, …). Mark breaking changes with `!` and
  a `BREAKING CHANGE:` footer, and record them in `CHANGELOG.md`.
- Pin new dependencies to immutable hashes: GitHub Actions by commit SHA,
  container images by digest, downloaded tools by SHA-256. See the README's
  "Supply-chain pinning" section.

## AI-assisted contributions

Contributions made with an AI agent must say which agent and which model
made them, so reviewers know what they are looking at and the history stays
auditable.

- **Commits** carry both trailers:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Assisted-by: Claude Code (claude-opus-5-5)
  ```
  `Co-Authored-By` gives GitHub co-author credit. `Assisted-by` names the
  agent and the exact model ID. Use one `Assisted-by` line per model if
  several were involved.
- **Pull requests** end with
  `🤖 Generated with Claude Code using <model> (<model ID>)`.
- A person still reviews and approves every AI-assisted change. Dependabot
  and AI-authored PRs are never auto-merged.

Claude Code follows these rules automatically through [`CLAUDE.md`](CLAUDE.md).
For other agents, add the equivalent trailers yourself.

To list AI-assisted commits and the models that made them:

```sh
git log --format='%h %s%n  %(trailers:key=Assisted-by,valueonly,separator=%x2C )' \
  | grep -B1 '^  .'
```

Commits from before the `Assisted-by` trailer only have `Co-Authored-By`:

```sh
git log --format='%h %(trailers:key=Co-Authored-By,valueonly,separator=%x2C ) %s' \
  | grep -i 'noreply@anthropic.com'
```
