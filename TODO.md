# TODO

## GFS
- File should not appear in namespace listing until fully committed (all chunks committed)
- Checksum verification for chunks
- Automated recovery when checksum fails
- Storage corruption failure counter

## Compute / Gateway
- Automatic TLS certificate provisioning for containers with HTTPS enabled
  - Options: wildcard cert for *.edd.io, per-container ACME, or cert-manager integration
  - Currently containers must manage their own certificates
- Distributed gateway (multiple gateway instances for high availability)
- Bring your own domain (custom domain mapping for containers)
