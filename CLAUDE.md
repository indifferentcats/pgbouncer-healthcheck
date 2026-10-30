# Claude Code instructions for this repository

## Attribution (required)

Credit every contribution to the agent **and** the specific model that made
it. Use the model you are actually running as, not a value copied from
elsewhere. These rules override the default attribution and the
`attribution` setting.

**Commits**: end every commit message with both trailers:

```
Co-Authored-By: <model display name> <noreply@anthropic.com>
Assisted-by: Claude Code (<model ID>)
```

For example, `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
`Assisted-by: Claude Code (claude-opus-5-5)`.

- `Co-Authored-By` makes GitHub credit Claude as a co-author.
- `Assisted-by` records the agent and the exact model in a form scripts can
  read.
- Use the canonical model ID (e.g. `claude-opus-5-5`), without provider
  prefixes such as `us.anthropic.`.
- When subagents or several models contributed to a commit, add one
  `Assisted-by` line per model.

**Pull requests and releases**: end the description with:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code) using <model display name> (`<model ID>`)
```

**Issues and review comments you write** get the same line.

Never remove or rewrite the attribution on existing commits.

See CONTRIBUTING.md for the policy and how to find AI-assisted commits.
