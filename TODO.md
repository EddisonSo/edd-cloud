# TODO

## GFS
- File should not appear in namespace listing until fully committed (all chunks committed)

## Compute / Gateway
- Automatic TLS certificate provisioning for containers with HTTPS enabled
  - Options: wildcard cert for *.edd.io, per-container ACME, or cert-manager integration
  - Currently containers must manage their own certificates
- Distributed gateway (multiple gateway instances for high availability)
