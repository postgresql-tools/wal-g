// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

# Versioning

## Scheme

This project uses **calver-style pre-1.0 numbering**: `v0.MICRO[-SUFFIX]`.

Current series: **v0.15.x** (forked at upstream v0.14.x).

The major version component (`0`) signals that the public API is not yet stable.
Once a release reaches `v1.0.0`, the major version will increment and breaking
changes will follow [semver](https://semver.org/) rules.

### Relationship to upstream WAL-G

Upstream WAL-G (`wal-g/wal-g`) has diverged to **v3.x**. This fork's numbers are
**not upstream's numbers**. The fork was created from upstream commit
`7e9f9055` (tagged as upstream `v0.14.1`) and continued the upstream numbering
to avoid confusion with existing deployments that reference specific versions.

| Fork tag | Upstream equivalent | Notes |
|---|---|---|
| v0.14.1 | v0.14.1 | Fork point — identical codebase |
| v0.14.2-lts.1 | (none) | LTS patch on fork |
| v0.14.3-stable | (none) | Stability fixes on fork |
| v0.15.0-stable | v2.x / v3.x | Fork-added features; unrelated to upstream v2/v3 |
| v0.15.1 | v3.0.8 | |
| v0.15.2 | v3.0.8 | Adds `compliance-report`; fixes `backup-verify` exit code. Latest fork release |

**Important:** do not assume this fork's `v0.15.x` is compatible with upstream
`v3.x`. They share a common ancestor at v0.14.1 but have diverged significantly
since. The Go module path is also different:

- Upstream: `github.com/wal-g/wal-g`
- This fork: `github.com/lateos-ai/wal-g`

## Tag format

Release tags follow this pattern:

```
v0.MICRO[-SUFFIX]
```

Where:
- `MICRO` increments with each release (14 → 15 → ...)
- Optional `-SUFFIX` indicates special status:
  - `-stable` — production-ready release
  - `-lts.N` — long-term support patch
  - No suffix — development or interim release

## Tooling

- **GitHub Releases**: built from `v*` tags via `.github/workflows/release.yml`
- **Go modules**: use the full module path `github.com/lateos-ai/wal-g` — no
  `/v2` suffix needed because this is a fork with its own module path
- **Homebrew formula**: not yet published (Roadmap item)
- **Docker images**: not yet published (Roadmap item)
- **Helm chart**: not yet published (Roadmap item)
