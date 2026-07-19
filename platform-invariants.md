# Shared platform invariants

These are guarantees and constraints owned by `platform-engineering`. Application pull requests are verified against this document before merge.

1. The `fast-ssd` StorageClass supports **1,000–5,000 provisioned IOPS** per claim.
2. Workloads must use Kubernetes DNS names, never hard-coded private ClusterIP addresses.
3. Public ingress must use TLS and the `public-nginx` ingress class.
4. A single container may request at most **4 CPU** and **8Gi memory** in this demo platform.
5. The platform provides the stable NAT addresses `34.120.10.10` and `34.120.10.11` and stable ingress address `34.98.20.5` while a published service contract depends on them.

The machine-readable publication metadata and deterministic demo signals live in `platform-contract.json`. This Markdown remains the human-owned source of truth.

