# GitHub Rulesets

These JSON files define the repository rulesets for vibez. They can be imported
via the GitHub UI without needing admin API access.

## How to apply

1. Go to **Settings → Rules → Rulesets**
2. Click **New ruleset → Import ruleset**
3. Upload the JSON file
4. Review and click **Create**

Repeat for each file.

---

## `protect-main.json` — Main branch protection

Targets `refs/heads/main`.

| Rule | Value |
|------|-------|
| Deletion | ❌ blocked |
| Force push | ❌ blocked |
| Required reviews | 1 approver, stale reviews dismissed on push |
| Review thread resolution | required before merge |
| Required status checks | `test` (CI workflow job) |
| Linear history | required (no merge commits) |

> **bypass_actors**: Repository role "Admin" (`actor_id: 5`) can bypass all
> rules. Verify or adjust this in the UI after import — the actor ID may differ
> for organisation-level roles.

---

## `protect-tags.json` — Release tag protection

Targets `refs/tags/v*` (all semver release tags).

| Rule | Value |
|------|-------|
| Deletion | ❌ blocked |
| Tag update | ❌ blocked (non-fast-forward) |
| Creation | restricted to bypass actors only |

Release tags are immutable once pushed. Only repo admins can create or move
`v*` tags, which prevents accidental or malicious tag rewrites that would
corrupt the published release artefacts.

> **bypass_actors**: same Admin role as above — the GoReleaser CI job pushes
> tags during release, so ensure the `GITHUB_TOKEN` used there has the
> necessary permission (the release workflow already has `contents: write`).
