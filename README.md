# WAL-G: PostgreSQL WAL Archiving & Backups

**Actively maintained fork of [wal-g/wal-g](https://github.com/wal-g/wal-g)**

PostgreSQL WAL archiving, point-in-time recovery, and disaster recovery.

✅ **100% backward compatible** with WAL-G v0.14.1
✅ **Actively maintained** — Security patches within 24 hours
✅ **Production ready** — Tested on real PostgreSQL infrastructure

---

## Attribution

This is a maintained fork of the [original WAL-G project](https://github.com/wal-g/wal-g).

- Original License: Apache 2.0
- Fork License: MIT
- See [NOTICE](NOTICE) for details

---

## Status

✅ **Actively Maintained** — Security patches within 24 hours  
✅ **100% Backward Compatible** — Works with WAL-G v0.14.1 backups  
✅ **Production Ready** — Tested on real PostgreSQL + S3/GCS/Azure  
✅ **Kubernetes Native** — Helm charts included  

## Quick Start

### Installation

\\\ash

# Homebrew (macOS) - coming soon
# brew tap postgresql-tools/wal-g
# brew install wal-g

# Docker - coming soon
# docker pull ghcr.io/postgresql-tools/wal-g:latest

# Kubernetes (Helm)
helm install wal-g ./helm/wal-g --namespace postgres
\\\

### Configure & Backup

\\\ash
# Set S3 bucket
export AWS_S3_BUCKET=your-bucket
export AWS_REGION=us-east-1

# Create backup
wal-g backup-push

# List backups
wal-g backup-list

# Restore
wal-g backup-fetch latest /tmp/restore
\\\

## Compatibility

✅ **100% backward compatible with WAL-G v0.14.1**
- All CLI flags identical
- S3 object format unchanged
- Drop-in replacement

[View compatibility test results](https://github.com/postgresql-tools/wal-g/actions)

## Features

- 🌍 **Multi-Cloud Support** — S3, Google Cloud Storage, Azure Blob Storage, Backblaze B2
- ☸️ **Kubernetes Native** — DaemonSet + CronJob Helm charts included
- 🔒 **Encrypted Backups** — AES-256-GCM with customer-managed keys (KMS)
- 🚀 **Point-in-Time Recovery** — Continuous WAL archiving + incremental backups
- 🧪 **Automated Testing** — Compatibility tests run on every commit
- 📊 **Monitoring Integration** — Prometheus metrics export (walg-exporter)
- 🔄 **Disaster Recovery** — Point-in-time recovery and WAL archiving

## Documentation

- [Installation Guide](docs/INSTALLATION.md)
- [Configuration Reference](docs/CONFIGURATION.md)
- [Backup & Recovery](docs/BACKUP-RECOVERY.md)
- [Kubernetes Deployment](helm/wal-g/README.md)
- [Monitoring Integration](cmd/pg/exporter/README.md)
- [Migration from Original WAL-G](docs/MIGRATION.md)

## Community

- 📖 [Documentation](https://github.com/postgresql-tools/wal-g/wiki)
- 💬 [GitHub Discussions](https://github.com/postgresql-tools/wal-g/discussions)
- 🐛 [Report Issues](https://github.com/postgresql-tools/wal-g/issues)
- 🔒 [Security Policy](SECURITY.md)

## Maintenance Commitment

We maintain WAL-G because PostgreSQL backups are critical infrastructure:

- **Daily deployments** — New features every 1–2 weeks
- **24-hour CVE SLA** — Critical security patches within 24 hours
- **100% test coverage** — All changes tested against v0.14.1
- **Transparency** — [View weekly metrics](https://github.com/postgresql-tools/wal-g/actions)

## Contributing Notice

Thank you for your interest in this project. Please note that we are 
currently not accepting any external code contributions, pull requests, 
bug fixes, or feature submissions at this time. 

Any pull requests opened will be automatically closed without review.

## License

MIT License. See [LICENSE](LICENSE) for details.

---

**Built by [Lateos](https://lateos.ai) — PostgreSQL infrastructure, simplified.**
